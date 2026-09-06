# PRD: проверяемая forensic-трассировка выполнения тестов

**Статус:** в реализации; критерий завершения раздела 10 ещё не достигнут.
**Дата:** 2026-09-06.
**Область:** `--verbose=4`, native protocol suites, встроенные SDK suites и
executable extension suites.
**Версия контракта:** `aiqa.trace.v1` (alpha-совместимость).

## 1. Проблема

Текущий JSONL sidecar при `--verbose=4` уже содержит структурированные события,
локальные trace/span ID и часть HTTP-диагностики. Но по нему ещё нельзя
автоматически доказать, что весь запуск восстановим: у некоторых composite
probes validator появляется после завершения owning probe, часть preflight и
pagination не имеет полной родительской цепочки, а существующие тесты в основном
доказывают наличие отдельных событий, а не полноту графа.

Это опасно по двум причинам:

- при сбое нельзя надёжно установить, какой именно report probe владеет запросом
  или почему получен итоговый статус;
- лог может выглядеть правдоподобно, хотя содержит незавершённый span, неверный
  порядок завершения или утечку диагностических данных.

Нужен не «более подробный лог», а версионируемый и машинно проверяемый контракт:
он должен связывать исполнение с report-результатом и выпускать отдельный verdict
о полноте трассы.

## 2. Цель и измеримый результат

Реализовать при `--verbose=4` защищённый потоковый JSONL trace и соседний
`<name>.trace-verdict.json`, по которым AIQA и тесты могут доказать для каждого
локального запуска:

1. весь граф исполнения имеет структуру
   `run → target → provider/model → suite → named probe → logical request → attempt`;
2. каждый outbound HTTP attempt принадлежит ровно одному активному именованному
   report probe;
3. terminal status каждого probe объясняется validator/result evidence внутри
   его периода жизни;
4. trace не содержит секретов, URL/query, заголовков, request/response body,
   prompt, output, tool arguments или media payload;
5. итог `trace-verdict` сообщает PASS только при полном соблюдении контракта.

Успех не означает совместимость внешнего провайдера. Он означает, что локальный
AIQA-запуск воспроизводим и проверяем по собственным артефактам.

## 3. Решения по области

- Охват первой версии: native suites, built-in SDK suites и executable extension
  suites. Ни одна из этих веток не может быть исключена из verdict молча.
- Результат: одновременно автоматические Go-тесты и отдельный
  `<name>.trace-verdict.json` рядом с `<name>.log`.
- Версия: `aiqa.trace.v1`; до `1.0.0` разрешены breaking changes, но каждое такое
  изменение обязано поднять/изменить версию формата, обновить schema, fixtures и
  migration note. Старый лог нельзя интерпретировать как новый без явного
  compatibility adapter.
- Trace остаётся локальным. OpenTelemetry propagation к provider APIs выключен
  по умолчанию и допускается только явной настройкой точного allowlisted origin.
- Реальные API, ключи, биллинг и публикация релиза не требуются для приёмки PRD.

## 4. Пользовательские истории

### US-001: получить защищённые артефакты запуска

Как пользователь AIQA, я хочу после `--verbose=4` получить trace и verdict,
чтобы диагностировать запуск без повторного обращения к провайдеру.

Критерии приёмки:

- [ ] До первого сетевого действия создаются `<name>.log` и
      `<name>.trace-verdict.json` с правами `0600`.
- [ ] Лог — потоковый JSONL, а не буфер, который может бесшумно обрезаться по
      лимиту размера; при ошибке записи запуск получает явную диагностическую
      ошибку и verdict `inconclusive` либо `fail` согласно FR-22.
- [ ] При `--verbose` ниже 4 trace-verdict не притворяется полным forensic
      доказательством: он либо не создаётся, либо имеет `status: not_requested`.
- [ ] Каждый JSONL record содержит `schema_version`, `time`, монотонный `seq`,
      `level`, `event`, `trace_id`, `span_id`, `parent_span_id`, `operation_id`.
- [ ] В JSONL отсутствуют credential values, заголовки, URL и query strings,
      request/response bodies, prompt, generated output, thought/tool/media data.

### US-002: восстановить граф конкретной проверки

Как сопровождающий AIQA, я хочу по ID однозначно пройти от итогового report probe
к его logical request и HTTP attempts.

Критерии приёмки:

