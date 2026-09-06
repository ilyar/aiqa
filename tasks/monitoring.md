# PRD: Прогресс выполнения и приватная диагностика CLI `aiqa`

**Статус:** готово к реализации

**Область:** CLI UX, progress reporting, logging
**Решение:** progress — обязательный интерфейс оператора; `--verbose` управляет только детализацией приватного JSONL-лога.

## 1. Проблема

Длительный запуск `aiqa` должен быть понятен без включения диагностики. Сейчас
`--verbose 4` выводит в терминал сырой JSONL-трейс. Он пригоден для
post-mortem анализа, но не отвечает во время запуска на вопросы: выполняется
ли процесс, какой provider активен, сколько работы осталось и ожидает ли он
rate-limit cooldown.

Нельзя честно вычислить единый total до первого сетевого запроса: при пустом
`--model` inventory определяет модели, capability classification определяет
модули, а pagination, retry, adaptive context probes и executable extensions
могут добавлять работу динамически.

## 2. Цели

- Показывать понятное состояние выполнения при `--verbose 0` и без
  `--verbose`.
- После discovery показывать честные процент, скорость и ETA для известного
  плана логических проб.
- Не изменять compatibility verdict, запросы к provider-ам, pacing или
  exit-code policy.
- Сохранить JSONL как приватную forensic-диагностику, не печатая его в
  терминал.
- Следовать Unix-правилам: интерактивное обновление только на TTY; при
  перенаправлении — неизменяемые текстовые строки без ANSI-последовательностей.

## 3. Термины и инварианты

- **Логическая проба** — AIQA-owned unit с одним lifecycle `probe.start` /
  `probe.end`; это единица прогресса.
- **HTTP attempt** не является единицей прогресса: retry и rate-limit меняют
  число attempts, но не выполненную работу.
- **План** — известное на данный момент число логических проб. План может быть
  пересмотрен только явным событием `plan revised`; процент никогда не должен
  ложно заявляться как окончательные `100%` до завершения target-а.
- **Progress output** независим от `--verbose` и не попадает в `.log` как
  диагностическая запись.
- Приватный JSONL создаётся только при verbosity `1..4`, как сейчас; уровень
  определяет его состав. При `0` прогресс остаётся видимым, но forensic-файл
  не требуется.

## 4. Поведение CLI и потоков

| Канал | TTY | non-TTY / CI | Зависит от `--verbose` |
| --- | --- | --- | --- |
| `stderr` | одна перерисовываемая progress-строка | неизменяемые milestone/heartbeat строки | нет |
| `stdout` | не используется для progress или JSONL | не используется для progress или JSONL | нет |
| `report/<name>.log` | приватный JSONL при `-v..-vvvv` | то же | да |
| `report/<name>.md`, `.json` | итоговые файловые артефакты | то же | нет |

`stdout` остаётся чистым для будущего машинного CLI contract; этот PRD не
добавляет потоковый вывод отчёта и не обещает, что `> report.json` создаст
JSON-отчёт.

### 4.1. Интерактивный TTY

Писать progress в `stderr`, перерисовывая одну строку через `\r`. Не скрывать
курсор и не использовать зависимость от внешнего terminal UI framework.

```text
aiqa: Anthropic  [##########----------] 24/42 57%  0.9 probe/s  ETA 20s  messages.basic
```

При завершении target-а и всего запуска строка завершается `\n`. Ширина бара
адаптируется к разумной фиксированной ширине; при узком терминале сокращаются
сначала модель/проба, затем скорость и ETA, но target и `done/total` остаются.

### 4.2. non-TTY и CI

Не писать `\r`, ANSI или escape sequences. Печатать:

- начало run и начало discovery target-а;
- зафиксированный план;
- начало ожидания pacing/rate-limit и его окончание;
- checkpoint при изменении целого процента на 10 пунктов либо не реже одного
  раза в 30 секунд;
- завершение target-а и итог со списком созданных отчётов.

```text
aiqa: google-genai: planned 42 probes
aiqa: google-genai: progress 24/42 (57%), 0.9 probe/s, ETA 20s
aiqa: google-genai: waiting for provider pacing (18s)
```

