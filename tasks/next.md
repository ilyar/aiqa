# AIQA next: параллельное выполнение, достоверный прогресс и мониторинг

**Статус:** спецификация к реализации, не отчёт об уже внесённых изменениях.  
**Дата:** 2026-09-08.  
**Версия документа:** 1.0.  
**Предлагаемый путь:** `tasks/next.md`.  
**Заменяет:** `tasks/backlog.md` как единственный активный перечень следующего этапа работ.  
**Проверенный baseline:** `e1acadbbcbbcc11b64bc3929343b32ea896e8e2e`, commit от 2026-09-07, `fix: stop planning spinner before progress bar`.  
**Базовые контракты, которые необходимо сохранить:** `aiqa.report.v3`, профильные verdicts, `test=sdk` отдельно от `test=ext-suite`, приватность forensic trace.

## 0. Основание и границы проверки

Документ основан на чтении актуального кода через GitHub, приложенном исходном PRD `monitoring.md`, текущих `docs/monitoring.md`, `docs/trace-contract.md`, `tasks/backlog.md`, а также терминальном примере пользователя. Ссылки на конкретные файлы baseline приведены в конце как [R1]–[R19]. Внешние источники [W1]–[W4] используются только для отдельных API и общепринятых технических механизмов, не подменяют продуктовые требования.

SHA-256 приложенного `monitoring.md`:

```text
70a288df71e65985c9367bbd6ded60bd56292f5a265ceb5ecbff7e010e38c299
```

В этом аудите не выполнялись реальные запросы к провайдерам и не запускался полный `make qa`. В доступном runtime установлен Go 1.23.2, тогда как проект требует Go 1.27; попытка получить checkout для запуска тестов остановилась на недоступности DNS GitHub. Наличие кода и тестов ниже означает статическое подтверждение реализации, не новый результат исполнения. Терминальный фрагмент пользователя не позволяет восстановить исходную трассу, реальные host quotas или весь runtime конкретного прогона.

Маркировка выводов:

- **Подтверждено кодом** — существует конкретный путь исполнения.
- **Выведено из кода** — установленное следствие алгоритма, без live-воспроизведения.
- **Частично** — механизм есть, но требование не закрыто целиком.
- **Новое решение** — нормативное требование этой спецификации.

Слово «должен» ниже обозначает требование AIQA, а не стандарт поставщика API. Числа в UI-макетах — синтетические состояния для согласования дизайна, не benchmark и не результат пользовательского запуска.

## 1. Решение в одном разделе

Следующий этап — не очередная замена progressbar-библиотеки. Необходимо согласовать четыре слоя:

```text
Неизменяемый план проверок и их зависимостей
                    ↓
Общий планировщик запросов + независимые pacing groups
                    ↓
Состояние исполнения + модель оставшейся работы
                    ↓
Один renderer stderr + aiqa log + сохранённый отчёт
```

Основные решения:

1. Независимые targets готовятся и выполняются параллельно. `--concurrency=16` остаётся общим пределом ресурса, а не лимитом 16 для каждого провайдера.
2. Ограничитель допуска работает на уровне физических HTTP attempts. Ожидание очереди, pacing, Retry-After и зависимостей не занимает HTTP-слот.
3. Квота привязана к **pacing group**, а не к имени протокола. По умолчанию используется нормализованный origin; известные общие/раздельные квоты задаются явно.
4. Процент означает долю завершённых заданий. ETA вычисляется отдельно по оставшемуся графу работ, длительности классов запросов, cooldown, ограничениям и активным запросам.
5. Терминал показывает общий результат исполнения и постоянную строку каждого target. `completed — completed`, голые скобки времени и `it/min` удаляются.
6. Существующие status/coverage/verdict не заменяются UX-метриками. `DONE` не равен `PASS`; 100% заданий не означает совместимость.
7. `aiqa log` реализуется как локальный reader существующего безопасного JSONL, без обязательного фонового сервиса, внешних `tail`/`jq` и доступа к API.
8. Текущие defaults файлов и приватности сохраняются; stdout payload добавляется явно, а не скрытым изменением старых команд.

## 2. Что уже реализовано, а что нет

| Область | Состояние baseline | Решение следующего этапа |
|---|---|---|
| OpenAI, Anthropic, Google GenAI targets | Реализованы, есть registry и profile assets [R1, R14] | Не добавлять их повторно |
| Official SDK | Три built-in; отдельный selector `test=sdk` [R1, R8, R9] | Сохранить; подключить к общему admission controller |
| Глобальный `--model` | Пересечение с каждым локальным inventory; отсутствующий ID не создаётся [R11] | Сохранить именно текущую семантику |
| `Prepare → Freeze → Execute` | Реализовано, inventory переиспользуется [R1, R5, R6] | Сохранить стабильность плана, убрать последовательность и рассогласование |
| Параллельность targets | Нет: два последовательных `for` [R1] | Реализовать и для discovery, и для execution |
| Executor | Есть конкурентные probes внутри target, но instances target-local [R5–R7] | Общие leases; immutable scope вместо общей изменяемой target-метки |
| Pacing | Есть interval, learned limit, cooldown, Retry-After, ревизии расписания [R4] | Сохранить адаптацию, исправить области разделения и наблюдение |
| Timeout | CLI обещает per HTTP-attempt после pacing [R9] | Сохранить; явно показывать отдельно от queue wait |
| Прогресс независимо от verbosity | Реализован [R1–R3] | Сохранить |
| Один общий frozen total | Реализован [R2, R3] | Проверять согласованность с реально выполненным планом |
| Счёт logical jobs вместо retries | Реализован, но единицы неоднородны [R2, R5, R8] | Уточнить гранулярность и отделить time model |
| Планирующий spinner | Реализован; последний commit устраняет остаточную перерисовку [R2] | Не включать исправленную ошибку снова в открытый backlog |
| ETA и throughput | Делегированы progressbar; не знают зависимостей/cooldown [R2] | Собственный estimator и явные единицы |
| Per-target dashboard/active counters | Нет: одна изменяемая `current`-строка [R2] | Реализовать из структурированного snapshot |
| Pacing countdown | Нет: максимум длительности, полученной при входе в wait [R2, R4] | `eligible_at`, `waited`, причина, область, реальный countdown |
| CI plain checkpoints | Есть на первом completion/границах 10% [R2] | Сохранить и улучшить формат |
| CI heartbeat раз в 30 секунд | Описан в docs, но таймера non-TTY в Reporter нет [R2, R3] | Реализовать независимо от новых completion events |
| Приватный JSONL | Есть при verbosity 1–4; 0600 и O_EXCL; verbosity 0 без файла [R1, R12] | Сохранить безопасный default; не писать payload на TRACE |
| `aiqa log` | Нет dispatch в main, нет описания команды в CLI [R9] | Реализовать |
| stdout report | Отчёты записываются в файлы, stdout не сериализуется [R1, R3] | Добавить явный `--stdout=json|markdown` |
| `--quiet`, `--no-progress`, responsive UI | В текущем CLI отсутствуют [R9] | Добавить согласованный интерфейс ниже |
| Unresolved coverage | Есть `materializeModuleCoverage`, `coverageForModels`, `applyCoverageVerdict` [R13] | Проверить end-to-end, закрыть оставшиеся дыры, не переписать с нуля |
| Forensic completeness при discovery | Клиент Prepare делает ListModels до подключения logging/telemetry [R5, R6] | Инструментировать с первого запроса, не выдавать неполный граф за полный |

Текущий Google default — GenerateContent-профиль [R18]. Этот этап **не** меняет его обратно на Interactions и не переоткрывает спор о Google API scope.

## 3. Почему наблюдаемое поведение получается именно таким

### 3.1. Последовательность находится выше worker pool

В `runMultiTarget` сначала по одному вызываются preparers, затем по одному выполняются `prepared.Execute` и `finishTargetExtensions`. Только после полного окончания одного target начинается следующий [R1]. Поэтому свободные возможности другого host не используются, пока текущий target ждёт pacing.

Просто запустить существующие closures в goroutines недостаточно: каждый Prepare создаёт собственный `probe.NewExecutor(cfg.Concurrency)` и свой pacer [R5, R6]. При трёх targets этот патч превратит заявленный общий потолок в три локальных потолка.

### 3.2. `(1m1s)` — не таймер до продолжения

`pacer.wait` сообщает `beginWait(estimate)` только при первом входе в ожидание. При изменении расписания второй start-сигнал не отправляется. Reporter хранит максимальную переданную длительность среди waiters, не хранит deadline и не вычитает прошедшее время [R2, R4]. Когда последний waiter уходит, `wait` обнуляется, но строка `current` не обязательно заменяется содержательным running-state.

Следовательно, `(1m1s)` может быть уже устаревшим первоначальным прогнозом очереди. По этой цифре нельзя определить, сколько осталось и был ли причиной именно HTTP 429.

### 3.3. `27/30` — общий счётчик, а не обязательно OpenAI

В baseline один global bar создаётся из `allJobs`; текст слева лишь описывает последнее событие [R1, R2]. Соседство `openai:` и `(27/30)` ошибочно подсказывает, что знаменатель принадлежит OpenAI.

Native `compat` для модели и весь `model-param` могут быть одним job каждый; один SDK suite тоже job. При этом один сетевой запрос может поддерживать несколько schema/usage requirements, а adaptive context job может включать десятки запросов [R5, R8]. Поэтому «осталось 10% jobs» не означает «осталось 10% времени».

### 3.4. SDK-события приходят в UI позже фактического завершения

`ExecuteBuiltinSuites` сначала ждёт возврата `probe.Map` со всеми задачами и лишь затем публикует результаты. Frozen SDK jobs стартуют все при начале invocation и завершаются циклом в `defer` invocation [R8]. Отсюда плато прогресса, затем пачка completion events и искажённая измеренная скорость.

### 3.5. Ещё четыре дефекта/риска, которые нельзя прятать под новым дизайном