- [ ] Есть ровно один root `run` span на запуск и он завершается после всех детей.
- [ ] Каждый target имеет отдельный span; одна target не может быть родителем
      событий другой target.
- [ ] Каждая модельная или provider-level операция имеет явный subject span,
      объясняющий, почему model отсутствует у discovery-only проверки.
- [ ] Каждый suite имеет устойчивый ID, executor (`native`, `sdk`, `extension`)
      и собственный lifecycle; одинаковые SDK suites в разных targets не сливаются.
- [ ] Каждый concrete report probe имеет стабильный ID из профиля/результата,
      а synthetic operational probes имеют явную классификацию и связь с
      конкретной report probe.
- [ ] Каждый `http.attempt` — потомок ровно одного `logical_request`; каждый
      `logical_request` — потомок активного именованного probe. HTTP без такой
      цепочки вызывает trace-verdict FAIL.
- [ ] Composite probe хранит `composite_of`/`derived_from` ссылки; все его
      validator events и производные результаты появляются до `probe.end`.

### US-003: корректно описать transport и SDK lifecycle

Как разработчик, я хочу видеть, почему запрос был повторён, отменён, завершён
SSE-ошибкой или ошибкой закрытия body.

Критерии приёмки:

- [ ] Для каждой попытки зафиксированы безопасные `attempt_number`, method,
      origin identity без пути/query, transport outcome, HTTP status class,
      duration и retry decision без raw error/response text.
- [ ] Pacing, ожидание slot, backoff и retry связаны с тем же logical request.
- [ ] Для streaming/SSE фиксируются только безопасные структура и счётчики:
      event index, наличие ID, безопасный kind и количество JSON keys; raw event
      type, key names и data не записываются.
- [ ] Для SDK `sdk.logical_request` и `sdk.probe` завершаются только после
      `Body.Close`; ошибка close фиксируется как отдельный безопасный outcome.
- [ ] Redirect к origin вне allowlist очищает propagation headers; default mode
      не отправляет `traceparent`, `tracestate` или baggage ни на один provider.
- [ ] Timeout, cancellation, read error, retry exhaustion и close error имеют
      terminal event и не оставляют незавершённых ancestor spans.

### US-004: получить машинный verdict и объяснение нарушения

Как CI и сопровождающий, я хочу автоматически определить, пригоден ли trace для
аудита, без разбора тысяч строк JSONL вручную.

Критерии приёмки:

- [ ] `<name>.trace-verdict.json` соответствует публичной JSON Schema
      `aiqa.trace-verdict.v1`.
- [ ] Verdict содержит `trace_schema_version`, `run_id`, `status`,
      `checked_records`, `checked_spans`, `checked_http_attempts`,
      `coverage` по executor/protocol/target и массив typed violations.
- [ ] Допустимые статусы: `pass`, `fail`, `inconclusive`, `not_requested`.
      `pass` разрешён только если нет violations и все ожидаемые ветки trace
      завершены; `inconclusive` используется при невозможности прочитать/дописать
      trace или при намеренно неполном диагностическом уровне.
- [ ] Каждое violation имеет стабильный `code`, severity, безопасные ссылки на
      `seq`/operation/span/probe IDs и краткое remediation; в нём нет payload.
- [ ] Markdown и report JSON ссылаются на наличие trace-verdict безопасным именем
      файла и его status, но forensic verdict не меняет canonical compatibility
      verdict и exit policy без отдельного будущего решения.
- [ ] Реализована локальная команда/внутренний API повторной проверки уже
      сохранённого JSONL без сетевых запросов; её результат совпадает с verdict,
      созданным во время запуска на тех же fixtures.

### US-005: не допустить регрессию тестами

Как сопровождающий, я хочу, чтобы нарушение графа или redaction нельзя было
замаскировать отдельными корректными log lines.

Критерии приёмки:

- [ ] Общий graph validator читает JSONL как данные, а не проверяет substring;
      он проверяет уникальность start/end, существование родителей, вложенность,
      порядок завершения, trace consistency и ownership каждого HTTP attempt.
- [ ] Есть отдельные loopback fixtures для native OpenAI, Anthropic и Google
      GenAI, built-in SDK и executable extension suite.
- [ ] Для retry, timeout, cancellation, SSE read error и body-close error есть
      самостоятельные тесты с точными ожидаемыми span IDs и terminal outcomes.