Ошибки запуска и итоговая ошибка всегда пишутся в `stderr`, независимо от
verbosity.

## 5. Планирование, счётчики и ETA

### 5.1. Две фазы

1. **До inventory.** Отображается indeterminate состояние: `Planning N
   targets`, затем `Discovering <target>`. Процента и ETA нет.
2. **После inventory и classification текущего target-а.** Раннер публикует
   immutable snapshot известного плана: discovery, выбранные native probes,
   provider endpoint probes и объявленные built-in extension suites. После
   этого отображаются `done/total`, процент, скорость и ETA.

Targets исполняются последовательно; progress явно называет активный target,
а не создаёт впечатление параллельной работы между provider-ами.

### 5.2. Динамическая работа

- Pagination, retries и pacing учитываются как состояние текущей логической
  пробы, не как новые завершённые пробы.
- `model-param` adaptive context search и executable extension suites не
  включаются в обещанный фиксированный total, пока host не получил их
  объявленный план/терминальный результат.
- Если известная host-owned работа добавлена после snapshot-а, показать
  `plan revised: 42 -> 57`, обновить total и ETA. Счётчик `done` сохраняется.
- External executable не может сообщать точное число своих HTTP attempts;
  его declared suite/invocation — одна логическая единица progress. Частичный
  или отсутствующий callback завершает эту единицу с ошибкой.

### 5.3. Скорость и ETA

- Скорость — EMA по времени завершения логических проб; измерение начинается
  после двух terminal events.
- До этого выводить `ETA estimating`; при нулевой скорости, контекстной отмене
  или неизвестном total выводить `ETA unknown`, не бесконечность и не
  отрицательное значение.
- В wall-clock время входят pacing и provider latency, поэтому ETA отражает
  реальное ожидание пользователя. При событии pacing UI немедленно сообщает
  ожидание, даже если checkpoint ещё не наступил.

## 6. Реализационные границы

### US-001: Отделить progress от логирования

Как оператор, я вижу ход запуска без `--verbose`, а forensic-записи не
засоряют терминал.

**Критерии приёмки:**

- [ ] В `internal/app/app.go` убрать прямой вывод `logger.LogfLine` в
  `stderr`; JSONL остаётся sidecar-артефактом.
- [ ] Создать внутренний progress reporter с отдельным writer, clock и
  TTY-detector, пригодными для unit-тестов.
- [ ] `--verbose 0` выдаёт progress, но не создаёт `.log`; `-v..-vvvv`
  сохраняют прогресс и создают JSONL соответствующего уровня.
- [ ] Ни один progress-текст не содержит credential, prompt, response body
  или API key.

### US-002: План и lifecycle-адаптер

Как оператор, я вижу корректный total лишь после того, как он стал известен.

**Критерии приёмки:**

- [ ] До discovery показывается indeterminate стадия без `%`, `N/M` или ETA.
- [ ] Native runners публикуют plan только после `selectModels` и capability
  classification; plan содержит только логические host-owned единицы.
- [ ] `probe.end` обновляет completed ровно один раз по operation/probe ID;
  validator и HTTP attempt не меняют completed.
- [ ] Динамическое расширение фиксируется отдельным событием revision и не
  допускает `done > total`.
- [ ] Подсчёт не меняет существующие telemetry lifecycle graph и
  traceverify-инварианты.

### US-003: Unix-safe rendering

Как пользователь терминала или CI, я получаю читаемый вывод без повреждения
pipe, файла или terminal state.

**Критерии приёмки:**

- [ ] TTY renderer использует одну строку и завершает её новой строкой при
  target end, run end и cancellation.
- [ ] Non-TTY renderer не содержит `\r` или ANSI и соблюдает heartbeat rule.
- [ ] `SIGINT`/`SIGTERM` завершают текущую progress-строку и сохраняют
  существующий exit semantics.
- [ ] `NO_COLOR` не требуется для progress v1, потому что v1 не использует
  цвет; цвет и сложный terminal UI — вне scope.

### US-004: ETA и pacing state

Как оператор, я отличаю полезную работу от ожидания provider limit.

**Критерии приёмки:**

