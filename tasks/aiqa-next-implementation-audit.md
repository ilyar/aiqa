# AIQA: аудит реализации `tasks/next.md`

**Вердикт:** REQUEST CHANGES — реализация частичная; закрывать `tasks/next.md` как выполненный нельзя.  
**Дата проверки:** 2026-09-09.  
**Проверенный HEAD:** `6b92b97cb09f7840a41d0e8aa66922f8787ca2ba` — `docs: document non-tty heartbeat`, 2026-09-09 03:43:07Z.  
**Повторная проверка main:** тот же SHA.  
**Область:** соответствие реализации monitoring/concurrency/CLI требованиям [tasks/next.md](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/tasks/next.md), а не повторный аудит OpenAI/Anthropic/Google API profiles.

## 1. Основание и сила доказательств

Локальная копия спецификации сравнивалась не по имени файла: Git blob `2823bae45d7387e641055c609e85d8358cae63f5`, размер 114257 байт, совпадает с `tasks/next.md` в проверенном HEAD. Таким образом, проверена именно текущая спецификация, а не пересказ прошлого обсуждения.

Использованы три уровня доказательств:

- **REPRODUCED:** собственный регрессионный тест выполнен локально против побайтно идентичного исходника компонента из HEAD.
- **CODE-CONFIRMED:** вывод следует из конкретной связки функций в проверенном исходнике; полный CLI-сценарий локально не запускался.
- **PARTIAL / NOT VERIFIED:** присутствие компонента или теста подтверждено, но полного acceptance-сценария не доказано. Это не PASS.

Полный проект локально не собирался: доступный toolchain — Go 1.23.2, проект использует Go 1.27; получить runnable checkout в этой среде не удалось. Для локального воспроизведения использован отдельный модуль с неизменённым stdlib-only `internal/scheduler/scheduler.go`. Это **не** понижение `go.mod` проекта и не утверждение о прохождении `make qa`.

