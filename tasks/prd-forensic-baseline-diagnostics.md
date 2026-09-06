# PRD: Доказательная диагностика baseline-совместимости

## Контекст

Запуск `baseline-compat-report` на официальных API сформировал полезный
Markdown/JSON-отчёт и private verbose-log, но не позволяет доказательно
разобрать часть результатов. В частности, Google Interactions SSE validator
считает отсутствующий `step_id` ошибкой, хотя актуальный контракт использует
числовой `index`. Ряд других результатов содержит только HTTP status и
человекочитаемую summary без безопасного описания формы ответа. Нельзя по ним
отличить несовместимость провайдера от устаревшего checker-а, неверного
ожидания AIQA или timeout встроенного SDK-suite.

Цель этой работы — сделать `--verbose 4` достаточным для безопасного
воспроизведения причин каждого non-pass результата и устранить подтверждённый
ложный Google core-fail. Реальные провайдеры не запускаются как часть
разработки или автоматического gate; повторный baseline выполняет оператор
после локальной приёмки.

## Цели

- Исправить проверку Google Interactions SSE для актуального идентификатора
  шага `index`.
- Сделать private `.log` доказательным для native, SDK и extension результатов,
  не записывая credentials, request/response body, prompt, image/audio или
  произвольный provider text.
- Разделить в отчёте наблюдение, классификацию AIQA и доказательство, чтобы
  `HTTP 200` с неудачной semantic-эвристикой не выглядел как нарушение API.
- Гарантировать, что forensic trace можно использовать как полноту
  хронологии: каждый начатый span имеет terminal event либо run получает
  честный trace verdict `FAIL` с correlation ID.
- Дать оператору воспроизводимый чек-лист для повторной оценки спорных
  Google-, Anthropic- и SDK-результатов.

## Термины и границы доказательства

- **Provider nonconformance** — ответ противоречит конкретному актуальному
  контракту после того, как сохранённая безопасная форма ответа это доказывает.
- **AIQA checker defect** — fixture из актуальной спецификации не проходит
  validator или validator требует поле/порядок, которого спецификация не
  требует.
- **Behavioral observation** — модель не вернула точный `AIQA_OK`, function
  call или локальную JSON-schema при успешном API-ответе. Это не provider
  nonconformance без отдельного нормативного требования.
- **Execution gap** — timeout, cancellation, transport failure или ошибка SDK
  без окончательного HTTP evidence. Это не provider nonconformance.

## Пользовательские истории

### US-001: Актуальная Google Interactions SSE-машина

**Описание:** Как разработчик AIQA, я хочу валидировать официальный lifecycle
Interactions SSE по `index`, чтобы Google не получал ложный core-fail.

**Критерии приёмки:**

- [ ] `step.start`, `step.delta` и `step.stop` с одинаковым числовым `index`
  образуют один lifecycle; строковые legacy aliases могут сохраняться только
  как backwards-compatible input.
- [ ] Fixture с официальной последовательностью `interaction.created` →
  `step.start(index: 0)` → `step.delta(index: 0)` → `step.stop(index: 0)` →
  `interaction.completed` → `done/[DONE]` проходит.
- [ ] Некорректные порядок, дублирование индекса и terminal event до
  `step.stop` по-прежнему дают определённую validation error.
- [ ] Тесты покрывают `event_type`, вложенные `interaction`, `step` и `delta`
  в фактической форме Google API.
- [ ] `google.interactions.stream.sequence` становится `PASS` на loopback
  fixture актуального формата.

### US-002: Безопасные диагностические fingerprints HTTP и SSE

**Описание:** Как оператор, я хочу видеть форму спорного ответа в private-log,
чтобы отличить дефект checker-а от API без утечки пользовательских данных.

**Критерии приёмки:**

- [ ] Для каждого terminal native HTTP/SSE result на verbosity 4 записывается
  один `diagnostic.response_shape` event с target/protocol/model/suite/probe,
  operation/span IDs, status, content type, provider request ID (если есть),
  безопасными top-level ключами и типами полей.
- [ ] Для Google error фиксируются только `error`-envelope presence, keys,
  code/status и классифицированная причина; message может быть только
  redacted bounded diagnostic code, не provider text.
- [ ] Для SSE фиксируются ordered event names, `event_type`, наличие и тип
  correlation field (`index`/legacy alias), terminal markers и validation
  problems; event data и text delta не пишутся.
- [ ] Запрос фиксируется только как method, normalized path, content type,
  body-size bucket и approved request-shape keys. Base URL, Authorization,
  API key, cookies, prompt, tool arguments и media не пишутся.
- [ ] Новая allowlist-schema диагностических полей отклоняет любое неизвестное
  поле. Тест с секретом во вложенном object/array доказывает redaction или
  отказ записи.
- [ ] В public Markdown/JSON не появляются private diagnostics; sidecar
  остаётся mode `0600` и создаётся только при `--verbose >= 1`.

### US-003: Диагностика SDK и extension execution gaps

**Описание:** Как оператор, я хочу отличать таймаут extension-suite от
несовместимости API.

**Критерии приёмки:**

- [ ] Каждый extension suite terminal event включает `extension_id`,
  `suite_id`, subject reference, outcome, duration, HTTP status когда он
  известен, typed error kind (`deadline_exceeded`, `cancelled`, `transport`,
  `assertion`, `process`) и безопасный error code.
- [ ] Для process/SDK failure лог содержит sanitized bounded stderr/stdout
  fingerprint: byte count, SHA-256 и approved diagnostic code; raw streams не
  пишутся.