**Идентификаторы:** `Freeze` применяет `safe` к `job.ID`, включая усечение до 96 байт, а `Start/Complete` ищут исходный ID. При длинном ID lookup может не совпасть; разные ID с одинаковым префиксом могут схлопнуться. Это следствие кода, не заявленное live-воспроизведение [R2]. Идентификаторы нельзя форматировать функцией для экранных labels.

**Согласованность плана:** OpenAI/Anthropic Prepare классифицируют candidates с overrides из файла, тогда как CLI `ModelCapabilities` дополнительно применяется в Run. Runtime preflight может изменить applicability; SDK execution вновь вызывает `planTarget` по уже изменившимся моделям [R5, R6, R8]. Следовательно, frozen jobs и выполненные jobs могут различаться. Нужен один артефакт плана, а не два независимых пересчёта.

**Весовые leases:** `Executor.DoN` занимает несколько мест последовательными sends в channel, без cancellation. Две операции, требующие по два места при capacity=2, допускают interleaving: каждая заняла одно и ждёт второе. Это алгоритмический риск при конкурентных weighted invocations, не доказательство зависания данного пользовательского запуска [R7].

**Завершение:** сообщение `completed; reports: ...` отправляется до deferred закрытия sidecar, trace verification и повторной записи public report [R1]. UI может обещать завершение, пока программа ещё выполняет значительную работу или позднее обнаружит ошибку записи.

## 4. Критический пересмотр исходного monitoring.md

| Исходное требование | Оценка | Новый нормативный контракт |
|---|---|---|
| stderr UI, stdout payload, файлы audit | Направление верное, default не согласован с уже используемым CLI | Файлы остаются default; stdout только явным флагом |
| Total до первого сетевого запроса | Невыполнимо при auto inventory и adaptive probes | До inventory — indeterminate; после планирования — frozen logical jobs |
| `Targets × Models × Probes × Samples` | Неверная модель для applicability, shared requests, retries и conditional probes | Граф конкретных executable jobs; requests и requirements отдельно |
| `done/total` как процент | Полезно при ясной единице | Только execution progress; не time progress и не conformance |
| SMA последних 20 проб для ETA | Недостаточно при разных моделях, размерах, типах работ, очередях | Классовые оценки service time + simulation расписания + explicit unknown |
| Один progress bar | Уместно для одного target, недостаточно для трёх | Run header + per-target rows + ограниченные details |
| `[3 active workers]` | Неясно: goroutines, probes или HTTP | `HTTP in flight 3/16`; отдельные ready/deferred jobs |
| Всегда писать log | Не соответствует текущему privacy/default; мониторинг не должен требовать TRACE | In-memory progress всегда; sidecar по verbosity 1–4 |
| TRACE содержит raw payload/hex/private traces | Конфликт с принятым forensic allowlist | TRACE остаётся структурным, без секретов, тел запросов, ответов и media |
| `aiqa log` без внешних утилит | Полезно, пока не реализовано | Включить в этап с testable reader/filter/follow контрактом |
| ERROR/FATAL всегда обходят regex | Нарушает ожидания `--target`/`--model` и strict filters | Все явные filters применяются, без скрытого bypass |
| `--failed` = severity ERROR | Severity и outcome не эквивалентны | Явный семантический фильтр failures; `--level ERROR` отдельно |
| Сеть недоступна = FATAL | Не всегда: один target может временно не ответить | Runtime fatal и provider execution error разделены |
| Каждый SKIP окрашен как WARN | Policy skip/N/A не обязательно проблема | Policy skip нейтральный; обязательная неполнота — отдельный сигнал |
| `-v`, `-vv` | В тексте и примерах разные значения | Не менять шкалу runner 0–4; viewer использует именованные уровни |
| Default `report/report.log` или `report/compat-report.log` | Неоднозначность и несовпадение с текущим `<name>.log` | Явный path / `--name`; автоматический выбор лишь при одном кандидате |
| `aiqa test=compat > out.json` даёт JSON | Конфликт с default двух форматов | `--format=json --stdout=json > out.json` |
| SIGINT всегда восстанавливает терминал | Верное управляемое поведение, но SIGKILL не перехватывается | Один owner terminal state, defer + сигналы; никаких обещаний для SIGKILL |

Основные цели исходного документа сохраняются: оператор видит живой процесс, машинный вывод не загрязняется, журнал можно исследовать автономно. Изменяются спорные формулы и интерфейсные детали, а не подменяется задача.

## 5. Переработка tasks/backlog.md

Текущий backlog [R10] содержит две открытые темы и раздел «Lost relevance». Его нельзя просто дополнить UI-задачами.

| Старый пункт | Новый статус | Обоснование |
|---|---|---|
| Honest verdict for unresolved model capability coverage | **P0, завершить и доказать** | Механизмы уже существуют; нужен аудит Requested/selection/preflight/zero-match, а не новая scoring-система |
| Anthropic model parameters/context | **P2, отдельный deliverable** | Прямо связан с `test=model-param` в целевой команде; сначала честно показать unsupported runner scope, затем реализовать отдельный профиль |
| Anthropic caching/document input | **Отложено, отдельный PRD** | Не блокирует параллельность и monitoring |
| Anthropic Batches/Files | **Отложено, safety/cleanup contract** | Удалённые ресурсы и стоимость; не смешивать с UI |
| Messages-only profile | **Отложено, profile requirement** | Не переопределять scope в runtime и не подгонять verdict |
| `aiqa verify`, `explain`, baseline comparison | **Не входит** | Пользователь сейчас запросил мониторинг и следующую спецификацию, а не восстановление всех старых идей |
| HTML/SARIF report, generated explanations, RU guidance | **Не входит** | HTML-макет терминала в поставке этой спецификации — design preview, не новый формат AIQA report |

При применении: создать `tasks/next.md`, удалить `tasks/backlog.md`, обновить ссылки. История старого backlog остаётся в Git. Если нужны внешние ссылки, на один переходный релиз допустим redirect-файл без открытых задач, но двух активных backlogs быть не должно. Приложенный original PRD архивировать как `docs/design/monitoring-original.md`; текущий `docs/monitoring.md` после реализации описывает фактический контракт, а не будущие обещания.

## 6. Обязательная семантика планирования и единиц измерения

### 6.1. Четыре разных счётчика

| Объект | Единица | Входит в execution percentage? |
|---|---|---|
| Requirement | Нормативное ожидание профиля | Нет; это conformance/evidence |
| Job | Конкретный workflow проверки с terminal outcome | Да |
| Logical request | Одна намеренная операция клиента | Нет; отдельная telemetry |
| Attempt | Реальная передача HTTP, включая retry | Нет; отдельные rate/slots/usage |

Job должен соответствовать исполняемой работе, а не количеству assertions. Один response для basic/schema/usage — один workflow job с тремя связями requirements. SDK suite — один workflow job. Независимые native workflows разбиваются на jobs там, где их реально можно планировать. Adaptive context — один composite job с внутренними steps и признаком неизвестного остатка; нельзя выдавать каждый новый binary-search step за неожиданное расширение global total.

### 6.2. RunPlan как единственный источник истины

План содержит `plan_id`, `plan_revision`, targets, model selection, applicability evidence, jobs, зависимости, resource constraints, cost gates и execution bindings. Нормативные profile IDs/requiredness не меняются планировщиком.

```go
type JobSpec struct {
    ID              string
    TargetID        string
    ModelID         string
    Suite           string
    Kind            string // native_workflow | sdk_suite | composite | extension_suite
    RequirementIDs  []string
    Dependencies    []string
    AdmissionGroup  string
    EstimateClass   string
    Composite       bool
    CostClass       string
}
```

ID хранится полностью и неизменно. Ограничение максимального допустимого ID проверяется как ошибка входа, а не реализуется тихим усечением. Укороченные labels живут в view model, не участвуют в lookup/dedup. Дубликат planned ID — ошибка плана до исполнения; повтор одного terminal event с тем же ID допустим как идемпотентная доставка, но противоречащий outcome — диагностическая ошибка.

### 6.3. Фазы

```text
VALIDATING
  → PREPARING (parallel inventories + necessary bounded preflight)
  → PLAN_FROZEN
  → RUNNING (parallel ready targets/jobs)
  → FINALIZING (assessment, reports, trace verification, final commit)
  → COMPLETED | INTERRUPTED | FAILED_RUNTIME
```

До сетевых действий выполнить локальную валидацию profiles, overrides, targets, output destinations. Параллельная подготовка подключает tracer и admission controller **до** первого inventory request. Discovery не повторяется при переходе в Run.

MVP сохраняет общий barrier Freeze после подготовки всех выбранных targets. Это сознательно оставляет ожидание самого медленного discovery, но preparation идёт параллельно и имеет ограничение времени. Streaming freeze по target не входит в MVP: он потребует отдельно согласовать динамический global plan. Нельзя одновременно обещать полный неизменный global total и незаметно добавлять targets после старта.

CLI/file overrides должны быть объединены один раз до классификации. Необходимый explicit-model preflight выполняется в Prepare, с журналом и лимитом, либо заранее резервируется как conditional branch с полным набором terminal reservations. После Freeze SDK исполняет сохранённые invocations, а не заново вызывает независимый `planTarget`.

На входе Execute проверяется plan fingerprint: profile revision, selected models, overrides и module gates совпадают. Перемены требуют явного replan до начала исполнения; тихое расхождение запрещено.

### 6.4. Completion и отмена

`finished_jobs` включает успешно/неуспешно исполненные и явно разрешённые без исполнения jobs (`skipped`, `blocked`). `cancelled_jobs` и abandoned jobs учитываются отдельно и не используются для искусственного доведения прогресса до 100%.

```text
execution_percent = 100 × finished_jobs / planned_jobs
planned_jobs = finished_jobs + running_jobs + queued_jobs + cancelled_jobs
```

`queued_jobs` имеет подкатегории ready, dependency-blocked и delayed; один job не входит сразу в две подкатегории. Request counters — другой уровень и не суммируются с jobs.

Нормально завершённый run разрешает весь план и достигает 100%. Отменённый может остаться на 59%; после этого UI показывает `INTERRUPTED`, а не дорисовывает остаток. При `planned_jobs=0` выводится `No executable jobs`, percentage и ETA равны null, а не PASS/100%.