- [ ] Для composite result есть fixture с несколькими derived validators; тест
      доказывает, что terminal outcome агрегирует все результаты, а не первый.
- [ ] Negative fixtures намеренно создают orphan attempt, child-after-parent-end,
      duplicate end, missing end, late validator и redaction leak; validator
      обязан вернуть заданный violation code.
- [ ] Параллельное исполнение с `--concurrency > 1` сохраняет целостность seq и
      независимые деревья без ложного смешивания target/model/probe.

## 5. Функциональные требования

- **FR-1.** Ввести versioned JSONL contract `aiqa.trace.v1` и публичную schema
  trace-verdict. Имена events, typed fields и violation codes документируются.
- **FR-2.** Корреляция каждого lifecycle record обязательна. Пустые trace/span
  IDs разрешены только для явно unscoped application records, которые не могут
  изображать execution event.
- **FR-3.** Span имеет ровно один `.start` и один соответствующий `.end` с тем же
  `span_id`; один `.end` не может закрывать несколько операций.
- **FR-4.** Parent существует, начинается раньше child и заканчивается позже
  child. Нарушение — `parent_missing`, `parent_order_invalid` или
  `child_outlives_parent`.
- **FR-5.** `run`, target, subject, suite, named probe, logical request и attempt
  образуют явную цепочку. Исключения (например, process bootstrap) типизированы
  и не учитываются как тестовое исполнение.
- **FR-6.** Provider-level discovery и model preflight обязаны иметь owner subject
  и suite/probe, даже если в отчёте нет исполняемой модели.
- **FR-7.** Каждая report result/validator event относится к активному probe и
  записывается до его terminal event. Постфактум менять result без нового
  допустимого lifecycle event запрещено.
- **FR-8.** Итог composite probe агрегирует все дочерние outcomes по
  документированному приоритету `error > fail > blocked > skipped > pass`; нельзя
  использовать outcome первого дочернего результата как итог.
- **FR-9.** Каждый HTTP attempt имеет owner named probe. Reused transport,
  pagination, retries и redirect не создают неучтённых attempts.
- **FR-10.** События transport не записывают request/response payload либо
  provider-generated content. Разрешённые поля формализуются allowlist, а не
  denylist.
- **FR-11.** Все redaction rules применяются непосредственно перед записью в
  любой sink, включая error paths и trace-verdict violations.
- **FR-12.** SDK body wrapper сохраняет identity logical/probe span и завершает
  оба только при `Close`; двойной close не создаёт второй terminal event.
- **FR-13.** Executor extension обязан завершить все открытые spans при
  cancellation, process error или ошибке одной параллельной ветки.
- **FR-14.** OTel propagation остаётся off by default. При opt-in разрешён только
  exact origin allowlist; redirect и cross-origin request повторно проверяются.
- **FR-15.** Trace-verdict вычисляется потоково или из сохранённого JSONL; он не
  требует API key, сети, provider response или report payload.
- **FR-16.** `trace-verdict.status=pass` покрывает все реально запущенные
  executor branches. Неактивные suite/probe фиксируются как `not_selected`, а не
  как успешно проверенные.
- **FR-17.** Для каждого violation предусмотрены stable code, описание и
  regression fixture. Изменение code требует migration note.
- **FR-18.** Verdict не содержит абсолютные пути, credentials или содержимое
  диагностируемого payload.
- **FR-19.** CLI/documentation явно различают compatibility report и trace
  verification: PASS совместимости не доказывает корректность trace, и наоборот.
- **FR-20.** Во время alpha допускаются breaking changes формата только с
  изменением `trace_schema_version`, schema, fixture expectations и документации.
- **FR-21.** Для каждого PR реализации поддерживается таблица traceability:
  `PRD requirement → source path → positive test → negative test/fixture → evidence`.
- **FR-22.** Ошибка записи forensic sidecar не должна быть скрыта. Если execution
  продолжается, trace-verdict обязан быть `inconclusive` и содержать безопасный
  `trace_write_failed`; если продолжение небезопасно — запуск завершается
  конфигурационной/операционной ошибкой до публикации ложного forensic PASS.

## 6. Формат `trace-verdict` (минимальный контракт)