- [ ] Первые одна-две пробы показывают `ETA estimating`, а не фиктивное число.
- [ ] ETA пересчитывается при completion, plan revision и завершении/отмене
  pacing wait.
- [ ] Pacing wait отображает target и известную длительность; если длительность
  неизвестна — `waiting for provider pacing`, без выдуманного countdown.
- [ ] Для `--concurrency > 1` показываются `active/limit` логических проб
  текущего target-а, если эти значения доступны; иначе поле опускается.

## 7. Технические точки интеграции

- `internal/app/app.go`: владеет run/target lifecycle, сортирует и
  последовательно выполняет targets; создаёт reporter и финализирует UI.
- `internal/protocol/{openai,anthropic,googlegenai}/runner.go`: после discovery
  и model/capability selection могут материализовать native plan.
- `internal/suites/common.go`: единая точка `probe.start`/`probe.end` для
  native probes; подходящая граница для completion callback.
- `internal/api/client.go`: уже публикует `pacing.wait.begin/end`; добавить
  безопасный progress callback, не интерпретируя HTTP attempts как completion.
- `internal/extsuite/runner.go` и `collector.go`: объявляют built-in/external
  suites и могут сообщить invocation terminal state.
- `internal/logging` и `internal/telemetry`: остаются источником private trace;
  progress не должен парсить JSONL обратно и не должен зависеть от verbosity.
- [Forensic baseline diagnostics](prd-forensic-baseline-diagnostics.md)
  остаётся нормативным контрактом для содержания, privacy, `aiqa.trace.v1` и
  lifecycle completeness private sidecar. Этот PRD меняет только его terminal
  presentation.

## 8. Тестовый план

- Unit: renderer TTY/non-TTY, truncation, checkpoint throttle, ETA states,
  plan revision, cleanup after cancellation.
- Unit: счётчик deduplicates duplicate terminal notification и не считает
  retries/validators как новые пробы.
- Runner tests: discovery без model inventory не публикует fixed total; после
  selection published plan совпадает с реально созданными logical probes.
- Integration: fake provider с pacing wait и concurrency >1; проверить
  `done <= total`, progress-восстановление и отсутствие ANSI в non-TTY.
- Regression: `make qa`, включая `make example`; сгенерированные примеры не
  должны содержать progress-вывод, потому что отчёты остаются файловыми.

## 9. Не входит в scope

- Подкоманда `aiqa log`, tail/follow и таблицы просмотра JSONL. Текущий JSONL
  уже можно читать `jq`; отдельный viewer — самостоятельный продукт.
- Потоковая сериализация Markdown/JSON-отчётов в `stdout`.
- Изменение формата `aiqa.trace.v1`, публичного report schema, verdict или
  exit policy.
- Изменение scheduler-а, concurrency, retry или rate-limit pacing.
- Цвета, скрытие курсора, сторонние TUI-библиотеки и сохранение истории
  progress в отчёте.

## 10. Готовность к реализации

План готов: архитектура уже имеет lifecycle-события `run`, `target`, `suite`,
`probe` и pacing события, а `app` владеет границами run/target. Требуемые
изменения локальны в перечисленных пакетах и не требуют credentials, внешних
сервисов или нового runtime dependency.

Контракт реализации уже выбран: создать внутренний `ProgressReporter` и
передавать его native runner-ам через `protocol.RunOptions`. Runners не
возвращают transient progress state вместе с report result. Это не смешивает
публичный результат с UI и не требует дальнейших продуктовых решений.

## 11. Проверяемый результат

Реализация считается завершённой, когда:

1. Без `--verbose` интерактивный запуск постоянно показывает текущую стадию,
   а non-TTY запуск регулярно выдаёт plain-text progress.
2. После discovery все фиксированные проценты удовлетворяют `0 <= done <=
   total`; единица — логическая проба, не HTTP attempt.
3. Пользователь видит pacing ожидание, а не воспринимает его как зависание.
4. `-vvvv` сохраняет полный приватный JSONL, но терминал остаётся операторским
   UI, а не неотформатированным JSON.
5. `make qa` и `make example` проходят на интегрированном HEAD.