Отсутствующий terminal result planned SDK-suite после process exit — `runner_error`/incomplete evidence, не фиктивный `completed`. `Start` отображает реальный запуск workflow, а не регистрацию всех будущих suites.

## 7. Параллельность: нормативная модель

### 7.1. Общий предел

`--concurrency=C` определяет общую ёмкость request admission во всём run:

```text
native_inflight + sdk_inflight + opaque_extension_reserved ≤ C
```

Это не число target goroutines, не число открытых HTTP/2 соединений и не requests per second. Десять streams на одном HTTP/2 connection — десять in-flight requests. Slot удерживается до EOF/закрытия body/cancellation, а не только до получения response headers.

`--target-concurrency=auto|N` — новый дополнительный предел одновременно активных target workflows. Default auto = число выбранных targets: активный workflow сам по себе не занимает HTTP slot. Значение 1 разрешает воспроизводить старое последовательное поведение. При C=1 HTTP attempts не перекрываются, но ожидающий pacing workflow не мешает готовому запросу другого активного target. При C≥3 три независимых targets должны начать работу без ожидания полного завершения другого.

### 7.2. Pacing group вместо «protocol = provider»

Origin нормализуется как scheme + lowercase host + effective port. Paths `/openai`, `/anthropic`, `/google` не создают новые origin. Origin используется локально как default ключ общей квоты; в telemetry и UI выводится безопасный alias `group-1`, а не URL/credential-derived ID.

| Конфигурация | Default |
|---|---|
| Разные origins, нет признака общей квоты | Независимые pacing groups, общий global cap |
| Один origin, разные protocol paths | Общий pacer/cooldown/limit |
| Разные origins, общий account/upstream quota | Явное объединение |
| Один origin, подтверждённые независимые quotas | Явное разделение |

Новый repeatable override:

```bash
--pacing-group=openai=shared-gateway \
--pacing-group=anthropic=shared-gateway \
--pacing-group=google-genai=google-independent
```

Одинаковый group alias означает общую квоту. У разных aliases независимые pacing состояния. Секреты не хешируются для определения группы и не используются в публичном ключе. Разные hosts — разумный default независимости, но не доказательство независимости backend quotas.

### 7.3. Admission controller

Создать один run-owned broker, которому принадлежат global capacity, группы, ready queues и deadlines. Клиенты/SDK используют scoped handles с неизменяемыми target/model/job IDs; не переустанавливают общий `SetLogFields` между targets.

Алгоритм допуска:

1. Проверить dependencies, cancellation, token/artifact/memory budgets.
2. Поставить request descriptor в target/group queue без занятия HTTP slot.
3. Рассчитать `eligible_at` по pacing interval, cooldown, backoff и локальному deadline.
4. Выбрать ready request по round-robin между eligible targets, затем stable FIFO внутри target; добавить aging, чтобы длинные jobs не голодали.
5. Под одним scheduler lock проверить все ограничения и **атомарно** выдать нужный lease целиком.
6. Освободить lock; только теперь создать attempt timeout и выполнить network call.
7. При закрытии body освободить lease и уведомить broker; retry становится новым ожидающим attempt того же logical request.

Ожидающий target не резервирует все C слотов. Запрещён порядок `Acquire(global) → sleep(pacing)` и частичное получение weighted slots. `semaphore.Weighted` [W1] может реализовать атомарную резервировку одного ресурса, но сам по себе не решает согласованный допуск global/group/memory; для нескольких ресурсов нужен broker или полностью rollback-safe TryAcquire.

### 7.4. Fairness и адаптация

Пока три targets имеют ready requests и C≥3, каждый получает возможность начать хотя бы один request. Свободные слоты заимствуются активными targets; статическое деление `16/3` не требуется. HTTP 429 одного group не меняет расписание независимого group.

Сохранить осторожный старт существующего pacer: learned in-flight limit начинается с 1 и растёт после quiet successes. UI обязан показывать `configured cap` и `current group limit`, чтобы C=16 не выглядел сломанным, когда реально допущен один запрос. Текущие детали AIMD не считаются доказательством безопасной квоты provider.

Значения rate не умножать на concurrency дважды. Если `interval` — минимальное расстояние между стартами для всей группы, верхний dispatch rate равен `1/interval`, а не `C/interval`. При среднем service time S и capacity L ожидаемый throughput ограничен:

```text
throughput ≤ min(1 / interval, L / S)
```

Текущие `snapshot/constraints` вычисляют `limit/interval` [R4]; в новой реализации это должно быть исправлено или переименовано с изменением семантики. Раздельно хранить observed completions/s, allowed start rate и learned capacity. Не называть их provider guarantee.

### 7.5. Зависимости и смешанные suites

Native, `model-param` и SDK не должны иметь target-wide barriers без причины. Готовый SDK workflow может идти одновременно с независимой native-проверкой. Но tool continuation ждёт конкретный tool call; adaptive binary-search step ждёт предыдущую границу; cleanup ждёт создания ресурса. Параллельность не разрешает менять содержимое эксперимента.

Статистические тесты параметров сохраняют design: одинаковые prompt/control conditions, seed policy, учёт load/cache confounders. Последовательность шагов внутри adaptive job не распараллеливается. Для context-probing default — не более одного high-memory adaptive workflow в pacing group одновременно; обычные небольшие probes других групп продолжаются.

### 7.6. Артефакты и память

`--max-artifacts-per-model=2` — одна общая атомарная корзина на `(target, model)` для native и SDK. Границу нельзя обходить параллельными проверками или retry после неоднозначного timeout. При неопределённом результате remote creation reservation остаётся consumed до подтверждения отсутствия создания; при локальной отмене до dispatch возвращается.

C ограничивает число requests, но не их память. Добавить run-owned byte budget для сериализованных request/буферов, например `--request-memory-budget=128MiB`; parser явно поддерживает binary units. Это новое значение политики, не текущий лимит baseline. Один request больше budget отклоняется локально как budget-constrained work, не как provider incompatibility. Memory reservation получается до выделения крупных payload/serialization buffers, иначе budget ограничит только уже выделенную память. Body replay для retry использует тот же зарезервированный immutable buffer; дополнительные копии учитываются отдельно. Actual budget и peak reservations фиксируются в execution metadata.

### 7.7. Executable extensions

Built-in SDK полностью использует host admission controller. Внешний executable может иметь только объявленную concurrency. AIQA резервирует её атомарно целиком и отмечает `accounting=reserved`, не притворяется, что видит реальную HTTP activity.

Неизвестная concurrency внешнего процесса — exclusive reservation на весь global capacity; значение >C — config error либо кооперативно согласованное снижение с подтверждением extension. Простое clamp без согласия запрещено. Reserve не смешивается с занятием каждого его подзапроса второй раз. Наличие telemetry `attempt.start` от процесса не делает ограничение host-enforced: процесс может не соблюдать протокол.

Unsafe-extension locks поднимаются с target scope на run scope. Честность UI: `HTTP in flight 5; external reserved 4; global 9/16`, без фиктивных четырёх наблюдаемых запросов.

## 8. Ожидания, timeout и жизненный цикл

Каждое ожидание получает безопасный `wait_id`, `job_id`, host-generated `request_id` (не идентификатор provider), `target_id`, `group_id`, `reason`, `started_at_mono`, `eligible_at_mono` (nullable), `source` и `schedule_revision`.

Причины — закрытый versioned enum:

```text
initial_pacing | adaptive_pacing | retry_after | retry_backoff
quota_capacity | global_capacity | memory_budget | dependency
```

`waiting` — не один boolean на target. Target может одновременно иметь 4 in-flight requests и 2 ожидающих: его состояние RUNNING, а details показывают waiters. THROTTLED используется, когда исполнимые запросы задержаны только pacing/quota и ни одного request нет в полёте. WAITING — dependency/capacity, FINALIZING — локальная обработка после requests.

Времена выводятся с названиями:

- `Waited` — сколько длился конкретный текущий wait.
- `Next eligible`/`Resumes in` — неотрицательный countdown до ближайшего допуска, а не обещание окончания всей работы.
- `Elapsed` — wall duration от старта run, включая discovery, pacing и finalization.
- `Request age` — длительность активного attempt; `Timeout left` — его deadline.
- `ETA` — прогноз до завершения текущего scope, включая известные задержки.

`--timeout=120` сохраняет текущий смысл: максимум одного HTTP attempt после admission, включая body/stream. Queue wait не расходует этот budget. При необходимости новый `--queue-timeout` ограничивает admission ожидание одного logical request, default 0 (не ограничен отдельно). `--run-timeout` ограничивает весь run, default 0. Превышение queue/run budget — локальная неопределённость/отмена, не `unsupported` provider.

Retry-After поддерживает delay-seconds и HTTP-date [W2]. Scheduler переводит wall-date в монотонный локальный срок; неотрицательно обрабатывает уже прошедшую дату, логирует источник, не отправляет до актуальной границы. Новая ревизия cooldown немедленно меняет UI и ETA. При выходе из wait запись удаляется адресно по wait_id; не остаётся stale waiting label.

## 9. ETA: отдельная модель оставшейся работы

### 9.1. Что обещает ETA

ETA — оценка, не SLA. У неё есть `scope`, `method_version`, `state`, `estimate`, `range_low`, `range_high`, `reason` и `updated_at`. Состояния:

```text
estimating | estimated | lower_bound | unknown | finalizing | complete
```

При недостаточных данных писать `ETA estimating`, `ETA unknown: adaptive context search` или `ETA >=01:01; work duration unknown`. Не показывать 00:00/14s, если единственный оставшийся target не может стартовать ещё минуту.

Диапазон — **сценарный диапазон прогноза**, не доверительный интервал. Обозначения P50/P90 запрещены для ETA, пока replay-калибровка не подтверждает соответствующую вероятность покрытия. Percentile service duration можно применять внутри модели, не выдавая его за percentile makespan.

### 9.2. Статистика длительностей

Собирать attempt service duration отдельно от queue, pacing и retry delay. Класс:

```text
(target, model, operation_family, input_size_bucket,
 output_cap_bucket, stream, reasoning_mode, executor_kind)
```