CI проверенного HEAD: [run 34321791547](https://github.com/ilyar/aiqa/actions/runs/34321791547), `success`. В job `test` прошли `make check`, `make security-check`, build, version и `make race`. Live jobs OpenAI, Anthropic, dual Bifrost и Google GenAI были `skipped`. Зелёный CI подтверждает имеющиеся проверки; он не доказывает выполнение всех A01–A44.

## 2. Краткий итог

Основной положительный результат — targets действительно переведены на параллельную подготовку и выполнение; появился общий broker для native/встроенных SDK HTTP. Исправлены raw job IDs и пакетная задержка SDK terminal results; добавлены non-TTY heartbeat, log reader и stdout opt-in.

Основной незакрытый результат — компоненты `execution`, `eta` и `terminal` не образуют действующую сквозную цепочку. Обычный CLI по-прежнему выводит старый однострочный Reporter. Наличие нового estimator или renderer в исходниках не равнозначно их использованию во время тестирования.

Кроме незавершённой интеграции, обнаружены реальные ошибки scheduler, обход общего лимита opaque extensions, некорректная безопасность artifact retries, потерянная отмена `aiqa log --follow`, преждевременное сообщение completion и нетрассируемый discovery.

Ниже P1 означает высокий приоритет и блокирование соответствующей приёмки. Это не CVSS и не утверждение о критической уязвимости публичного сервиса. P2 — существенная недоработка, не требующая аварийного исправления.

## 3. Findings

### F01 — P1 — Broker теряет пробуждение при переходе capacity → cooldown

**Доказательство:** REPRODUCED.  
**Место:** [internal/scheduler/scheduler.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/scheduler/scheduler.go), `Acquire`, `setWaitLocked`, `ApplyCooldown`, `Lease.release`.  
**Требования:** N03/N04; A04, A05, A08.

`Acquire` может войти в `select` с `timer=nil`, когда запрос ждёт освобождения capacity. Позднее `ApplyCooldown` изменяет `pending.eligibleAt`, но ожидающей горутине не отправляется уведомление о новом deadline. Её `select` слушает только result, cancellation и прежний timer.

Последовательность воспроизведения:

```text
capacity = 1
request A получает lease
request B ждёт capacity без таймера
для общей группы устанавливается cooldown +60s
A освобождает lease; B теперь должен ждать cooldown
fake clock продвигается на 61s
результат: inFlight=0, waiting=1; B не возобновляется
```

Без постороннего scheduler event или cancellation очередь может оставаться неподвижной после истечения cooldown. Установленный `--timeout` HTTP attempt здесь не спасает: attempt ещё не начался.

Тест `TestAuditCapacityWaitThenCooldownMustWake` упал 10/10 раз. С `-race` также упал; race detector не сообщил гонку данных — ошибка логическая, а не data race.

**Исправление:** broker должен владеть пробуждением расписания: отменяемый timer на ближайший eligible deadline и/или revision/change channel, пересоздаваемый при изменении очереди, capacity и cooldown. Waiter обязан перечитывать deadline при изменении; нужно проверить также extension/shortening deadlines. Не исправлять периодическим busy polling.

**Приёмка:** waiter получает lease после fake-clock advancement без дополнительных `Acquire`, `Release` или `ApplyCooldown` для пробуждения; cancellation до/после grant не теряет lease.

### F02 — P1 — Artifact budget не обеспечивает обещанную границу remote creation

**Доказательство:** CODE-CONFIRMED для retry; REPRODUCED для исчерпанного budget.  
**Места:** [internal/suites/images.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/suites/images.go), `probeImageGeneration`; [internal/api/client.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/api/client.go), `ReserveArtifact`, `ImagesGenerate`, `doRawURL`, SDK `RoundTrip`; [internal/scheduler/scheduler.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/scheduler/scheduler.go), `admissibleLocked`.  
**Требования:** N03; §7.6, A09.

Есть две разные проблемы.

**F02a. Одна reservation покрывает несколько потенциальных созданий.** Native image probe резервирует один artifact и вызывает `Commit` один раз до `ImagesGenerate`. Сам `ImagesGenerate` идёт через общий retry-loop до трёх HTTP attempts. Если provider создал изображение, но ответ потерян или получен transient error, повторная POST отправляется без новой reservation. Встроенный SDK также резервируется на invocation, а его transport имеет retries.

Следовательно, limit ограничивает число зарезервированных логических вызовов, а не число потенциальных remote creations. Не утверждается, что конкретный provider реально начислил дополнительную плату: это зависит от его обработки повторов. Но клиентская гарантия A09 не соблюдена.

**F02b. Исчерпанная корзина превращается в ожидание возобновляемой квоты.** После расходования artifact budget `admissibleLocked` возвращает `quota_capacity` без deadline. Artifact counter после committed release не уменьшается, поэтому такое ожидание само по себе никогда не разрешится.

Тест `TestAuditSpentArtifactBudgetMustNotWait`: budget=1, первая reservation потрачена, вторая возвращается только по context timeout вместо локального terminal budget rejection. Упал 10/10 раз и с `-race`.

**Исправление:** различить повтор безопасной read-only операции и ambiguous creation. После возможного dispatch создания либо запретить автоматический retry, либо требовать новую reservation для каждой новой потенциальной creation; вариант с idempotency допустим только при доказанном контракте endpoint. Исчерпанный consumptive budget должен немедленно разрешать job как local budget-constrained, не ждать «пополнения» и не обвинять provider. Для ещё не committed reservations можно отдельно моделировать возможный refund.

**Приёмка:** mock фиксирует creation до разрыва соединения; при limit=1 не приходит вторая потенциальная creation. Проверяется общая корзина native+SDK и корректный pre-dispatch refund.

### F03 — P1 — Opaque extensions не входят в общий request budget

**Доказательство:** CODE-CONFIRMED.  
**Места:** [internal/extsuite/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/extsuite/runner.go), executable branch в `executeTarget`; [internal/protocol/openai/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/openai/runner.go), target-local executor; [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), общий HTTP broker.  
**Требования:** N03/N04; §7.1, §7.7; A02, A06.

Native и встроенные SDK используют run broker через HTTP transport. Внешний executable по-прежнему запускается через `executor.DoN(slots, ...)` на target-owned executor. Этот reservation не увеличивает `scheduler.Broker.inFlight`. `unsafeLocks` также создаются внутри вызова одного target.

При параллельных targets возможны одновременно занятые global HTTP slots и незасчитанные opaque reservations. Два targets могут параллельно запустить один unsafe extension, поскольку их mutex не общий. Даже исправленный atomic `DoN` внутри локального executor не исправляет отсутствие reservation в run-wide budget.

**Исправление:** до запуска процесса атомарно резервировать объявленную capacity именно в run broker. Неизвестная concurrency — exclusive reservation на C. Locks и reservations должны быть run-scoped; telemetry процесса не считается доказательством host-enforced ограничения его реальных HTTP.

**Приёмка:** C=1, executable удерживает reservation — native/SDK другого target не допускается до её освобождения; два unsafe invocations не перекрываются; UI отдельно считает observed HTTP и external reserved.

### F04 — P1 — Pacing groups объединяют только часть состояния квоты

**Доказательство:** CODE-CONFIRMED.  
**Места:** [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), `newRunScheduler`; [internal/api/client.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/api/client.go), `newClient`, `SetScheduler`, `applySchedulerRetryAfter`, `normalizedOrigin`; [internal/api/pacing.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/api/pacing.go).  
**Требования:** N03/N04; §7.2–7.4, A03–A05.

Общий broker получает global capacity/memory/artifact budget, но не получает параметры learned pacing каждой группы. Динамически созданная группа имеет capacity=C и interval=0. Каждый target по-прежнему создаёт собственный adaptive pacer с локальным interval, learned limit и success streak.

В broker передаётся положительный явный `Retry-After`; adaptive cooldown после 429 без такого header и learned capacity/interval остаются target-local. Поэтому одинаковый origin или explicit alias ещё не означает общий pacer, обещанный §7.2.

Дополнительная точность ключа: `normalizedOrigin` учитывает только явно указанный port. `https://host` и `https://host:443` получают разные hash keys, хотя §7.2 требует effective port.

В native пути release broker lease происходит раньше обработки 429 и установки shared cooldown: уже queued work может быть допущено в этом промежутке. Эту границу необходимо проверить и упорядочить вместе с общей квотой.

**Исправление:** сделать interval, learned capacity, cooldown и revision состоянием одной admission group. Обновлять его до допуска следующей заявки. Не «приклеивать» общий ограничитель после нескольких независимых pacers. Нормализовать default ports и IPv6 origins.

**Приёмка:** same-origin three-path fixtures и explicit aliases проверяют не только shared Retry-After, но и общий начальный limit=1, увеличение limit, no-header429 и минимальное расстояние между group dispatches.

### F05 — P1 — RunPlan проверяется, но не является исполняемым источником истины

**Доказательство:** CODE-CONFIRMED.  
**Места:** [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), `runPlanFromPrepared`/`runMultiTarget`; [internal/protocol/registry.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/registry.go), `PreparedTarget.Binding`/`ExecuteFrozen`; [internal/protocol/openai/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/openai/runner.go), `PrepareTarget`/`RunTarget`; [internal/extsuite/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/extsuite/runner.go), `PlannedJobs`/`executeTarget`.  
**Требования:** N02/N05; §6.2–6.3, A11/A12/A14/A15.

В приложение добавлены `RunPlan` и `ValidatePlan`, но после проверки plan не передаётся исполнителю. Выполняются прежние closures `prepared.Execute`; `ExecuteFrozen` остаётся вызовом `run(ctx)`. При построении job указан общий `Suite="planned"`, `Kind="workflow"`; реальные dependencies, cost/estimate classes и invocation bindings из §6.2 не связывают execution.

В OpenAI Prepare объединяются file overrides, но CLI `target.ModelCapabilities` применяются только в Run; override file также читается повторно. SDK plan вычисляется один раз по prepared candidates и затем повторно по итоговым `targetReport.Models`. Сохранённые SDK invocations не исполняются как неизменяемый набор.

Это воспроизводимый по коду путь расхождения: неизвестная модель получает explicit CLI capability → Prepare не видит её, Run видит → набор применимых SDK suites после Freeze отличается. `executionProgress` при неизвестном ID ставит unavailable и может вообще убрать optional execution summary; mismatch не устраняется.

**Исправление:** сначала одна локальная resolution/validation profiles+overrides, затем Prepare/preflight, затем immutable jobs и executable bindings. Execute принимает конкретный plan/fingerprint и не перечитывает файлы, не классифицирует заново, не перепланирует SDK. Допустимый conditional branch резервируется заранее.

**Приёмка:** CLI/file override equivalence, изменение override file после Freeze, неизвестный preflight route, exact planned-vs-executed IDs. Доказательство должно проходить через app, не только через `NewRunPlan` unit test.

### F06 — P1 — ETA и dashboard существуют как компоненты, но не работают в CLI-path

**Доказательство:** CODE-CONFIRMED.  
**Места:** [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), `newOperatorReporter`; [internal/progress/progress.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/progress/progress.go); [internal/eta/eta.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/eta/eta.go); [internal/terminal/terminal.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/terminal/terminal.go); [internal/app/execution_progress.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/execution_progress.go).  
**Требования:** N07–N09; §8–§12, A19–A32.

Обычный запуск создаёт `progress.Reporter`, а не adapter к `terminal.Render`. Reporter выводит одну строку `aiqa: [bar] done/total percent current`. Он не получает/не выводит ETA, per-target request states, per-target ETA или persistent target rows.

`Pacing` остаётся прежним `(target, waiting bool, duration)`: хранит max initial duration, не знает wait_id, eligible deadline/revision и не пересчитывает countdown. После последнего waiter старый current label может остаться. `TargetFinished` буквально формирует `completed — completed` при обычном completion.

`eta.Estimator` действительно содержит remaining-work simulation, а `terminal.Render` — responsive layouts и cell-aware truncation. Но их полезность для пользователя пока не проявляется: нет подключённого producer → execution snapshot → ETA → terminal writer конвейера.

Live TTY detection по-прежнему проверяет ModeCharDevice, а не использует существующий `term.IsTerminal` helper. Однострочная отрисовка использует `` без стирания хвоста более длинной предыдущей строки; текущий `safe` не является полной cell/bidi-safe функцией из нового terminal package. Это дополнительные проявления одной интеграционной незавершённости.

**Исправление:** единственный typed event reducer; живые request/wait/group snapshots; estimator только по этим данным; один writer, который использует renderer, размеры терминала и таймер отображения. Старый Reporter должен стать adapter либо быть удалён из runtime пути. Не оставлять две конкурирующие трактовки прогресса.

**Приёмка:** тест запуска реального CLI под PTY с 3 controlled providers и fake/controlled cooldown. Проверить фактический stderr, а не только `terminal.Render(Snapshot{...})`.

### F07 — P1 — Public execution aggregate выдаёт неполные наблюдения за измеренные значения

**Доказательство:** CODE-CONFIRMED; это вывод по цепочке построения, не измеренный live report.  
**Места:** [internal/app/execution_progress.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/execution_progress.go); [internal/execution/summary.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/execution/summary.go); [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go).  
**Требования:** N07/N08/N11; §12/§18, A23/A32/A39/A43.

`executionProgress` отправляет в reducer главным образом RunPhaseChanged, PlanFrozen, JobStarted, JobFinished. Pacing и lifecycle forwards не добавляют туда реальные request admissions/completions, waits и group changes. `SummaryBuilder.Observe` требует живых snapshots для peak/wait-duration, но bridge вызывает только `Build` на последнем snapshot.

Следствия этого пути:

```text
Requests/Waits/Groups не заполняются реальными событиями
observed_peak_inflight не измеряет фактический peak
pacing_group_count не отражает настроенные группы
wait-duration aggregates не отражают ожидания
phase сохраняется после PhaseFinalizing, до final commit
estimate_method указан, хотя estimator в runtime не запускался
```

Нулевые значения, или их отсутствие при omitempty, не являются доказательством отсутствия запросов/ожиданий. Отключение summary на accounting mismatch также не заменяет явный сигнал неполноты.

**Исправление:** связать broker/transport/lifecycle с reducer; Observe каждый согласованный snapshot либо считать интервалы непосредственно по событиям. Публиковать только измеренные поля; неизвестное обозначать явно. Финальную phase/elapsed фиксировать после завершения определённой операции finalization. Не заявлять метод вычисления ETA, который не использовался.

**Приёмка:** независимый mock ledger с ненулевым overlap и cooldown должен совпадать с report peak/group/wait counters. Добавить тест с одним быстрым и одним медленным запросом, где на финальном снимке inflight=0, но peak ранее был 2.

### F08 — P1 — `aiqa log --follow` не обрабатывает перехваченные SIGINT/SIGTERM

**Доказательство:** CODE-CONFIRMED. Полный CLI subprocess локально не запускался.  
**Места:** [cmd/aiqa/main.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/main.go), `main`/ветка `runCLI(args[0]=="log")`; [cmd/aiqa/log.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/log.go), `runLog`/`followLogFile`.  
**Требования:** N10/N11; §14.3, A35/A40.

`main` регистрирует `signal.Notify(signals, SIGINT, SIGTERM)` до выбора subcommand. Ветка log вызывает `runLog` напрямую и не читает signals. `followLogFile` — бесконечный poll loop без context и cancellation select.

Согласно [официальной документации os/signal](https://pkg.go.dev/os/signal#hdr-Changing_the_behavior_of_signals_in_Go_programs), Notify отменяет default termination и перенаправляет сигнал в channel. Следовательно, в log-follow пути Ctrl+C/SIGTERM не достигают ни default handler, ни собственного consumer. Второй сигнал также не вызывает логику `runApplication`, поскольку она здесь не вызвана.

**Исправление:** включить log reader в общий signal lifecycle, передать context в follow и блокирующее чтение, обрабатывать cancellation и ошибки writer. Либо не устанавливать execution signal handler для log subcommand и задать отдельную явную политику.

**Приёмка:** subprocess тест настоящего CLI, временный logfile, SIGINT → bounded termination, отдельно SIGTERM; без orphan process. Тест передачи signal только в mock `applicationRunner` этой ошибки не увидит.

### F09 — P1 — COMPLETED печатается до final forensic/отчётного commit

**Доказательство:** CODE-CONFIRMED.  
**Места:** [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), `runMultiTarget` и deferred forensic finalizer; [cmd/aiqa/main.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/main.go).  
**Требования:** N11; §16, A32/A38/A40.

Основное тело run сначала коммитит отчёт и печатает `completed; reports: ...`, а затем при возврате выполняется deferred trace close/verify, verdict write и повторный final report commit. Этот путь по-прежнему может завершиться runtime/write error после сообщения success. В операторский Reporter фаза FINALIZING не передаётся; его heartbeat остаётся running.

Положительная часть: на успешном пути deferred finalizer пересобирает stdout из того же final snapshot, что и файлы. Это прогресс по A38, но не исправление A32.

При cancellation до Freeze/на early fatal branches также нет доказанной общей процедуры сохранения partial reports всех targets; emptyReport может прервать сбор остальных результатов.

**Исправление:** явная последовательность остановки producers → закрытия trace → verification → окончательной сборки → commit → единственного COMPLETED. Deferred функции использовать для cleanup, а не для продолжения объявленной завершённой бизнес-операции. Partial evidence сохранять единообразно на early/error/cancel paths.

**Приёмка:** inject задержку verifier и финальную write failure; ни один COMPLETED до последнего успешного close/rename. SIGINT в discovery, during stream и при finalization проверять настоящим app runner.

### F10 — P1 — Prepare discovery всё ещё отсутствует в forensic execution graph

**Доказательство:** CODE-CONFIRMED на OpenAI Prepare; общий target lifecycle начинается после подготовки.  
**Места:** [internal/protocol/openai/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/openai/runner.go), `PrepareTarget`/`RunTarget`; [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go), target span после Freeze; [internal/protocol/registry.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/registry.go), `ConfigureClientScheduler`.  
**Требования:** N05; §13, A11/A43.

Prepare создаёт client и делает `ListModels` до `SetLogger`/`SetTelemetry`. ConfigureClientScheduler подключает broker, но не tracer. Target span в app начинается только при Execute. Позднее Inventory переиспользуется без повторной отправки, что правильно для цены/повторяемости, но отсутствующая иерархия spans ему не добавляется. Отдельные raw diagnostic records могут появляться через logger в context; это не полноценная ветвь target → probe → logical request → attempt.

Offline verifier может подтвердить внутреннюю согласованность записанного графа; это не доказывает полноту незаписанного discovery. Нельзя закрыть A43 проверкой только файла trace без независимого server ledger.

**Исправление:** target lifecycle и client tracing установить до Prepare network. Discovery имеет provider/suite/probe/logical-request/attempt owners. Span завершать один раз после всех target phases; assertions из сохранённого response связывать с исходным evidence.

**Приёмка:** каждый реальный запрос controlled server — включая первый `/models` — имеет ровно один соответствующий trace attempt и корректный target ancestry.

### F11 — P2 — Request memory budget выдаётся после выделения тела

**Доказательство:** CODE-CONFIRMED.  
**Место:** [internal/api/client.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/api/client.go), `DoJSONForModel`, `doJSON`, `StreamJSONForModel`, `doRawURL`; context payload producer требует отдельной интеграции.  
**Требования:** N03; §7.6, A10.

Вызов сначала выполняет `json.Marshal(body)`, а затем получает broker lease с `MemoryBytes=len(body)`. Значит, большое тело уже находится в памяти, когда request ждёт memory budget. Параллельные ожидающие workflows могут держать большие сериализованные буферы, не входящие в текущий reservation counter.

Это не утверждение о точном RSS потолке: allocator/runtime overhead требует отдельной модели. Но предусмотренная спецификацией reservation **до** крупных allocation здесь отсутствует.

**Исправление:** byte reservation перед созданием payload/serialization, shared immutable replay buffer и явный учёт копий; bounded serializer/lazy body builder. Зафиксировать, что именно покрывает лимит: request-owned buffers, а не весь RSS процесса.

**Приёмка:** memory instrumentation у controlled body factory подтверждает, что суммарные удерживаемые request buffers, включая queued work, не превышают budget; small requests независимой группы продолжаются.

## 4. Что уже реализовано и не следует делать заново

| Изменение | Подтверждённый результат | Граница вывода |
|---|---|---|
| Parallel targets | `parallelTargets` используется и в Prepare, и в Execute; сбор по индексам сохраняет порядок | Не доказывает полный shared-quota contract |
| Run-owned broker | Native и built-in SDK clients получают общую capacity | Opaque executables остаются вне неё, F03 |
| Atomic leases | Broker выдаёт несколько ресурсов под одним lock, Release идемпотентен | Есть lost wake и consumptive-budget wait |
| Raw IDs | Reporter больше не сокращает execution ID до 96 bytes | Labels/live terminal требуют дальнейшей работы |
| Immediate SDK result | Completion channel доставляет suite_result до завершения всех siblings | Start пока выставляется invocation-wide; frozen plan остаётся повторно вычисляемым |
| Missing SDK suite | Отсутствующий terminal превращается в runner_error, не общий completed | Нужен app-level test совпадения с frozen plan |
| Non-TTY heartbeat | Таймер 30s, fake-clock seams, generation guard | Он не подключает ETA и не решает FINALIZING |
| Log viewer | Есть reader/filter/follow/file resolution | Signal, TTY/color/output integration не завершены |
| Stdout opt-in | Final bytes на успешном forensic path обновляются вместе с файлами | Error/cancellation/final message order не закрыты |
| Signal execution path | У обычного run есть первый cancel и второй interrupt, 130/143 | Не действует на ветку log |
| Core architecture | Три протокола и `test=sdk` сохранены | Не требуется откат к предыдущим PRD |

Источники: [internal/app/app.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/app/app.go); [internal/scheduler/scheduler.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/scheduler/scheduler.go); [internal/progress/progress.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/progress/progress.go); [internal/extsuite/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/extsuite/runner.go); [cmd/aiqa/main.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/main.go); [cmd/aiqa/log.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/log.go).

## 5. Матрица N01–N13

Статус отражает весь deliverable, а не факт появления одноимённого package.

| ID | Результат аудита | Остаток |
|---|---|---|
| N01 | ЧАСТИЧНО | Raw IDs исправлены; нет полного audited acceptance ledger, план/реальное событие не всегда согласованы |
| N02 | НЕ ЗАКРЫТО | F05: RunPlan декларативный, overrides и SDK replanning расходятся |
| N03 | ЧАСТИЧНО, БЛОКЕРЫ | F01–F04/F11: broker присутствует, но safety/liveness/квоты неполны |
| N04 | ЧАСТИЧНО | Parallel Prepare/Execute есть; все ограничения A02–A05 не обеспечены |
| N05 | ЧАСТИЧНО | SDK terminal delivery улучшен; real Start и discovery trace не закрыты |
| N06 | ЧАСТИЧНО | Добавлена обработка глобального no-match; полный selected-scope и explicit implementation-gap contract требуют проверки |
| N07 | ЧАСТИЧНО, НЕ ИНТЕГРИРОВАНО | Reducer получает jobs, но не live request/group/wait facts; F07 |
| N08 | КОМПОНЕНТ + HEARTBEAT | Estimator реализован отдельно; runtime ETA отсутствует |
| N09 | КОМПОНЕНТ, НЕ ПОЛЬЗОВАТЕЛЬСКИЙ РЕЗУЛЬТАТ | Renderer есть, actual CLI использует старый Reporter |
| N10 | ЧАСТИЧНО | Log viewer появился; follow не отменяется сигналами, часть display semantics не подключена |
| N11 | ЧАСТИЧНО | Stdout и normal-run signals есть; premature completion и partial/finalization gaps |
| N12 | НЕ ГОТОВО К ЗАКРЫТИЮ | CI зелёный, но выявленные e2e gaps остаются; docs заявляют больше реального CLI |
| N13 | НЕ РЕАЛИЗОВАНО / ОТДЕЛЬНЫЙ P2 | Anthropic model-param пока закрывается skipped; собственных parameter probes в исследованном пути нет |

N13 — предусмотренная отдельно работа, а не повод блокировать независимый корректный monitoring fix. Но N06 требует уже сейчас объяснить пользователю `not implemented by AIQA`, а не молча показать обычный завершённый target. См. [internal/protocol/anthropic/runner.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/protocol/anthropic/runner.go).

## 6. A01–A44: прослеживаемость приёмки

Обозначения: **ЧАСТИЧНО** — полезная реализация есть, но criterion целиком не доказан; **НАРУШЕНО** — конкретное противоречие в коде/воспроизведение; **КОМПОНЕНТ** — изолированная реализация без runtime интеграции; **КОД ПОДТВЕРЖДЁН** — изученный путь соответствует узкому требованию, полного локального CLI-прогона не было; **НЕ ПРОВЕРЕНО** — нельзя честно выдать PASS.

| Acceptance | Статус | Основание | Finding / workstream |
|---|---|---|---|
| A01 | ЧАСТИЧНО | Parallel targets реализован; именно three-generation barrier acceptance локально не исполнен. | N04 |
| A02 | НАРУШЕНО | Opaque executable reservation вне общего broker; native/SDK подключены. | F03 |
| A03 | НАРУШЕНО | Shared Retry-After не равен shared learned pacer; effective ports не нормализованы. | F04 |
| A04 | НАРУШЕНО | Потеря wake после смены capacity wait на cooldown воспроизведена. | F01/F04 |
| A05 | ЧАСТИЧНО | Broker не занимает global slot ожидающими; liveness после reschedule нарушена. | F01 |
| A06 | ЧАСТИЧНО | Atomic broker leases есть, но opaque runtime использует локальный executor. | F03 |
| A07 | ЧАСТИЧНО | SDK lease живёт до Close/cancel; отдельный EOF путь operationReadCloser не переопределён. | api/client.go |
| A08 | ЧАСТИЧНО | Target round-robin есть; total liveness/fairness не доказаны из-за lost wake. | F01 |
| A09 | НАРУШЕНО | Ambiguous creation retries без новой reservation; consumed budget wait. | F02 |
| A10 | НАРУШЕНО | Marshal раньше memory admission. | F11 |
| A11 | НАРУШЕНО | Inventory переиспользуется, но Prepare request до tracer. | F10 |
| A12 | НАРУШЕНО | CLI overrides только в Run, SDK plan пересчитывается после Freeze. | F05 |
| A13 | КОД ПОДТВЕРЖДЁН | Raw job IDs не truncate; сокращаются display fields. | progress.go |
| A14 | ЧАСТИЧНО | Duplicate/conflict detection есть; accounting unavailable может тихо убрать summary. | F05/F07 |
| A15 | ЧАСТИЧНО | Immediate suite result channel есть; запуск всех Start в начале invocation остаётся. | extsuite/runner.go |
| A16 | ЧАСТИЧНО | Missing-suite terminal runner_error исправлен; policy skip/app-plan completeness не воспроизводились. | extsuite/runner.go |
| A17 | ЧАСТИЧНО | Globally-unmatched обработка есть; complete selected/unmatched ledger и вся матрица не доказаны. | app.go |
| A18 | ЧАСТИЧНО | Anthropic model-param job skipped; отдельная явная parameter implementation-gap запись не подтверждена. | anthropic/runner.go |
| A19 | КОМПОНЕНТ | Pure ETA simulator существует; все репозиторные replay cases локально не запускались. | eta.go |
| A20 | НАРУШЕНО | В actual CLI нет run ETA и revision-aware deadline отображения. | F06 |
| A21 | НАРУШЕНО | Старый bool+duration callback, нет live Waited/Resumes countdown. | F06 |
| A22 | КОМПОНЕНТ | Estimator моделирует groups/global capacity; реальные snapshots не поступают. | F06/F07 |
| A23 | ЧАСТИЧНО | Модель service samples отделена в estimator, но runtime counters/observations не интегрированы. | F07 |
| A24 | ЧАСТИЧНО | Planning не выдаёт fixed percentage; full live monotonic ETA-path отсутствует. | F06 |
| A25 | КОД ПОДТВЕРЖДЁН | Non-TTY heartbeat имеет независимый timer и fake-clock seam. | progress.go |
| A26 | КОМПОНЕНТ | Width-aware pure renderer существует, но actual CLI/golden PTY contract не замкнут. | F06 |
| A27 | НАРУШЕНО | Actual Reporter не знает ширину cells и не использует новый sanitation/layout. | F06 |
| A28 | ЧАСТИЧНО | Mutex сериализует Reporter, но runtime не единый dashboard-writer и пишет под lock. | F06 |
| A29 | НАРУШЕНО | App использует ModeCharDevice вместо действительного is-terminal helper. | F06 |
| A30 | ЧАСТИЧНО | Heartbeat stop/generation guards есть; actual unified renderer lifetime не реализован. | progress.go |
| A31 | НАРУШЕНО | completed — completed осталось; нет target runtime-state/verdict rows в actual UI. | F06 |
| A32 | НАРУШЕНО | Completed раньше deferred trace verification/recommit. | F09 |
| A33 | ЧАСТИЧНО | Flag validation и filter есть; exhaustive log fixtures локально не исполнялись. | cmd/aiqa/log.go |
| A34 | НЕ ПРОВЕРЕНО | Фильтрацию всех outcome/severity/FATAL combinations необходимо отдельно выполнить. | N10 |
| A35 | ЧАСТИЧНО | Follower существует; real --follow не реагирует на intercepted cancellation. | F08 |
| A36 | НЕ ПРОВЕРЕНО | Полная malicious/EOF/schema matrix decoder не исполнялась в этом аудите. | N10 |
| A37 | КОД ПОДТВЕРЖДЁН | Sidecar opt-in,0600,O_EXCL сохранены; полнота forensic данных отдельно F10. | app.go |
| A38 | ЧАСТИЧНО | Successful forensic finalizer обновляет bytes/files одинаково; error branches требуют отдельного теста. | F09 |
| A39 | ЧАСТИЧНО | Old progress safe не полный unicode control sanitizer; full redaction matrix не прогонялась. | F06/F08 |
| A40 | НАРУШЕНО | Normal-run signal codes есть; log signal path сломан; queue timeout не охватывает old pacer wait. | F08/F09 |
| A41 | КОД ПОДТВЕРЖДЁН | Selectors/three targets и model intersection сохранены в исследованных путях. | app.go/main.go |
| A42 | НЕ ПРОВЕРЕНО | Equivalence canonical evidence across concurrency/UI/verbosity нужно подтвердить black-box fixtures. | N12 |
| A43 | НАРУШЕНО | Prepare network не имеет complete trace; aggregate не наблюдает реальные requests. | F07/F10 |
| A44 | ЧАСТИЧНО | Имеющийся CI success, live skipped; вся новая e2e/PTY приёмка не доказана. | CI run 34321791547 |

## 7. Локальное воспроизведение

**Тестированный исходник:** `internal/scheduler/scheduler.go`, Git blob `54dfe227e69b91c0d05d6b97534b0a908e8b6156`. Совпадение проверено `git hash-object`. Код scheduler не изменялся. Созданы собственные tests рядом в изолированном временном Go module.

Команды, действительно выполненные:

```bash
GOTOOLCHAIN=local GOPROXY=off go test -count=10 -v .
GOTOOLCHAIN=local GOPROXY=off go test -race -count=1 -v .
```

Итог: оба регрессионных теста воспроизводят ошибку во всех 10 повторах. Затем добавлены два контрольных теста: обычное освобождение capacity и pre-dispatch artifact refund; оба проходят. Полный локальный контрольный запуск:

```text
PASS TestAuditControlOrdinaryCapacityReleaseWorks
PASS TestAuditControlPreDispatchArtifactRefundWorks
FAIL TestAuditCapacityWaitThenCooldownMustWake
  cooldown expired but waiter did not wake: inflight=0 waiting=1
FAIL TestAuditSpentArtifactBudgetMustNotWait
  spent artifact budget became an unbounded quota_capacity wait
  instead of a terminal budget rejection
```

Race detector в этом запуске не сообщил data race. Это не доказательство отсутствия гонок во всём AIQA. Ошибки tests являются ожидаемым результатом аудита текущего дефектного кода; test exit=1 не следует маскировать.

В пакете воспроизведения есть:

```text
audit_regression_test.go
audit_controls_test.go
run-isolated.sh
scheduler-audit-full.log
scheduler-regression-repeat.log
README.md
```

`run-isolated.sh /path/to/aiqa` проверяет blob файла, копирует только stdlib-only scheduler в temp directory, добавляет tests и не меняет рабочее дерево. Для проверки исправленной версии использовать `--allow-modified`, при этом provenance уже отличается от данного аудита.

Другие findings помечены CODE-CONFIRMED, а не выданы за выполненные subprocess/PTY/live-provider сценарии.

## 8. Дополнительные замечания, не требующие отдельного архитектурного PRD

### 8.1. Queue timeout охватывает не всю admission задержку

[internal/api/client.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/api/client.go) сначала вызывает target-local `waitForPacing`, и лишь затем `acquireScheduler`, внутри которого применяется QueueTimeout. Значит, часть admission wait не ограничена `--queue-timeout`. Объединить его с request-queue lifecycle из §8, не списывать ожидание из HTTP-attempt timeout.

### 8.2. Log display options приняты, но не полностью работают

В [cmd/aiqa/log.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/cmd/aiqa/log.go) `--color` валидируется, но не используется в output, default format жёстко compact. Auto-stdin распознаёт pipe, но не обычный file redirect. Follow reopen немедленно возвращает ошибку при временном отсутствии path во время rotation. Это проверить в N10 integration fixtures; одни unit tests `Follower.Poll` не доказывают correctness filesystem adapter.

### 8.3. Реальные lifecycle IDs и frozen job IDs всё ещё смешиваются

[internal/progress/progress.go](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/internal/progress/progress.go) в ObserveLifecycle вызывает Start/Complete по telemetry OperationID. Они не обязаны быть host JobID. Новый unknown-ID diagnostic полезен для настоящей ошибки плана, но старый forwarding path способен создавать шум из нормальных span events. Нужен явный mapping, а не подавление всех unknown IDs и не включение каждого trace span в progress.

### 8.4. Документация опережает runtime

[docs/monitoring.md](https://github.com/ilyar/aiqa/blob/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba/docs/monitoring.md) говорит об ETA remaining-work simulation и execution frozen binding. Сейчас доказан pure estimator, но не live ETA; доказано повторное использование inventory, но не immutable executable plan. Исправить формулировки на `implemented as component / runtime integration pending`, либо завершить интеграцию прежде чем объявлять поведение текущей версии.

### 8.5. Нет оснований переоткрывать уже решённые пункты

Не возвращать задачу остановки старого planning spinner, не сокращать raw IDs вновь и не откатывать SDK completion на `probe.Map` batch emission. Не требовать в этом audit перехода Google profile на другую API surface: это другой scope, не `tasks/next.md`.

## 9. Минимальный порядок исправлений

| PR / этап | Что исправить | Доказательство завершения |
|---|---|---|
| 1. Scheduler safety | F01, F02; consumers учитывают ambiguous creation и terminal budget exhaustion | Приложенные tests зелёные + remote-create fault fixture |
| 2. Shared resource integration | F03/F04/F11, единый admission wait timeout | Real three-target ledger, opaque reservation, same-origin quota, pre-allocation memory |
| 3. Executable plan | F05, CLI/file/preflight bindings один раз; убрать ненужный native→SDK barrier | Frozen-vs-actual ID equality через app |
| 4. Event completeness | F07/F10, requests/waits/groups/trace owners до discovery | Independent server ledger == trace == execution counts |
| 5. Operator interface | F06/F09, renderer+ETA+real finalization phase | Actual subprocess/PTTY stderr, controlled 61s cooldown, final-write failure |
| 6. Log lifecycle | F08 и log filesystem/display edge cases | SIGINT/SIGTERM/rotation/pipe fixtures |
| 7. Acceptance ledger | N12: все A01–A44 имеют test name, source, result или явное deferred/not verified | `make qa` + monitoring/scheduler acceptance на project toolchain |

Не закрывать deliverable на основании строки "package exists" или summary unit test. Критерий закрытия — наблюдаемый пользовательский сценарий с соответствующими safety invariants.

## 10. Итог для ревью

**REQUEST CHANGES.** Изменения полезные и значительные, особенно parallel targets, общий broker для встроенного HTTP, raw ID fix и immediate SDK completion. Однако полная приёмка `tasks/next.md` сейчас невозможна: две scheduler-проблемы воспроизведены; несколько критических runtime integrations отсутствуют; observed execution metadata и terminal messages могут вводить оператора в заблуждение.

В `tasks/next.md` задачи остаются планом, а не доказательным acceptance ledger. Поэтому здесь нет обвинения в подделанных checked-off строках: речь о том, какие deliverables объективно ещё нельзя признать выполненными.

Репозиторий этим аудитом не изменялся; review/issue/PR не публиковались. Для исправлений приложены воспроизводящие тесты и точные критерии, а не очередная замена спецификации.