```json
{
  "schema_version": "aiqa.trace-verdict.v1",
  "trace_schema_version": "aiqa.trace.v1",
  "run_id": "local-safe-id",
  "status": "pass",
  "checked_records": 0,
  "checked_spans": 0,
  "checked_http_attempts": 0,
  "coverage": [{"target": "openai", "executor": "native", "status": "complete"}],
  "violations": []
}
```

Поле `run_id` — случайный локальный идентификатор, не URL, не request ID
провайдера и не credential-derived value. Точные дополнительные поля допустимы
только после schema review и fixture coverage.

## 7. Матрица приёмки

| Сценарий | Что доказывается | Ожидаемый trace-verdict |
|---|---|---|
| Native OpenAI loopback | Полная цепочка, basic request и retry | `pass` |
| Native Anthropic composite | Derived validators до `probe.end`, агрегированный outcome | `pass` |
| Native Google discovery/preflight | provider/model/suite/probe ownership, pagination | `pass` |
| Built-in SDK normal response | `sdk.probe` и logical request закрыты после `Body.Close` | `pass` |
| Built-in SDK close error | Один terminal outcome `close_error`, корректный порядок | `pass` |
| Executable extension, одна ветка падает | Все параллельные branches закрыты, failure атрибутирован suite | `pass` |
| Retry exhaustion | Все attempt принадлежат одному logical request, финальный outcome | `pass` |
| Timeout/cancellation | Нет orphan/missing end | `pass` |
| SSE truncated stream | Безопасный SSE event structure и read-error terminal | `pass` |
| Cross-origin redirect | Trace propagation очищен | `pass` |
| Fixture с orphan HTTP attempt | Детектор ownership | `fail`, `http_attempt_without_probe` |
| Fixture с late validator | Детектор lifecycle | `fail`, `validator_after_probe_end` |
| Fixture с duplicate end | Детектор cardinality | `fail`, `duplicate_span_end` |
| Fixture с credential/prompt | Детектор privacy | `fail`, `sensitive_field_detected` |
| `--verbose=0..3` | Нет ложного forensic утверждения | `not_requested` |

## 8. Порядок реализации и контроль прогресса

| Этап | Результат | Условие перехода |
|---|---|---|
| 1. Контракт | Schema, event catalogue, violation catalogue, fixtures | Schema tests и review таблицы FR-1..FR-5 |
| 2. Lifecycle | Исправлены native/SDK/extension owner chains и composite semantics | Положительные graph tests для всех трёх executor типов |
| 3. Privacy/transport | Redaction boundary, retries, SSE, close, redirects | Negative privacy и transport fixtures проходят |
| 4. Verdict | Stream/offline validator, JSON artifact, docs | Один и тот же fixture даёт идентичный online/offline verdict |
| 5. Приёмка | Полная матрица и repository gates | `make check`, `make race`, `make security-check` на итоговом HEAD |

После каждого этапа исполнитель прикладывает к PR таблицу из FR-21 с фактическими
командами, revision и результатами. PASS отдельного package test не закрывает
этап 5 и не доказывает готовность release.

## 9. Что не входит в задачу

- Подтверждение совместимости, качества или доступности реальных OpenAI,
  Anthropic, Google GenAI либо сторонних провайдеров.
- Запуск с реальными credential, расходование токенов/артефактов или публикация
  отчётов как часть автоматической приёмки.
- Удалённый OTel exporter, глобальная OTel-конфигурация или propagation по
  умолчанию.
- Запись в trace provider request IDs, raw errors, ответов, prompt/output или
  расширение диагностических данных за границы allowlist.
- Изменение canonical conformance/assessment verdict либо exit policy только из-за
  trace-verdict; такое решение требует отдельного PRD.
- Долгосрочная обратная совместимость alpha trace без явной версии/migration.

## 10. Критерий завершения

PRD считается реализованным только когда:

- все FR-1..FR-22 имеют source/test/evidence в traceability table;
- все строки матрицы приёмки проходят на loopback fixtures;
- offline validator и verdict, созданный во время запуска, совпадают;
- на итоговом commit прошли `make check`, `make race`, `make security-check`;
- независимая проверка JSONL fixtures не находит orphan spans/attempts, нарушения
  вложенности, late validators или чувствительные данные;
- документация описывает версию, ограничения и различие между trace-verdict и
  compatibility report.

Отсутствие реального provider run не блокирует завершение: задача доказывает
корректность наблюдаемости AIQA, а не поведение внешнего API.