MVP хранит rolling window до 64 successful service samples на класс в памяти текущего run. Timeouts — censored observations: не записывать их как успешную длительность; учитывать их как нижнюю границу и сигнал нестабильности. Skip/blocked/validation-only local jobs не ускоряют измеренный request throughput.

Допустимое укрупнение класса: тот же target/model/family с соседним input bucket; затем та же family target. Чужой provider, image и short text не смешиваются. Для ≥8 сопоставимых samples рассчитывать median и сценарные q25/q90; для 1–7 оценка помечается low evidence; без samples и validated profile hint — unknown. Историческая статистика позже opt-in, с TTL/version/provenance, не тихий глобальный cache.

Активные requests требуют оценки **остатка**, а не повторного полного времени. При наличии выборки использовать эмпирическую условную величину `S-a | S>a`, где a — уже прошедший service age. Если request старше всех samples, нельзя обнулить остаток: unknown/timeout-bounded signal и снижение доверия.

### 9.3. Оставшиеся requests внутри job

Для фиксированного workflow известны оставшиеся dependencies и calls. Для sampling известны ещё не выполненные samples. Для adaptive контекста runner публикует текущую фазу (`expanding`, `bisecting`), bounds, precision, used/remaining budget и допустимый interval числа оставшихся шагов.

Формула `ceil(log2(range/precision))` допустима только для уже ограниченного бинарного поиска, где range и precision измерены в одной шкале. Она не описывает незавершённую экспоненциальную фазу и не является универсальным числом токеновых шагов.

Opaque extension без сопоставимых исторических samples — неизвестный остаток. Declared timeout является потолком допустимого ожидания, а не прогнозом нормального исполнения.

### 9.4. Прогноз расписания

Estimator получает immutable snapshot того же broker: running requests, ready/dependency queues, leases, current pacing limits, cooldown deadlines и remaining work. Для low/base/high сценариев симулирует list scheduling с той же политикой fairness, **не создавая реальные requests**.

Для eligible job/request j:

```text
start_j = max(now,
              finish_of_dependencies,
              next_dispatch_of_group,
              cooldown_of_group,
              availability_of_global_and_group_capacity)
finish_j = start_j + estimated_service_j
```

Внутри каждого сценария освобождаются те же ресурсы по finish_j; independent groups накладываются во времени. Для полностью независимых targets без конкуренции за global slots:

```text
ETA_run ≈ max(ETA_target_i) + remaining_finalization
```

При global resource contention используется simulation, не сумма ETA и не безусловный max. Shared cooldown учитывается один раз на расписание группы, а не прибавляется заново каждому параллельному waiter.

Физические нижние границы должны соблюдаться. Если весь remaining work блокируется одним cooldown, ETA не меньше оставшегося cooldown. Это утверждение не применяется к target, у которого остаётся только завершить уже активные requests: cooldown не останавливает текущие streams.

### 9.5. Обновление и сглаживание

State snapshot обновляется событиями; UI tick — 250ms максимум 4 fps, estimator пересчитывается не чаще 1 раза/сек и при существенном изменении планировщика. Запрещена зависимость численных метрик от того, сколько раз terminal перерисовался.

Можно сглаживать прогнозируемый absolute finish time EWMA с half-life 10s внутри стабильного режима. Новые Retry-After, plan errors, изменение remaining adaptive bounds или переход стадии обходят smoothing немедленно. Отображаемое значение никогда не сглаживается ниже известной нижней границы. ETA округляется: <2m до секунды; 2–10m до 5s; >10m до 30s. Elapsed и короткие countdown отображаются посекундно независимо от округления ETA. ETA может увеличиться, если появились новые доказательства; его нельзя насильно заставлять монотонно убывать.

Если upper bound неизвестен у возможного critical-path job, whole-run ETA становится unknown/lower_bound, даже если остальные targets имеют хороший прогноз. Локальные ETA этих targets остаются видимыми.

### 9.6. Проверяемые расчёты

**Сценарий A: три независимых host.** По 10 запросов длительностью 1s, один in-flight на каждую группу, нет start pacing. При C=16 completion каждой группы 10s, общего run до finalization — 10s. Последовательный запуск займёт 30s. Это детерминированная fixture, не обещание ускорения реальных провайдеров втрое.

**Сценарий B: один HTTP slot.** Те же 30 requests при C=1 требуют 30s. `max(10,10,10)` здесь неверен, потому что есть общий ресурс.

**Сценарий C: похожий на пользовательский хвост.** Остались 3 serial requests; cooldown 61s, service каждого 5s, minimum start interval 4s, finalization 2s. Допуски 61, 66, 71; окончания 66, 71, 76; ETA run 78s. Счётчик `3 / (12 jobs/min)` дал бы 15s и проигнорировал cooldown. UI показывает около `01:18`, а не `14s`.

**Сценарий D: тот же cooldown, группа допускает 3 concurrent requests.** Старт 61, 65, 69; finish 66, 70, 74; с finalization ETA=76s. Так проверяется, что интервал стартов и concurrency — разные ограничения.

**Сценарий E: неизвестный context остаток.** Есть cooldown 61s, но ещё не найден верхний context bound. Корректно `ETA >=01:01; context search remaining unknown`. Не разрешено превращать бюджет 8M токенов в известное время.

### 9.7. Критерий качества estimator

В CI обязательны replay fixtures с известным расписанием, без live API. Проверяются точные детерминированные значения A–D, корректный unknown E, lower-bound invariants, отсутствие отрицательных времен и мгновенная реакция на cooldown.

Для последующей оценки на реальных traces собирать median absolute error, долю недооценок >2×, ошибку за 60s до финиша и частоту смены estimated↔unknown. Не вводить обещание «ETA точен до 10%» без набора данных. Эта метрика не влияет на conformance score.

## 10. Терминальный дизайн: визуальный контракт

### 10.1. Принципы

Терминал — read-only status dashboard, не fullscreen приложение. По умолчанию не перехватывает stdin, не включает raw mode, не использует alternate screen. Работает в scrollback и завершается устойчивым summary. Вся запись в stderr идёт через одного renderer owner; worker goroutines не печатают поверх панели.

Основной экран отвечает на вопросы: что происходит, кто работает, кто ждёт и почему, сколько осталось по оценке, что уже завершилось, записаны ли артефакты. Не показывает body/prompt/completions/URLs. Имя отчёта и safe IDs допускаются.

UI language первого этапа — English как текущий CLI. Текст этой спецификации русский; новый locale subsystem не требуется.

### 10.2. Экран ≥110 колонок: три targets одновременно

```text
AIQA  baseline-compat-report                                            RUNNING
Jobs 86/146 (59%)  [############--------]   Elapsed 04:12   ETA ~03:10
HTTP in flight 9/16   Ready 12   Deferred 4   Targets running 3/3

TARGET         STATE        JOBS      HTTP   READY   ETA        NEXT ELIGIBLE
anthropic      RUNNING      28/50       4       5    ~00:55     now
google-genai   RUNNING      31/46       5       7    ~00:40     now
openai         THROTTLED    27/50       0       0    ~03:10     01:01

openai: 4 requests deferred | HTTP 429 / Retry-After | waited 00:08
Critical path: openai | ETA range 02:40-04:30 | estimate: low evidence
```

Эти 59% относятся к jobs, не к времени. `Ready/Deferred` в строке HTTP — request queues, а `JOBS` — workflows; headings и JSON keys различают units. Параллельные jobs не считаются «active HTTP», пока не прошли admission.

### 10.3. Хвост прогона из пользовательского сценария

Разбиение jobs по targets ниже специально условное: из исходного вывода оно неизвестно.

```text
AIQA  baseline-compat-report                                          RUNNING
Jobs 27/30 (90%)  [##################--]   Elapsed 09:03   ETA ~01:18
HTTP in flight 0/16   Ready 0   Deferred 3   Targets finished 2/3

TARGET         STATE        JOBS      HTTP   ETA        RESULT
anthropic      DONE         10/10      0      --        PASS
google-genai   DONE          8/8       0      --        PASS-WITH-GAPS
openai         THROTTLED     9/12      0     ~01:16     pending

openai: resumes in 01:01 | waited 00:08 | group group-3
Reason: HTTP 429 / Retry-After | current group limit 1 | global cap 16
Run ETA includes cooldown and finalization; estimate range 01:12-01:35
```

Нельзя выводить такой `Reason` лишь из boolean waiting. При обычном стартовом interval причина должна быть `local adaptive pacing`, не выдуманный 429. При capacity wait без срока — `next eligible unknown; group capacity busy`, а не `(0s)`.

### 10.4. Во время planning

```text
AIQA  baseline-compat-report                                         PREPARING
Elapsed 00:04   ETA estimating   HTTP in flight 2/16

TARGET         DISCOVERY       SELECTION
anthropic      ready           1 selected / 8 discovered
google-genai   fetching         waiting for inventory
openai         fetching         waiting for inventory

Planning executable work; total is not known yet.
```

До Freeze нет 0/0, 100%, «остаток 0s». Если discovery timeout достигнут, показывается ошибка конкретного target; другие подготовленные evidence сохраняются.

### 10.5. Adaptive context job

```text
openai  RUNNING  41/42 jobs   HTTP 1   ETA unknown
  gpt-5.4-mini / context window: bisecting
  accepted >=128k | rejected <=160k | precision target 512 tokens
  steps 9 | prompt budget 2.1M / 8.0M | request age 00:06
```

Bounds маркируются как exact/estimated по данным runner. Если upper не найден, писать `upper bound not found`; не создавать числовой local percentage из расхода budget. Budget progress — деньги/лимит, не степень завершения теста.

### 10.6. Finalization

```text
AIQA  baseline-compat-report                                       FINALIZING
Jobs 146/146 (100%)   Elapsed 07:48   ETA estimating
Requests complete. Verifying private trace and committing report files...
```

После всех успешных закрытий/commit:

```text
AIQA completed in 07:52  |  Run verdict PASS-WITH-GAPS  |  Exit 0
Targets 3/3 finished  |  Jobs 146/146  |  Requests 238  |  Retries 4
Reports: baseline-compat-report.md, baseline-compat-report.json
Private diagnostics: enabled  |  Trace integrity PASS
```