- [ ] `context deadline exceeded` при отсутствии HTTP response отображается
  как `execution_gap`, а не как provider incompatibility.
- [ ] Fixture покрывает extension assertion failure, deadline, cancellation,
  non-zero process exit и successful suite.

### US-004: Доказательная классификация semantic checks

**Описание:** Как читатель отчёта, я хочу понимать, является ли fail нарушением
контракта или только наблюдаемым поведением модели.

**Критерии приёмки:**

- [ ] Для каждого result с status `fail`, `warn`, `error`, `unsupported` или
  `inconclusive` JSON содержит `classification` из фиксированного набора:
  `provider_nonconformance`, `checker_defect`, `behavioral_observation`,
  `execution_gap`, `unsupported_capability`, `insufficient_evidence`.
- [ ] `classification` содержит requirement/profile revision и ссылки на
  private operation IDs, но не меняет canonical status без явного правила
  assessment.
- [ ] Exact-marker, tool-call и local structured-output assertions являются
  `behavioral_observation`, пока profile не содержит отдельного обязательного
  нормативного контракта.
- [ ] Error-shape checker при HTTP 4xx без тела получает
  `insufficient_evidence`, а не `provider_nonconformance`.
- [ ] Markdown показывает classification рядом с non-pass строкой и кратко
  объясняет, что это не эквивалентно сертификации/нарушению стандарта.

### US-005: Полнота forensic lifecycle

**Описание:** Как разработчик, я хочу, чтобы trace verdict находил и объяснял
незакрытые spans, включая SDK streaming paths.

**Критерии приёмки:**

- [ ] Для каждого begin-event существует один terminal end/result event с тем
  же span/operation ID либо trace verdict содержит `missing_span_end` с
  target/protocol/suite/probe/operation ID.
- [ ] Fixture воспроизводит незакрытый SDK stream span и получает trace
  verdict `FAIL`; исправленный path получает `PASS`.
- [ ] `baseline-compat-report`-подобная loopback fixture не имеет
  `missing_span_end`.
- [ ] Public report сохраняет ссылку и status trace verdict; несовместимость
  провайдера не выводится из trace verdict failure.

## Функциональные требования

1. AIQA MUST валидировать Google Interactions SSE по документированному
   числовому `index`; `step_id` не является обязательным условием обычного
   text/model-output lifecycle.
2. AIQA MUST иметь versioned allowlist для private diagnostics; logging
   произвольных map, response bodies или `error.Error()` запрещено.
3. AIQA MUST создавать machine-readable response/SSE shape diagnostics на
   `--verbose 4`; на уровнях 0–3 они не обязательны.
4. AIQA MUST сохранять correlation IDs во всех terminal diagnostic events.
5. AIQA MUST классифицировать result evidence отдельно от status и assessment.
6. AIQA MUST понижать вывод до `insufficient_evidence`, когда отсутствует
   безопасная форма необходимого response/error.
7. AIQA MUST считать SDK/extension timeout без HTTP response execution gap.
8. AIQA MUST testировать privacy recursively для object и array nesting.
9. AIQA MUST обновить report and trace schemas и их deterministic fixtures.

## Вне рамок

- Запуск, оплата или изменение состояния реальных provider API.
- Логирование prompt, completion, tool arguments, media, credentials, полного
  URL или raw SDK stdout/stderr.
- Изменение core/optional policy без отдельного решения.
- Новые provider surfaces, модели или расширение Anthropic coverage.
- Автоматическое исправление provider-side несовместимостей.

## Технические ориентиры

- Основные точки: `internal/protocol/googlegenai/stream.go`,
  `internal/protocol/googlegenai/validate.go`, `internal/api`,
  `internal/extsuite`, `internal/logging`, `internal/traceverify`,
  `internal/report` и JSON schemas.
- Existing `seq`, run/trace/span/operation IDs являются canonical correlation
  mechanism; новые diagnostics не создают параллельный ID namespace.
- Profile source revision и checker version должны попадать в classification,
  чтобы будущий отчёт можно было сравнить с актуальной документацией.

## Приёмка и доказательства

| Проверка | Доказательство |
| --- | --- |
| Google SSE parser | Unit fixtures для официального `index` lifecycle и invalid order |
| Privacy | Unit/property fixtures с credentials и nested raw data; allowlist rejection |
| Native diagnostics | Loopback HTTP 2xx/4xx/SSE fixtures, проверка JSONL fields и отсутствия raw content |
| SDK/extensions | Deterministic fake extension fixtures: assertion, timeout, cancel, process error |
| Trace lifecycle | Fixture с missing end → FAIL, исправленный stream → PASS |
| Report contract | JSON schema + Markdown golden tests для classification и verdict link |
| Полный локальный gate | `make qa` |

После локального gate оператор **может** повторить исходный baseline command с
`--verbose 4`. Ожидаемые признаки успеха: Google stream sequence больше не
ложно fail на `index`; каждый оставшийся core non-pass имеет classification и
связанные private shape diagnostics; trace verdict не содержит
`missing_span_end`. Повторный реальный запуск — отдельное операторское
доказательство, не часть CI и не условие завершения реализации.

## Метрики успеха

- 100% non-pass результатов в deterministic fixtures имеют classification и
  correlation ID.
- 100% тестовых diagnostic events проходят allowlist/privacy checks.
- Official Google `gemini-3.8-flash` SSE fixture проходит без `step_id`.
- Trace fixture без искусственно оставленного span завершается verdict `PASS`.

## Открытые вопросы

Нет блокирующих. При реализации нужно сверить Google profile `source_ref` с
текущими официальными API references и зафиксировать точную revision в тестах.