`Run verdict`, `Trace integrity`, `Exit` — разные поля. Несовместимый provider с полностью завершёнными jobs может дать `DONE / FAIL`, а не runtime ERROR. Отсутствие forensic tracing отображается `not requested`, не PASS.

### 10.7. Отмена

```text
AIQA INTERRUPTED  |  Elapsed 04:12  |  Jobs 86/146
Cancelling 9 in-flight requests; pending work will not be started.
Writing partial report...  ETA unavailable
```

Финал прерывания сообщает partial artifacts и cancelled work. Не ставить `completed` и не менять finished count после факта, заполняя все jobs фиктивными skips.

### 10.8. Узкий терминал 60–109 колонок

Две строки summary, по две строки target при необходимости. Удаляются throughput, range и secondary counts, но остаются target/state/done/ETA и причина ведущей задержки.

```text
AIQA RUNNING  27/30 jobs (90%)
Elapsed 09:03 | ETA ~01:18 | HTTP 0/16
anthropic     DONE       10/10  PASS
google-genai  DONE        8/8   PASS-WITH-GAPS
openai        THROTTLED   9/12  ETA ~01:16
  Retry-After: resumes in 01:01; waited 00:08
```

<60 колонок — compact block без таблицы, например:

```text
AIQA 27/30 jobs | 90%
Elapsed 09:03 | ETA ~01:18
HTTP 0/16 | targets 2/3 done
openai THROTTLED
Retry-After: resumes in 01:01
```

Завершённые targets при таком размере перечисляются в итоговом summary. Новая ошибка остаётся видимой, даже если панель урезана по высоте.

### 10.9. Non-TTY / CI

Без ANSI, `\r`, spinner, cursor hide и динамических перезаписей. Start/phase/target-end немедленно; periodic heartbeat каждые 30 секунд; 10% milestones дополнительно с debounce не чаще 1 строки/сек на run.

```text
aiqa run=7f1a phase=preparing targets=3 concurrency=16
aiqa run=7f1a phase=running jobs=86/146 percent=59 elapsed=04:12 eta=~03:10 inflight=9/16
aiqa run=7f1a target=openai state=throttled reason=retry_after next_eligible=01:01 waited=00:08
aiqa run=7f1a heartbeat phase=running jobs=86/146 elapsed=04:42 eta=~02:40 inflight=0/16
aiqa run=7f1a phase=finalizing jobs=146/146 elapsed=07:48 eta=unknown
aiqa run=7f1a phase=completed elapsed=07:52 verdict=pass-with-gaps exit=0
```

Нельзя ждать completion, чтобы напечатать heartbeat: именно затишье является причиной его необходимости.

## 11. Renderer: точные требования реализации

1. Default `--progress=auto`; варианты `auto|tty|plain|off`. `--no-progress` — alias off. `--quiet` скрывает operator progress и обычный summary, но не фатальную ошибку и не явно запрошенный stdout payload. Verbosity не включает/выключает UI.
2. TTY определяется `term.IsTerminal(int(os.Stderr.Fd()))`, а не `ModeCharDevice`. `GetSize` — текущая ширина/высота [W3]. `/dev/null` character device не равен terminal. `CI=true/1` и `TERM=dumb` отключают interactive mode в auto.
3. `--progress=tty` на non-TTY не должен засорять файлы control bytes: fallback plain с однократной диагностикой. Non-TTY всегда без ANSI.
4. Цвет: `--color=auto|always|never`; в auto непустой `NO_COLOR` выключает цвет. `NO_COLOR` не требует отключать корректную TTY cursor navigation, а `--progress=plain` отключает её полностью. Для screen reader — plain.
5. Palette: основной текст terminal-default, labels bold, running — cyan/blue, PASS зелёный, FAIL/ERROR красный, THROTTLED/WARN янтарный, SKIP/N/A нейтральный. Progress bar никогда не становится зелёным «прошло», пока это лишь работа. Без цвета смысл тот же.
6. ASCII обязателен, Unicode по `--unicode=auto|always|never`; auto только при UTF-8 locale. Ширина считается в terminal cells с grapheme clusters, не `len(bytes)` и не количеством ANSI bytes.
7. Full width ≤ terminal width−1, чтобы не вызвать autowrap. Длинные model labels усекать в середине с сохранением отличимого suffix, полный ID доступен в log/report. Raw ID не меняется. Удалять C0/C1/ESC/OSC и опасные bidi controls из labels.
8. Порядок rows стабилен — configured target order или один canonical sort, не sort по текущей скорости. Переходы RUNNING/THROTTLED не прыгают между строками. Details держать не более 3 активных причин, важные errors не скрывать.
9. До 4 fps при активных изменениях; countdown раз/сек. Один ticker owner на всё lifecycle; никаких оставшихся spinner goroutines после Freeze. Idle UI не должен занимать ядро.
10. Refresh подготавливается вне state lock. Медленная запись stderr не держит scheduler/HTTP locks. Snapshot можно coalesce, но нельзя потерять terminal event в state store.
11. Resize проверяется через SIGWINCH или переносимый polling; после уменьшения размера чистятся ровно предыдущие строки. Нет полного clear screen. В низком окне остаётся run header + summary самого важного blocker.
12. `Close` идемпотентен; одна newline после финала; restoring cursor/attributes на return, recoverable panic, SIGINT/SIGTERM. SIGKILL нельзя перехватить; это явно не часть гарантии.

Рекомендуемая реализация: собственный небольшой pure `Render(snapshot, width, mode)` плюс terminal writer на `golang.org/x/term` и `github.com/rivo/uniseg`, уже присутствующих в dependency graph [R14]. `progressbar/v3` можно убрать после parity tests. Новый full-screen framework не нужен для read-only панели; если он предлагается позже, это отдельный ADR с terminal contract, а не обязательная зависимость ради косметики.

## 12. Событийный контракт мониторинга

UI-state не извлекается из свободного текста logs и не зависит от уровня `--verbose`. Вводится типизированная in-memory шина с single reducer; forensic logger подписывается на разрешённые поля. Внутренний `aiqa.execution.v1` snapshot не требует обязательного нового файла для каждого запуска.

Минимальные event families:

```text
run.phase_changed        target.phase_changed
plan.frozen             job.queued / started / progress / finished / cancelled
request.queued          request.admitted / headers / completed / cancelled
wait.started            wait.updated / ended
group.limits_changed    group.cooldown_changed
artifact.reserved       artifact.committed / released
report.write_started    report.committed
trace.verify_started    trace.verify_finished
```

Базовые поля: run ID, seq, UTC timestamp, monotonic offset, target ID, job ID, attempt ID, group alias, phase/outcome. Поля дельт перечисляются в typed structs. Каждая мутация state выполняется один раз; duplicate terminal event не увеличивает счётчик.

Пример безопасного wait event:

```json
{
  "schema_version": "aiqa.execution.v1",
  "event": "wait.updated",
  "run_id": "run-example",
  "target_id": "openai",
  "job_id": "native:openai:gpt-5.4-mini:chat.stream",
  "wait_id": "wait-12",
  "group_id": "group-3",
  "reason": "retry_after",
  "waited_ms": 8000,
  "eligible_in_ms": 61000,
  "schedule_revision": 4,
  "source": "retry_after_seconds"
}
```

`eligible_in_ms` — snapshot value, для последующих тиков reducer хранит monotonic deadline. Event time UTC нужен для корреляции, monotonic offsets — для длительностей; нельзя рассчитывать wait elapsed через меняющиеся часы системы.

Совместимость с OTel: существующие trace/span IDs переиспользуются; log severity, timestamps и outcome не сливаются в одно поле [W4]. Snapshot не является обещанием экспорта OTLP. Внешняя propagation остаётся выключенной, если пользователь не разрешил exact origins.

## 13. Forensic trace: сохранить и закрыть пробелы

Действующий allowlist [R12] важнее предложения исходного PRD писать raw payload. Добавление monitoring не разрешает логировать API keys, body, prompts, ответы, tools, URLs и media ни на одном verbose level.

Target lifecycle должен начинаться до Prepare, а не после Freeze. Discovery создаёт полноценную ветвь:

```text
run → target → provider → suite(discovery) → probe(models.list)
                                      → logical request → attempt
```

На shared scheduler контекст очереди/slots наследуется от request/probe; запрещён `context.Background()` с потерянным target scope, как в текущем Executor.DoN [R7]. Branches разных targets имеют независимые spans; root run заканчивается после завершения всех ветвей. Schedule/control events могут иметь собственную служебную ветвь с явно разрешённой иерархией.

Completeness test сравнивает **известный mock-server request ledger** с trace attempts, включая discovery, preflight, SDK и retries. Сам по себе PASS offline verifier только говорит, что полученный граф непротиворечив; он не доказывает, что producer не пропустил целые requests. Это различие обязательно сохранить в docs.

Формат trace v1 не переопределять. Если новые event fields укладываются в документированное расширение allowlist/schema, добавляются fixtures и schema revision. Если меняются смысл обязательных полей/иерархии или reader не может отличить старые записи — новая версия trace и явный migration adapter; не называть несовместимую запись v1.

Ошибка forensic disk write не меняет provider verdict, но делает artifact incomplete и runtime exit 2. Нельзя тихо drop tail. При oversized logs предусмотреть ограничение на record и явную диагностику write failure; произвольный trim forensic trace без INCONCLUSIVE запрещён.

## 14. aiqa log: новый, ограниченный продуктовый контракт

### 14.1. CLI

```text
aiqa log [FILE|-] [--name NAME] [--follow]
         [--level TRACE|DEBUG|INFO|WARN|ERROR|FATAL]
         [--only-level LEVEL[,LEVEL]]
         [--failed] [--status STATUS[,STATUS]]
         [--target ID] [--model ID] [--job ID] [--event REGEX]
         [--format table|compact|json] [--color auto|never|always]
```

Level default INFO. `--only-level` и явный `--level` взаимоисключающие; parser выдаёт ошибку вместо неявного приоритета. Runner shorthand `-v..-vvvv` в log v1 не переносится: именованные уровни исключают конфликт с исходным PRD и текущими 0–4.

`--failed` означает исходные failure outcomes (`fail`, `assertion_failed`, `execution_error`, runtime `error`) либо severity ERROR/FATAL. Валидационный тест, успешно ожидавший 401, не считается failed лишь из-за HTTP code. Для чистой severity-фильтрации есть `--level ERROR`. `--status` применяет exact outcome/status; несколько фильтров соединяются AND, кроме явно документированного OR внутри --failed.

При `--failed` без явного `--level`/`--only-level` неявный INFO-порог не применяется: failure outcome должен быть видим даже в DEBUG/TRACE-записи. При явно заданном уровне severity filter применяется дополнительно через AND.

Scope и event filters не обходятся никакими levels. Команда `--target=openai --model=X` не печатает ошибки другой модели «для пользы пользователя».

### 14.2. Источник

Порядок разрешения:

1. Явный FILE, или `-` для stdin.
2. `--name NAME` → `./NAME.log`.
3. Нет файла, stdin — pipe/FIFO с данными → stdin. `/dev/null` не считать содержательным pipe.
4. В интерактивном stdin без arguments: ровно один `*.log`, распознаваемый как AIQA trace в cwd → открыть его; ноль → сообщить, что log не создан; больше одного → вывести имена и попросить явный selection через CLI error.

Не брать автоматически «самый новый» приватный журнал и не искать произвольные `report/report.log`, которых runner не создаёт. Не вычислять путь из приватного ключа. Сохраняется нынешний путь `<name>.log`.

### 14.3. Follow

Native reader на Go, без внешних программ. Для файла использовать polling с 200ms default; читать только добавленные bytes. Неполная последняя JSON line удерживается до newline, а не считается malformed на каждом poll. Maximum record 1MiB; для input со слишком длинной строкой — одна диагностика и discard до следующего delimiter, exit code при batch mode non-zero.

При inode replacement/recreate показать `source rotated`, завершить старый остаток, открыть новый файл после проверки типа/permissions. При truncate — явный `source truncated`, reset offset; не дублировать уже показанные records незаметно. Rotation не должна смешивать run IDs в одну временную шкалу. По умолчанию follow продолжается до Ctrl+C; не завершается на `run.end`, потому что trailing diagnostics могут появиться позднее.

Stdin читается до EOF; `--follow -` не запрашивает polling file semantics. Closed pipe/EPIPE прекращает reader корректно. Не хранить весь file в памяти. Отсутствующий logfile для follow — понятная ошибка, не бесконечное молчаливое ожидание по случайному имени.

### 14.4. Форматы и безопасность

TTY table:

```text
SEQ   TIME          LEVEL  TARGET        JOB                    OUTCOME  DETAIL
412   21:14:02.031  INFO   openai        chat.stream            pass     service 00:02
413   21:14:02.070  WARN   openai        context-window         --       retry_after; next 01:01
414   21:14:02.088  ERROR  google-genai  sdk.generate-content   error    transport timeout
```

Это представление записей, не пересчёт compatibility verdict. Новые outcome columns выводятся только при наличии поля; viewer не угадывает их по английскому message.

`compact` — по одной safe line. `json` — один валидный JSON object на строку, без ANSI/headers. Unknown schema version не интерпретируется как v1; явно сообщается `unsupported schema`, без загрузки URL или выполнения instructions из logs. Control sequences из прочитанного файла экранируются во всех человекочитаемых форматах.

Viewer не восстанавливает события, которые runner не записал: `--level TRACE` не повышает глубину уже созданного журнала verbosity1. Если для нужного события данных нет, вывод сообщает о доступном уровне/отсутствии записи, а не генерирует её.

TRACE-file безопасен по своему контракту, но reader всё равно не должен делать input источником terminal injection. Сеть, shell execution и изменение исходного журнала в `aiqa log` отсутствуют.

## 15. IO и совместимость команд

### 15.1. Сохранить default

Точная пользовательская команда остаётся допустимой:

```bash
aiqa --verbose 4 --concurrency=16 --timeout=120 \
  test=compat,model-param,sdk \
  --model=claude-sonnet-5,gemini-3.8-flash,gpt-5.4-mini,gpt-image-2 \
  --module=auto,images-generations \
  --allow-billable-artifacts \
  --max-artifacts-per-model=2 \
  --format=markdown,json \
  --name=baseline-compat-report
```

Активные targets по-прежнему определяются окружением. Общая модельная selection пересекается с inventory каждого target, а не маршрутизируется по префиксу имени. Если gateway одного target публикует все эти IDs, проверяются все применимые IDs именно этого target. В mock acceptance три независимых inventory публикуют соответствующие им модели; fake IDs имеют тестовые ответы и не требуют реальных моделей.

`--verbose 4`/`--verbose=4`/`-vvvv` сохраняют текущую шкалу. `test=sdk` не переписывается в ext-suite, не требует `--ext-suite=official-sdk`. Старые советованные команды из предыдущих PRD не подменяют текущий CLI.

Результаты default: `.md`, `.json`, при verbosity>0 `.log`, при level4 `.trace-verdict.json`. Existing O_EXCL для private sidecars сохраняется: одно и то же имя нельзя тихо использовать повторно, пользователь выбирает новое имя/явно ротирует файл. Создавать новый logfile по hidden fallback имени запрещено.

### 15.2. Stdout как явная опция

Добавить `--stdout=json|markdown`, default отсутствует. Формат должен входить в `--format`; при ошибке — fail config до API requests. stdout содержит ровно один выбранный отчёт. Два формата одновременно в stdout запрещены. Файлы продолжают записываться по текущему контракту, stdout — дополнительный явно запрошенный sink.

```bash
aiqa test=compat --format=json --stdout=json > result.json
```

UI определяет TTY по **stderr**, а не по stdout: если stdout перенаправлен, но stderr остаётся TTY, интерактивный UI допустим. Если `2>progress.log`, он plain. Для совсем тихого pipeline добавить `--progress=off`.

Serialization выполняется после финального forensic attachment, чтобы stdout и file не отличались статусом pending/final. Никаких debug lines, path banners, progress или JSONL в stdout отчёта.

### 15.3. Новые параметры — сводка

| Параметр | Default | Назначение |
|---|---|---|
| `--target-concurrency=auto|N` | auto | Предел активных target workflows; не заменяет global HTTP cap |
| `--pacing-group=TARGET=GROUP` | origin grouping | Явная общая/раздельная квота |
| `--progress=auto|tty|plain|off` | auto | Рендер progress |
| `--no-progress` | false | Alias off |
| `--quiet` | false | Минимальный human output |
| `--color=auto|always|never` | auto | Цвет, не scheduler |
| `--unicode=auto|always|never` | auto | Сетка и glyphs |
| `--stdout=json|markdown` | нет | Один machine payload |
| `--queue-timeout=DURATION` | 0 | Ограничение admission ожидания |
| `--run-timeout=DURATION` | 0 | Общая длительность run |
| `--request-memory-budget=SIZE` | 128MiB | Общий бюджет in-memory requests |

Флаги не должны расширять набор выбранных probes или включать media/high-token checks скрытно.

## 16. Отмена и finalization

Каждый blocking acquire/timer уважает caller context. Отмена queued request удаляет его reservation и не сдвигает nextStart на несуществующий request. Running stream closes response body, высвобождает global/group/memory permits. Ни одна операция не ждёт освобождения channel slot на `context.Background()`.

Первый SIGINT/SIGTERM переводит run в stopping, запрещает новый dispatch и начинает bounded cleanup. Второй сигнал прекращает ожидание cleanup; cursor/terminal reset выполняются best-effort. Для owned stateful resources cleanup использует отдельный ограниченный context после основной отмены и учитывается admission controller, не бесконечный Background.

Partial reports сохраняют finished evidence, cancellation, незапущенные jobs и coverage gaps. Продолжать другой target после provider timeout нормально; внутреннее повреждение плана/небезопасный output destination — global runtime failure.

Execution scope и conformance score отделены: пользовательская отмена не доказывает provider FAIL. Предлагаемая CLI конвенция — exit130 на SIGINT, exit143 на SIGTERM, exit2 на runtime/config/write failure, 0/1 остаются verdict policy. Это **новая явно документируемая** конвенция, не текущий baseline; добавить compatibility note и regression tests. `--fail-on=never` не превращает abort/write failure в exit0.

UI `COMPLETED` разрешён только после закрытия target branches, assessment, serialization, trace close/verify, final report commit и обработки ошибок. Пока выполняется trace verification или переписывается public attachment, фаза FINALIZING. Напечатанный path не означает успех файла до проверенного close/rename.

## 17. Coverage: завершить задачу backlog без потери уже сделанного

В baseline unresolved assessment уже существует [R13]. Нормативный gap следующего этапа: **selected scope** должен быть сохранён ещё до capability routing и не исчезать, если ни один модуль не был распознан/запрошен.

Хранить отдельно:

```text
requested_model_ids
matched_models_by_target
unmatched_model_ids_across_run
selected_but_unclassified_models
selected_and_evaluated_models
inventory_only_models
```

`Requested` module и selected model — разные оси. Unknown-capability модель не становится N/A только потому, что classifier не выбрал chat. Если пользователь запросил compat для неё, но core generation applicability осталась неизвестной, оценка target INCONCLUSIVE/rankable=false, когда нет уже доказанного core FAIL. Явный core FAIL сохраняется выше unknown, но неполнота остаётся видимой.

Модель, отсутствующая только у одного из нескольких targets, не вызывает обязательного failure этого target: глобальный `--model` — filter. Если ни один requested ID не найден нигде, run должен явно сообщить `NO_MATCH / no evaluated requested scope`, не PASS. Не подставлять синтетические IDs и не тестировать unknown provider маршруты без согласованного preflight.

Для `model-param` на Anthropic и других ещё не реализованных surfaces — честная локальная пометка `not implemented by AIQA`, а не `UNSUP` provider и не завершённый PASS. Это отдельный gap implementation coverage; существующие unrelated native compat results остаются действительны.

## 18. Схемы и совместимость

Public compatibility report остаётся `aiqa.report.v3`, если изменения действительно additive и старые значения сохраняют смысл. Новые execution поля добавлять отдельным optional объектом `execution`, а не перегружать `summary.score` или `usage`.

Предлагаемые данные:

```json
{
  "execution": {
    "schema_version": "aiqa.execution-summary.v1",
    "phase": "completed",
    "elapsed_ms": 472000,
    "global_request_capacity": 16,
    "observed_peak_inflight": 11,
    "planned_jobs": 146,
    "finished_jobs": 146,
    "cancelled_jobs": 0,
    "pacing_group_count": 3,
    "scheduler_policy": "fair-target-admission/v1",
    "estimate_method": "remaining-work-simulation/v1"
  }
}
```

Значения примера синтетические. Длительности overlapping waits не складывать и не называть wall elapsed. Отдельные поля `sum_request_wait_ms` и `wall_time_with_waiters_ms` допустимы, но с явным смыслом.

Перед добавлением optional объектов проверить `additionalProperties` действующих schemas и реальных consumers. Release note обязан назвать, что старый validator с закрытой схемой требует обновления; нельзя обещать всем consumers безусловную обратную совместимость. Если потребуется изменить существующий смысл/required fields/status enum public report — bump report schema. ETA snapshot не обязан попадать в финальный public report; для качества estimator достаточно приватного safe event stream и агрегатов.

Forensic trace и executable contract имеют свои версии. Не расширять `aiqa.ext.v2` несовместимыми mandatory events ради красивого dashboard; opaque progress допустим и должен быть честно помечен.

## 19. Порядок реализации: новый активный backlog

Все пункты ниже имеют статус **OPEN / proposed**, кроме явно отмеченного сохранения существующих механизмов. Закрывать по тесту и артефакту, не по наличию нового пакета.

| ID | Приоритет | Deliverable | Зависит от |
|---|---|---|---|
| N01 | P0 | Зафиксировать regression fixtures, ID invariants, evidence ledger, убрать truncation internal IDs | — |
| N02 | P0 | Один RunPlan: overrides/preflight/native/SDK bindings; frozen/actual equality | N01 |
| N03 | P0 | Run-owned admission broker, atomic cancelable leases, pacing groups, memory/artifact budget | N01 |
| N04 | P0 | Parallel Prepare и Execute без нарушения global cap; deterministic reports | N02, N03 |
| N05 | P0 | Immediate SDK result delivery, real job lifetime, подготовка в forensic trace | N02, N03 |
| N06 | P0 | Coverage end-to-end для unknown/no-match и честная поддержка model-param | N02 |
| N07 | P1 | Typed execution state и live wait/deadline snapshots | N03, N05 |
| N08 | P1 | ETA estimator, replay fixtures и независимый heartbeat | N02, N07 |
| N09 | P1 | Responsive terminal dashboard + pure layout snapshots | N07, N08 |
| N10 | P1 | Reader/filter/follow `aiqa log`, schema safety | N07 |
| N11 | P1 | stdout opt-in, cancellation/finalization contract и final artifact commit | N04, N05, N09 |
| N12 | P1 | Docs migration, full deterministic acceptance, dependency/security check | N01–N11 |
| N13 | P2 | Отдельный Anthropic model-param/context PRD и bounded implementation | N06, N12 |

### N01 — не спрятать старые дефекты

Добавить тест IDs длиной 97/128/512 bytes, одинаковый 96-byte prefix, Unicode границы; unknown completion не молча игнорировать. Проверить fixed total, late plan revisions, spinner shutdown из текущих тестов [R15]. Long labels не влияют на lookup. Зафиксировать разделение job count и requirement count.

### N02 — план действительно исполняется

`PreparedTarget` хранит resolved overrides, classification, selections и exact SDK invocations. Убрать независимый второй пересчёт planned suites. Добавить `ValidatePlan` до API execution. Выполненное множество job IDs — подмножество frozen plan; все reserved conditional jobs получают явный terminal outcome. Ранний config error не должен происходить после уже платных probes другого target.

### N03–N04 — scheduler раньше обещаний ускорения

Сначала реализовать shared broker и scoped handles, потом распараллеливать target closures. Не накладывать shared executor поверх target-local и ожиданий, получая два уровня occupied slots. Prepare и execution используют один group policy. При provider failures сохранять независимые результаты; report rows собираются по индексу, а не append из нескольких goroutines.

### N05 — realtime completion и completeness

SDK callback публикует terminal event сразу после local validation конкретного suite, пока его span ещё активен. Сортировка переносится на финальный report, не задерживает UI. Deferred end закрывает только действительно незавершённые jobs как interrupted/error; не маркирует все completed. Trace-coverage тест сверяет actual mock requests с recorded attempts.

### N06 — scoring regression, не scoring redesign

Использовать существующие coverage helpers. Доразобрать model-local `Requested`, empty selection, mixed known/unknown и explicit preflight. Добавить tests для compat-only, sdk-only, model-param-only и смешанного run. Не вводить новый общий numeric score.

### N07–N09 — экран строится по данным, не наоборот

Reducer и estimator тестируются без terminal и без logging. Макеты раздела 10 — golden contract. Интерактивный просмотр статических макетов `cli-preview.html` служит только design review; production вывод остаётся терминальным. Reviewer может менять layout/колонки до implementation, но не семантику global limits/ETA lower bounds.

### N10–N12 — законченная эксплуатационная цепочка

`aiqa log` читает реальный sidecar, stdout работает с pipe, progress не ломает grep/CI, finalization не объявляет завершение раньше записи. Документация не обещает heartbeat/ETA/filter, если нет executable test. Один migration PR удаляет backlog и переводит согласованные требования из `next.md` в docs по мере завершения.

## 20. Карта изменений по файлам

| Существующая область | Изменение |
|---|---|
| `internal/app/app.go` | Параллельная orchestration, run runtime, общий lifecycle, финальный artifact commit |
| `internal/protocol/registry.go` | RunContext/admission handles, typed plan, scope без mutable singleton |
| `internal/protocol/{openai,anthropic,googlegenai}/runner.go` | Resolve once, Prepare traced, Execute plan, subprogress composite |
| `internal/api/client.go`, `pacing.go` | Admission per attempt/body lifecycle, wait IDs, quota groups, точные telemetry |
| `internal/probe/executor.go` | Убрать частичное DoN; context-aware workflows; не считать waiters HTTP active |
| `internal/extsuite/runner.go`, `collector.go` | Frozen SDK invocations, immediate completion, shared external reservations |
| `internal/assessment/assessment.go`, capability routing | Selected-scope unresolved coverage и no-match guard |
| `internal/progress/*` | Reducer/snapshot integration, собственный ETA interface, отсутствие truncation IDs |
| Новый `internal/scheduler/*` | Broker, queues, group constraints, budgets, fake-clock tests |
| Новый `internal/eta/*` | Remaining-work estimator и deterministic replay |
| Новый `internal/terminal/*` | Pure frames, width/Unicode, single writer, TTY/CI lifecycle |
| Новый `internal/logview/*` | Reader, filters, follow, rendering, malformed-input policy |
| `internal/config`, `cmd/aiqa/main.go` | Новые флаги, log dispatch, cancellation exit conventions |
| `internal/logging`, `telemetry`, `traceverify` | Safe event fields, prepare coverage, scheduler correlation, version gates |
| `internal/report`, `internal/result`, schemas | Optional execution summary, stdout, partial/final states |
| `docs/monitoring.md`, `docs/trace-contract.md`, README | Точный shipped contract, отказ от устаревших обещаний |
| `tasks/backlog.md` | Удалить/однорелизная ссылка; задачи теперь в `tasks/next.md` |

Точные имена новых пакетов допустимо изменить в design review. Обязательны boundaries и observability semantics, не количество Go-файлов.

## 21. Acceptance: обязательные детерминированные проверки

### 21.1. Параллельность и ресурсы

**A01.** Три независимых loopback origins, C=16: barriers подтверждают, что первая генерация каждого target начинается до полного окончания любого другого. Не заменять этот тест измерением «примерно быстрее».

**A02.** При C=1 общая фактическая in-flight HTTP cardinality никогда не превышает 1; при C=16 — 16, включая discovery, negative-auth clones, retries, built-in SDK, downloads и streams.

**A03.** Один origin с тремя paths → общая learned capacity/cooldown. Три origins с одним explicit group → то же. Один origin с отдельными aliases → независимость по явному override.

**A04.** HTTP429/61s Retry-After в OpenAI не задерживает ready Anthropic/Google requests другой группы. При отсутствии ready work программа ожидает без busy spin.

**A05.** Waiting group не занимает global slots. C=2, один group throttled и два ready requests другого — допускается полезная работа.

**A06.** Weighted reservation 2+2 при capacity2 не deadlock; получение all-or-none, cancellation немедленно освобождает очередь. Резерв >C не clamp молча.

**A07.** Stream slot освобождается на close/EOF, не на headers; cancellation освобождает его exactly once.

**A08.** При конкуренции всех ready targets нет starvation; default scheduling deterministic в fake-clock model, runtime completion ordering свободен.

**A09.** Native и SDK совместно не создают больше двух image artifacts при budget2; timeout after possible create не инициирует бесплатный «повтор» без reservation.

**A10.** High-memory context workload не превышает request byte budget и не блокирует весь unrelated small-request throughput.

### 21.2. План и evidence

**A11.** Один discovery per target, кроме явного pagination; Prepare requests имеют полноценные trace owners.

**A12.** CLI overrides и file overrides дают одинаковый frozen/actual SDK plan. Изменившийся preflight route не создаёт незарегистрированный job.

**A13.** Long IDs >96 bytes завершаются; разные IDs с одинаковым prefix не объединяются. Display truncation не меняет state.

**A14.** Duplicate completion того же outcome идемпотентен; неизвестный ID и конфликт terminal outcomes обнаруживаются и не меняют count молча.

**A15.** SDK fast suite completion видно до окончания slow suite того же invocation. Финальная сериализация остаётся стабильной по порядку.

**A16.** Отсутствующий SDK result становится runner_error, а не completed. Неисполнявшийся image job имеет policy skip с причиной, не phantom PASS.

**A17.** Unknown selected scope, mixed PASS+unknown, FAIL+unknown, no-match, explicit preflight, route rejection и transient errors имеют согласованные coverage/verdict/rankable по разделу 17.

**A18.** `test=model-param` не изображает поддержку Anthropic параметров, если runner её не реализует; no capability inference from failed tooling.

### 21.3. ETA и ожидания

**A19.** Сценарии 9.6 A–D дают расчётные значения; E даёт lower_bound/unknown.

**A20.** Known cooldown 61s не сопровождается whole-run ETA14s, если весь remaining work за ним. Сменившийся deadline сразу отражается на UI.

**A21.** `Waited` растёт, `Resumes in` убывает при стабильном расписании. Source initial_pacing и retry_after различаются. На wait end label исчезает.

**A22.** Один общий cooldown не прибавляется по числу waiters. Межцелевой ETA не суммируется при полностью независимой работе и не берётся max при глобальном contention.

**A23.** Skip/blocked/local assertions не повышают throughput HTTP. Active request старше duration samples не получает нулевой остаток.

**A24.** Fake wall clock jump не влияет на elapsed/countdown. Во время planning/unknown numerator нет процентов или фиктивной ETA.

**A25.** Heartbeat non-TTY приходит каждые30s даже без event completions. Эту проверку делать fake clock, не 30-секундным sleep в unit test.

### 21.4. Терминал

**A26.** Golden frames: 120,110,80,60,48 columns; 12 и 24 lines height; ASCII, UTF-8, NO_COLOR, redirected stderr, TERM=dumb, CI.

**A27.** Никаких строк шире width−1; combining marks/CJK/long model names не ломают alignment. Escape/control injection из IDs не попадает в terminal.

**A28.** Renderer — единственный stderr owner. Concurrent complete/wait/error/resize не смешивают lines и не повреждают terminal state.

**A29.** TTY detector отличает PTY от pipe и `/dev/null`. stdout redirect сам по себе не отключает TTY stderr.

**A30.** Freeze останавливает planning ticker; Close идемпотентен; после финальной newline нет redraw.

**A31.** FINISHED provider с conformance FAIL отображается `DONE / FAIL`; runtime FAILED и пользовательский INTERRUPTED отличны. `completed — completed` отсутствует.

**A32.** MAIN COMPLETED появляется только после последнего проверенного artifact close/commit. Trace verification с искусственной задержкой сохраняет FINALIZING.

### 21.5. Logs, IO, приватность

**A33.** `aiqa log --level WARN` исключает INFO/DEBUG/TRACE. --only-level точен; ошибочные комбинации flags отклоняются до чтения.

**A34.** `--failed` и --level ERROR дают разные корректные выборки; expected401/PASS не попадает в failed по одному HTTP code. Target/model filters не обходятся FATAL.

**A35.** Follow дочитывает разрезанную JSON line, переживает rotation/truncation явно, ограничивает record/memory, не требует внешних tools.

**A36.** Input schema mismatch, malformed record, malicious ANSI и файл без final newline обрабатываются детерминированно; reader не обращается к сети.

**A37.** Default verbosity0 не создаёт forensic log; verbosity4 создаёт обе private sidecars с0600 и collision error. Не ослабляются redaction/allowlist.

**A38.** `--stdout=json` возвращает один parseable JSON; Markdown/logs/progress в нём нет. Public stdout/file совпадают по final forensic attachment.

**A39.** Secrets/URLs/bodies отсутствуют в progress, execution events и trace при retry/error/cancel. Viewer не выводит terminal control из входных строк.

**A40.** First interrupt отменяет queue/inflight и пишет partial; second interrupt завершает bounded shutdown. Queue wait не расходует --timeout, но respects queue/run timeout.

### 21.6. Регрессии

**A41.** Сохраняются selectors compat/model-param/sdk/ext-suite, current model intersection и existing profile revisions.

**A42.** При одинаковом evidence UI mode, verbosity и concurrency не меняют canonical assessment. Разные настоящие provider outcomes из-за load остаются реальными наблюдениями, а не нормализуются до желаемого verdict.

**A43.** Traces разных targets не смешивают parent scope; prepared requests сопоставляются с mock request ledger. Offline trace PASS не скрывает missing producer coverage в integration test.

**A44.** `make qa` baseline toolchain, плюс новые replay/PTY tests проходят. Обычный CI не вызывает настоящие billable providers.

## 22. Проверка готовности и поставка

Существующий `make qa` включает deterministic checks, race, build/version и security gates [R19]. Его необходимо расширить новыми scheduler/ETA/PTY/log fixtures, а не заменить визуальным ручным просмотром.

```bash
make qa
# Новые отдельные target names ниже — часть реализации N12:
make scheduler-check
make monitoring-check
```

Live validation — только после deterministic gates, с явно заданными QA keys, budgets и hosts. Она подтверждает наблюдаемое поведение конкретной конфигурации, не является обязательной для unit PRs и не доказывает универсальную независимость квот.

Definition of Done:

- Пользовательская команда запускает три независимых provider targets параллельно при C16, не превышая общий cap.
- Pacing одного target не останавливает другие; same-origin/explicit shared quotas учитываются.
- В терминале понятны phase, target, active HTTP, done/total jobs, ожидание и ETA/unknown.
- Native/SDK/parameter progress основан на реальных lifecycle boundaries.
- План, счётчики и report evidence согласованы; long IDs и early failures не теряются.
- `aiqa log`, CI heartbeat, stdout opt-in и cancellation работают по тестам.
- `tasks/next.md` единственный активный следующий план, старые требования имеют явную disposition.
- UI, private forensic verdict и provider conformance остаются разными независимыми результатами.

## 23. Что сознательно не делается в этом этапе

Не добавлять новые model benchmarks, новый numeric compatibility score, SDK languages, hosted dashboards, remote telemetry по умолчанию, background monitoring daemon, live provider autoconfiguration, автоматическое исправление gateway, массовые Files/Batches тесты и новые multimodal scopes.

Отдельный design-preview HTML не превращается в `--format=html`; это только согласуемый макет терминального интерфейса. Реализация стадий N01–N12 не требует потратить токены реального Dahl/Google/OpenAI.

## 24. Источники и прослеживаемость

Ниже permalink-и к прочитанному baseline. Номера строк в linked code используются для навигации, а символы/functions в тексте — как устойчивый ориентир. Исторические PRD из предыдущих обсуждений не считаются описанием текущей реализации.

**[R1]** [Orchestration, Prepare/Execute, sidecars и finalization — `internal/app/app.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/app/app.go).

**[R2]** [Reporter, Freeze, Complete, Pacing, UI labels — `internal/progress/progress.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/progress/progress.go).

**[R3]** [Текущий monitoring contract — `docs/monitoring.md`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/docs/monitoring.md).

**[R4]** [Adaptive pacer, wait lifecycle, rate calculations — `internal/api/pacing.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/api/pacing.go).

**[R5]** [OpenAI Prepare/Execute и composite jobs — `internal/protocol/openai/runner.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/protocol/openai/runner.go).

**[R6]** [Anthropic Prepare и target-owned runtime — `internal/protocol/anthropic/runner.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/protocol/anthropic/runner.go).

**[R7]** [DoN, Map и cancellation boundary — `internal/probe/executor.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/probe/executor.go).

**[R8]** [PlannedJobs, SDK dispatch/collection, frozen job completions — `internal/extsuite/runner.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/extsuite/runner.go).

**[R9]** [Текущий CLI и signal handling — `cmd/aiqa/main.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/cmd/aiqa/main.go).

**[R10]** [Заменяемый backlog — `tasks/backlog.md`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/tasks/backlog.md).

**[R11]** [Global model intersection — `internal/protocol/selection.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/protocol/selection.go).

**[R12]** [Privacy и forensic schema policy — `docs/trace-contract.md`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/docs/trace-contract.md).

**[R13]** [Coverage materialization и verdict — `internal/assessment/assessment.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/assessment/assessment.go).

**[R14]** [Toolchain и зависимости — `go.mod`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/go.mod).

**[R15]** [Существующие progress regression tests — `internal/progress/progress_test.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/progress/progress_test.go).

**[R16]** [PreparedTarget и logical job contract — `internal/protocol/registry.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/protocol/registry.go).

**[R17]** [Requested/applicability/module gating — `internal/capability/router.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/capability/router.go).

**[R18]** [Текущие defaults и Google profile selection — `internal/config/config.go`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/internal/config/config.go).

**[R19]** [make qa и quality gates — `Makefile`](https://github.com/ilyar/aiqa/blob/e1acadbbcbbcc11b64bc3929343b32ea896e8e2e/Makefile).

**[U1]** Приложенный original `monitoring.md`, PRD «CLI Stream Monitoring, Structured Logging & Progress Feedback», разделы 1–6; hash указан в §0. Это отдельный источник, не текущий `docs/monitoring.md`.

**[U2]** Терминальный фрагмент и команда из запроса пользователя. Источник наблюдаемого симптома; raw trace/точные hosts не предоставлены.

**[W1]** [Go `semaphore.Weighted`](https://pkg.go.dev/golang.org/x/sync/semaphore): cancelable Acquire всего веса. Не является готовым multi-resource scheduler или стандартом ETA.

**[W2]** [RFC 9110 §10.2.3 Retry-After](https://www.rfc-editor.org/rfc/rfc9110.html#name-retry-after): delay-seconds/HTTP-date. Не определяет provider group и не гарантирует успешный следующий запрос.

**[W3]** [Go `x/term`](https://pkg.go.dev/golang.org/x/term): IsTerminal, GetSize и terminal state. Не предписывает UI layout; layout этой спецификации — продуктовый выбор.

**[W4]** [OpenTelemetry Logs Data Model](https://opentelemetry.io/docs/specs/otel/logs/data-model/): время, severity, trace correlation. Не является стандартом API conformance или готовой системой расчёта ETA.
