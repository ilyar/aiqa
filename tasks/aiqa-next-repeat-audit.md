# AIQA: повторный аудит monitoring / progress UI и обязательные исправления

**Вердикт: REQUEST CHANGES. Визуальный и сквозной runtime-контракт `tasks/next.md` не выполнен.**

Дата: 2026-09-09.  
Проверенный HEAD: `c66dbd8d51f6f828173bbc71e78801c47f7bf56f`, `feat: reconcile terminal outcomes with frozen plan`.  
Первый аудит: `6b92b97cb09f7840a41d0e8aa66922f8787ca2ba`. Между ними 44 commits.  
Повторная проверка main в конце аудита: тот же `c66dbd8d51f6f828173bbc71e78801c47f7bf56f`.  
Область: повторная проверка исправлений первого аудита, с приоритетом реального CLI, соответствия согласованным макетам, progress lifecycle, ETA, pacing и критериев приёмки. Это не повторная сертификация API-профилей моделей.

## 1. Решение

Не принимать работу по наличию `internal/terminal`, `internal/eta`, новых тестов отдельных packages или успешного общего CI. Приёмка должна показывать **собранный `./bin/aiqa`, выполняющий native + SDK проверки трёх targets под настоящим PTY**. Этот процесс обязан перерисовывать одну компактную inline-панель, а не добавлять progress snapshots в scrollback.

Обязательные исправления:

1. Разделить `operation_id` трассировки и `job_id` неизменяемого плана. Не передавать произвольные OTel probe spans в счётчик jobs.
2. Удалить старый hand-written TTY writer из production CLI-path.
3. Использовать **`charm.land/bubbletea/v2 v2.0.9` как единственного владельца интерактивной перерисовки**, в inline-режиме, без alternate screen, чтения stdin, mouse tracking и terminal title/progress extensions.
4. Подключить transport/scheduler → execution reducer → ETA → view model → renderer; не рисовать отсутствующие наблюдения как нули.
5. Довести pure layout до макетов §10 `next.md`: отдельные строки targets, HTTP in-flight, причины ожиданий, countdown, ETA и финализация.
6. Добавить обязательную PTY-приёмку с VT-состоянием экрана и scrollback, а не только сравнение строк из `Render`.

Пункты 1–6 составляют одну вертикально завершённую поставку. Один только шумоподавляющий фильтр — hotfix, **не завершение monitoring UI**.

## 2. Основания и ограничения доказательств

### 2.1. Пользовательский запуск

Приложение `Pasted text(7).txt` содержит:

```bash
aiqa --verbose 4 --concurrency 16 --timeout=60 test=compat,sdk \
  --format=markdown,json --name=compat-report
```

В 259 текстовых строках обнаружено:

| Наблюдение | Количество / пример |
|---|---|
| `unknown job start` | 63 |
| `unknown job completion` | 63 |
| Все такие diagnostics | 126 |
| Повреждённый label | `OpenAI compatibilitygenai.models.list`, строка 72 |
| Другой хвост | `official-sdk.anthropic.structured-outputrn`, строка 180 |
| Другой хвост | `official-sdk.openai.chat.jsonletionstput`, строка 200 |
| Финальный progress | `27/27 100%`, строка 256 |
| Diagnostic после 100% | строка 257 |
| Старое completion-сообщение | `completed — completed`, строки 59/251/258 |

SHA-256 приложения:

```text
914cdd3803ae69e9c53640d95fa6af6558def04577d51c6578e509b2a91790c6
```

Это **clipboard text**, а не raw PTY capture: в файле нет исходных ESC/CR bytes и нет `aiqa --version`/build SHA. По нему нельзя восстановить все cursor operations, определить точную сборку, оценить concurrency/длительности или заключить, что 126 проверок потеряны. В частности, показанный счётчик достигает 27/27 — утверждение о зависшем на 22% целом запуске было бы неверным.

Тем не менее наблюдаемые noise и stale tails имеют конкретный действующий источник в проверенном HEAD. Объяснять всё исключительно старым локальным бинарником или особенностями copy/paste оснований нет.

### 2.2. Репозиторий

Через GitHub прочитаны актуальные файлы, перечисленные в Sources. Blob-сопоставление особенно показательно:

| Файл | Git blob в первом аудите и текущем HEAD | Вывод |
|---|---|---|
| `internal/progress/progress.go` | `0e72f190341623f9f54b876afe712b0b0575cc8c` | Файл отрисовки не изменён |
| `internal/terminal/terminal.go` | `42ec4de9366f65d56cf3f4cc6afb2bc83c837340` | Pure renderer не изменён |
| `go.mod` | `e985ddad6c9d4cb1acf1ffbc23f6ee1eb6e83123` | Renderer framework не добавлен |
| `tasks/next.md` | `2823bae45d7387e641055c609e85d8358cae63f5` | Исходный согласованный контракт тот же |

Источники: [S1]–[S9].

### 2.3. Локальное воспроизведение

Запущен отдельный Go-модуль с **извлечённой логикой проверенных методов** Reporter и минимальными audit-only типами. Это не полный исходный файл, не настоящий AIQA binary и не end-to-end тест.

Результат:

```text
PASS  GenuineUnknownJobStillDiagnosed
PASS  DuplicateTerminalIsIdempotent
PASS  EraseLineClearsSuffix
FAIL  TraceSpansMustNotCreateJobDiagnostics
      63 пары trace events → 126 false diagnostics, 252 LF bytes
FAIL  ShorterFrameMustEraseSuffix
      got: OpenAI compatibilitygenai.models.list
      want: OpenAI compatibility
```

Оба regression expectations падают в 10/10 повторах и с `-race`; три positive controls проходят. Race detector в этом изолированном сценарии не выявил data race. Это проверка логики маршрутизации и вывода, а не доказательство отсутствия всех гонок проекта.

В приложенном наборе есть AST-extractor: он позволяет повторить тесты на **методах собственного checkout**, без правок рабочего дерева и без зависимости от скачивания SDK. При последующем удалении legacy API эти тесты должны быть перенесены на новый adapter, а не использоваться как требование сохранять плохую архитектуру.

### 2.4. Что не запускалось

Полный `make qa`, новый AIQA binary и live-provider сценарий локально не запускались. Здесь Go 1.23.2; `go.mod` проекта требует Go 1.27. Наличие Go 1.27 в CI не означает, что он доступен в runtime данного аудита.

Проверен CI именно текущего SHA: run `34377516746`, success; шаги check/security/build/race успешны, live-provider jobs skipped [S16]. Это подтверждает существующие CI-проверки, но не исполнение отсутствующей визуальной приёмки.

## 3. Что изменилось после первого аудита

Ниже «изменение подтверждено» означает **чтение нового production-path**, а не свежий полный acceptance PASS. Старые дефекты не следует механически копировать в открытый список без проверки.

| Первый finding | Новый результат | Остаток / приёмка |
|---|---|---|
| F01: lost wakeup capacity → cooldown | Добавлены `changed` channel, notification/re-read и schedule revision [S10] | Сам исходный fake-clock reproducer на полном свежем scheduler здесь не перезапускался; требовать regression gate |
| F02: artifact retry / budget | Для `images/generations` native retry-loop ограничен одним attempt; committed budget отклоняется через `ErrArtifactBudgetExhausted` [S11,S12] | Проверять обе native/SDK границы и ambiguous creation; визуальный fix не должен откатить policy |
| F03: opaque extensions вне общего budget | Invocation получает `AcquireOpaqueLease` через общий broker [S13] | Сохранить combined native/SDK/opaque cap в integration test |
| F04: раздельные adaptive pacers | Клиенты регистрируют `EnsureAdaptiveGroup`, при bound broker локальный pacer больше не допускает запросы [S11] | Проверять actual shared quota; UI group facts всё ещё не подключены полностью |
| F05: plan не участвует в execution | Добавлены frozen SDK invocations, `ExecutePlan`, terminal reconciliation [S3,S14] | Не равно полному workflow DAG: runtime owner mapping и estimate bindings ещё требуют работы |
| F06: ETA/dashboard не в CLI | **Открыт**: CLI продолжает создавать старый Reporter [S3,S4] | Главный blocker данного аудита |
| F07: неполный execution aggregate | Добавлены preparation/API bridges и `builder.Observe` после reducer updates [S5] | **Частично**: runtime events без owner игнорируются; scheduler hook не передан из app; group bridge отфильтровывает часть facts |
| F08: log follow signals | `runCLI` использует `runLogApplication` и cancellable `runLogContext` [S15] | Исправление маршрута подтверждено; реальный subprocess SIGINT/SIGTERM gate обязателен |
| F09: completed до commit | Forensics/tracing завершаются явно перед единственным report commit; completed после error boundary [S3] | Порядок исправлен; экран FINALIZING ещё не подключён |
| F10: discovery без trace owner | Target spans созданы до parallel Prepare, preparation owner/hooks регистрируются заранее [S3,S14] | Проверить mock request ledger и runtime ownership после Prepare |
| F11: память после serialization | Добавлены sizing/reservation/buffer-lifetime механизмы [S11] | Полную allocation-bound гарантию не считать закрытой только по имени `marshalJSONReserved`; нужен allocation/budget test |

**Итог delta:** внутренние исправления действительно есть. Но два файла, определяющие вид progress UI, остались прежними. Нельзя объявлять N07–N09 готовыми по тому, что исправлены scheduler и final report commit.

## 4. Findings повторного аудита

### U01 — P1 — OTel operation IDs попадают в namespace frozen jobs

**Места:** `internal/app/execution_progress.go: ObserveLifecycle`, `internal/progress/progress.go: ObserveLifecycle/Start/Complete` [S4,S5].  
**Связь с next:** §6, §12, N01/N05/N07; A13–A16/A28/A42.

В коде:

```go
func (p *executionProgress) ObserveLifecycle(event telemetry.LifecycleEvent) {
    p.sink.ObserveLifecycle(event)
}
```

Затем Reporter:

```go
if event.Name == "probe" && event.Phase == "start" {
    r.Start(event.OperationID)
}
```

Здесь `OperationID` — идентификатор span/операции, а план содержит host-owned IDs вроде `native:openai:...:compat` и `extension:...`. Новая защита `isKnown` в `executionProgress.Start/Complete` не устраняет проблему: forwarding через `p.sink.ObserveLifecycle` обходит её.

**Следствие:** валидные внутренние lifecycle events ошибочно объявляются неизвестными jobs. Это не доказательство, что все соответствующие probes отсутствовали в плане.

**Обязательное исправление:**

- Убрать преобразование произвольного `LifecycleEvent.OperationID` в JobID.
- Убрать `telemetry.Observer` из ответственности display renderer. Допустим временный adapter, но он не имеет права вызывать JobStarted/Finished для span IDs.
- Планировщик/native workflow/SDK suite публикуют типизированные job events с ID из frozen plan. Trace correlation остаётся отдельными `operation_id`/`span_id`.
- Ввести различные Go-типы `JobID` и `OperationID`; не делать fallback `jobID = operationID` при пустом owner.
- Сохранить обнаружение **настоящих** неизвестных плановых IDs: один structured runner finding, без изменения denominator, без сотен сообщений в терминале. Конфликт terminal outcomes — execution-integrity error, а не provider incompatibility.
- Идемпотентный повтор одного terminal outcome не увеличивает count и не создаёт error.

**Не принимать:** удаление всех unknown diagnostics без устранения namespace mixing; добавление каждой OTel операции в denominator; отключение telemetry при `--verbose 4`.

### U02 — P1 — Diagnostics намеренно превращают repaint в новые строки

**Место:** `progress.go: diagnosticLocked → clearBarLocked → durableLocked → renderLocked` [S4].  
**Связь:** §10–11; A28/A30.

`clearBarLocked` ничего не стирает:

```go
if r.tty && r.ttyRendered && !r.barTerminated {
    fmt.Fprintln(r.writer)
    r.ttyRendered = false
}
```

При каждом U01 diagnostic текущая строка закрепляется переводом строки, затем печатается diagnostic, затем снова progress. Поэтому даже TTY превращается в журнал. Это не объясняется тем, что clipboard превратил все `\r` в `\n`: код действительно выполняет `Fprintln` на данном пути.

**Обязательное исправление:** после внедрения renderer owner удалить старые `clearBarLocked`, `renderLineLocked`, `finishBarLocked` как production-реализацию terminal lifecycle. Routine probe events не должны создавать durable lines. Редкое настоящее сообщение можно провести через единственного владельца UI с однократной записью над live region; допустимая частота не зависит от числа probes.

**Приёмка:** 500 routine lifecycle events в настоящем TTY не создают 500 строк history. Проверять терминальный scrollback, не только bytes buffer.

### U03 — P1 — Короткий frame не очищает хвост длинного

**Место:** `progress.go: renderLineLocked` [S4].  
**Связь:** A26/A27/A28.

```go
fmt.Fprintf(r.writer, "\r%s", text)
```

CR возвращает позицию в начало строки, но не удаляет уже нарисованные символы правее нового текста. В изолированном тесте пара

```text
official-sdk.google-genai.models.list
OpenAI compatibility
```

создаёт ровно строку из пользовательского приложения:

```text
OpenAI compatibilitygenai.models.list
```

**Обязательное исправление:** очистку остатков строк и сокращённого многострочного блока делегировать одному renderer. Не делать локальный hotfix `\r\x1b[K` конечным решением: он не подключит dashboard, resize, ETA и target rows.

**Приёмка:** long→short label, исчезновение двух detail rows, смена 120→80→48→120 columns не оставляют ни старых суффиксов, ни старых строк.

### U04 — P1 — Новый layout и ETA не являются частью actual CLI

**Места:** `app.go: newOperatorReporter`, `internal/terminal/terminal.go`, `internal/progress/progress.go` [S3,S4,S6].  
**Связь:** N08/N09; A20/A21/A26–A32.

Фабрика по-прежнему возвращает `*progress.Reporter` и передаёт только Writer/TTY. Она не подключает новый terminal view и ETA. Active path не знает постоянных target rows, Ready/Deferred, per-target ETA, next eligible и результатов assessment.

Дополнительно нынешний pure `terminal.Render` **сам ещё не эквивалентен макету**: в `Target` нет типизированных HTTP/ETA/NextEligible, общий `HTTPDone/HTTPTotal` смешан с in-flight-показателем, широкая строка содержит лишь TARGET/STATE/JOBS/VERDICT. Его простое включение исправит не весь дизайн.

**Обязательное исправление:** реализовать точный view model и сцены из §7 этого документа; заменить фабрику на единый controller/sink interface. Pure renderer доработать, а не считать его нынешнюю строковую раскладку согласованным дизайном.

### U05 — P1 — Live request/group/wait facts ещё не доходят до полного состояния панели

**Места:** `app.go` при создании `ExecutionHooks`; `execution_progress.go: ObserveAPIExecution/ObserveSchedulerFact`; `api/client.go: SchedulerFactObserver`; `protocol/registry.go: StartJob/ExecutionHooks` [S3,S5,S11,S14].

Подтверждены конкретные разрывы:

- App заполняет `Observe` и `PreparationOwner`, но не `ObserveScheduler`.
- `ObserveAPIExecution` явно отбрасывает события, в которых нет ни JobID, ни PreparationID.
- `StartJob` передаёт ID счётчику, но не возвращает context с execution owner.
- Client `SchedulerFactObserver` пропускает только wait facts. Group-limit/cooldown facts должны иметь отдельного подключённого producer.
- `builder.Observe` теперь существует, но он не может восстановить события, которые не поступили.

**Обязательное исправление:**

1. Каждый native workflow и каждый SDK suite получает context с `api.WithExecutionOwner(...JobID...)` из frozen binding, до своего первого HTTP call. Дочерние retries сохраняют JobID, но получают уникальный AttemptID.
2. PreparationOwner применяется только к pre-freeze discovery/preflight, не используется как универсальный owner дальнейших запросов.
3. Подключить `ExecutionHooks.ObserveScheduler`; один run-owned subscription отдельно передаёт group changes, а waits разрешаются через явный owner mapping. Не создавать второго счётчика HTTP на основе broker events, если transport уже авторитетный producer.
4. Snapshot содержит реальную кардинальность in-flight requests до body/stream terminal, а не число jobs/goroutines.
5. Неизмеренные runtime metrics — unavailable/partial, не фиктивные нули. До полной интеграции блок `execution` можно честно не публиковать, но это **не** закрывает приёмку.
6. Не скрывать actual ownership gaps: emit instrumentation finding и не выдавать incompletely observed run за полностью измеренный.

**Приёмка:** независимый mock ledger физически активных запросов совпадает с UI/state; у 429 wait есть причина и deadline; обычный local pacing не назван HTTP429.

### U06 — P1 — Current CLI не соответствует TTY/resize/flags contract

**Места:** `app.go: progressTTY/newOperatorReporter`, `terminal.go: StderrMode/SelectMode` [S3,S6].

Production detection всё ещё использует `ModeCharDevice`; корректный `term.IsTerminal` находится в другом helper. Factory не передаёт color/unicode/width policy. Reporter пишет под собственным mutex; медленный stderr может задержать producer, который синхронно вызывает progress API.

**Обязательное исправление:** единый policy resolver, `IsTerminal(stderr.Fd())`, `GetSize`, правила mode/color/unicode из `next.md`; isolated render IO, coalesced view snapshots; resize actual output FD. Не исправлять это отключением UI по умолчанию.

### U07 — P1 — Existing test success не проверяет пользовательский результат

**Места:** `progress_test.go`, pure terminal fixtures, текущая CI pipeline [S7,S16].

Изученные Reporter tests проверяют buffers, counters, heartbeat и long IDs. В частности, отсутствие ANSI в plain buffer или наличие `2/2` не проверяет содержимое настоящего терминального экрана после перезаписи. Зелёный CI не опровергает U01–U06.

**Обязательное исправление:** новый обязательный `make ui-acceptance`, который строит настоящий CLI и проверяет его через PTY + полноценный VT emulator, независимые mock endpoints и контрольные fixtures. Pure Render golden tests остаются первым уровнем, но не заменяют integration gate.

### U08 — P1 — Canonical transport success превращается в failed execution outcome

**Места:** `api/client.go: executionOutcome/executionHTTPOutcome` и `app/execution_progress.go: executionOutcome` [S5,S11].

Transport публикует `Outcome: "succeeded"` для успешного 2xx attempt. App normalizer принимает только `completed`, `success`, `pass`, `passed`; `succeeded` попадает в `default → execution.OutcomeFailed`.

Это отдельная ошибка смыслового преобразования. Подключение нового renderer без исправления может показать ошибочные failed request states. Это не утверждение, что canonical conformance verdict в пользовательском отчёте уже изменился: execution state и профильный verdict здесь разные слои.

**Обязательное исправление:** использовать один типизированный enum на transport/reducer boundary или полный явно проверенный adapter. Canonical `succeeded` обязан давать `OutcomeSucceeded`; неизвестное значение — instrumentation error, не автоматический provider/request FAIL. Дополнить contract tests для всех canonical outcomes и legacy aliases. Тест `executionOutcome("succeeded") == execution.OutcomeSucceeded` должен проходить в настоящем package app.

## 5. ADR: конкретная библиотека и границы её применения

### 5.1. Факт текущей реализации

В текущем `go.mod` нет ни `schollz/progressbar`, ни `mpb`, ни Bubble Tea [S8]. `golang.org/x/term` и `github.com/rivo/uniseg` — полезные зависимости для terminal detection/size и ширины grapheme clusters, но они не являются движком многострочной перерисовки.

**Поправка к прежней спецификации:** последняя рекомендация §11 `next.md` сама разрешала собственный pure renderer + terminal writer. Поэтому неверно утверждать, что исполнитель нарушил уже существовавшее обязательство использовать Bubble Tea. Нарушен **визуальный/поведенческий контракт**, а ниже фиксируется новое однозначное implementation decision вместо прежней необязательной рекомендации.

### 5.2. Принятое решение

Добавить проверенный pin:

```go
require charm.land/bubbletea/v2 v2.0.9
```

Не использовать `@latest` в воспроизводимой сборке. Это выбранная проверенная версия, а не обещание, что она навсегда последняя или не содержит дефектов. Обновить `go.sum`, SBOM/security gates. Дополнительная UI dependency не освобождает от `govulncheck` и PTY regression tests.

Bubble Tea поддерживает inline-представление и берёт на себя terminal redraw; официальный API предоставляет `WithOutput`, `WithInput(nil)`, `WithoutSignalHandler`, `WithFPS`, `WithWindowSize`, а v2 использует `View() tea.View` [L1,L2,L3]. Использование библиотеки **не означает fullscreen**.

Выбор:

| Вариант | Решение для этой работы |
|---|---|
| Bubble Tea v2 | Использовать для единственного live renderer и lifecycle |
| Вернуть single-line progressbar | Не использовать: не воспроизводит согласованную панель |
| Писать новый cursor writer вручную | Не использовать: уже дважды не получен требуемый terminal result |
| Добавить второй независимый bar writer поверх framework | Запрещено: возникает несколько владельцев stderr |

### 5.3. Конфигурация адаптера

Скелет использует реальные API выбранной версии. Это граница адаптера, **не готовый патч проекта**:

```go
import tea "charm.land/bubbletea/v2"

program := tea.NewProgram(
    dashboardModel, // Init, Update, View() tea.View
    tea.WithOutput(stderr),
    tea.WithInput(nil),
    tea.WithoutSignalHandler(),
    tea.WithFPS(4),
    tea.WithWindowSize(width, height),
)
```

`View()`:

```go
func (m dashboardModel) View() tea.View {
    frame := terminal.Render(m.view, m.width, m.height, m.options)
    view := tea.NewView(frame.String())
    view.AltScreen = false
    view.DisableBracketedPasteMode = true
    return view
}
```

Не задавать mouse tracking, clipboard, window title, terminal badge `View.ProgressBar` или keyboard enhancement: это read-only панель, а не terminal IDE. Текстовый progress bar в View — часть согласованного layout, не OS-specific progress indicator.

Ключевые ограничения:

- `WithoutSignalHandler` означает, что существующий CLI signal owner продолжает управлять отменой run; нельзя убрать handler вообще.
- `WithInput(nil)` исключает чтение stdin; проверить PTY termios: raw mode не включается. Отображение не должно перехватывать данные pipeline.
- UI context не отменять первым SIGINT вместе с network context: панель должна отобразить CANCELLING/FINALIZING. `Quit` отправляется после terminal result/final commit либо forced shutdown.
- Bubble Tea `Program.Send` может блокировать; не вызывать его под broker/reducer locks или непосредственно из сетевого producer. Нужен один bridge с latest-snapshot mailbox.
- `WithoutRenderer` не использовать для TTY-path; не выбирать другой старый writer скрытым fallback.
- Failure создать UI в реально интерактивном mode: один понятный warning и plain fallback, с recorded effective mode. Failure нельзя превращать в silent «успешный dashboard».

Записать это решение в `docs/adr/cli-renderer.md`; заменить только реализационную рекомендацию последнего абзаца §11 `next.md`, сохранив остальные требования и §10 макеты.

## 6. Точные изменения исполнения и данных

### 6.1. Один state owner, один presentation owner

```text
Frozen RunPlan ─── canonical job lifecycle ──────┐
Native + official SDK transport facts ──────────┤
Broker waits / group limits / cooldown ─────────┤
Target assessment + finalization events ────────┘
                       │
                execution.Reducer
                       │
          immutable measured snapshot
                       │
       remaining-work model + ETA estimator
                       │
               terminal view mapper
                       │
         latest-snapshot mailbox (capacity 1)
                       │
          Bubble Tea inline renderer → stderr
```

Domain events не теряются и применяются exactly once. Coalesce разрешён **только уже рассчитанным display snapshots**, не request/job terminal events. Один и тот же snapshot sequence кормит UI и safe execution summary.

Plain renderer использует тот же state, но печатает milestones/heartbeat вместо перерисовок. Forensic logger независимо получает allowlisted facts по выбранной verbosity.

### 6.2. View model должен содержать поля, а не распарсенный text

Минимальные обязательные поля (эквивалентные Go structs допустимы):

```text
RunView:
  name, build_ref, phase
  planned_jobs, finished_jobs, cancelled_jobs, running_jobs, queued_jobs
  http_inflight, global_http_capacity, opaque_reserved
  ready_requests, deferred_requests
  elapsed, eta, target_rows, notices
  report_state, final_verdict, exit_code, trace_integrity

TargetView:
  id, execution_state, conformance_verdict
  planned_jobs, finished_jobs, cancelled_jobs
  http_inflight, ready_requests, deferred_requests
  eta, next_eligible, group_alias, wait_reason, wait_source, wait_started
  current_activity, adaptive_progress

ETA:
  scope, state, estimate, range_low, range_high, reason, evidence, updated_at
```

Нулевой in-flight допустим только как измеренный факт. Unknown upper duration не преобразуется в ETA0. `HTTP 9/16` в compact view означает **активные запросы / capacity**, а не completed attempts / неизвестный total. Сумма requests за run показывается отдельно в final summary как `Requests N`, retries отдельно.

Для opaque extension нельзя выдумывать actual HTTP in-flight; показывать reservation отдельно. Для shared quota нельзя выдавать сумму wait durations за elapsed wall time.

### 6.3. Job progress

Сохранить семантику §6 `next.md`:

```text
execution_percent = 100 * finished_jobs / planned_jobs
planned_jobs = finished_jobs + running_jobs + queued_jobs + cancelled_jobs
```

`finished_jobs` включает реальные terminal outcomes и обоснованные skips/blocks; cancelled jobs учитываются отдельно и не дорисовывают interrupted run до100%. Requirements, HTTP attempts и OTel spans не увеличивают progress denominator.

Before Freeze: `Total work is not known yet`, никаких 0/0/100%. При no executable jobs: отдельный текст, percentage/ETA отсутствуют; не выдавать это за PASS.

### 6.4. Pacing и ETA

Хранить monotonic wait start и eligible deadline. На render tick:

```text
waited = now - wait_started
resumes_in = max(eligible_at - now, 0)
```

Revision scheduler обновляет deadline; продление может законно увеличить countdown. Wait end сразу убирает устаревшую причину. Для capacity wait без deadline — `next eligible unknown`, не `(0s)`.

Один обязательный regression fixture:

```text
одна группа, current capacity=1
cooldown remaining=61s
три оставшихся запроса по5s
finalization=2s
ETA_run=61+3*5+2=78s
```

Если duration запросов не известна, `ETA >=01:01; remaining work duration unknown`, не78s из воздуха. Для adaptive context без upper bound — unknown; остальные targets могут сохранять локальные estimates. Метод ETA задаётся §9 `next.md`, не throughput из bar library.

Estimator вызывается на существенном scheduler change и не чаще1/s в стабильном состоянии; renderer максимум4fps. `Elapsed`/countdown используют monotonic clock, а не последнее время прихода completion или изменение системных UTC-часов.

### 6.5. Rendering lifecycle

Ввести общий controller interface для app, вместо возврата конкретного `*progress.Reporter`:

```go
type Controller interface {
    Publish(ViewSnapshot) // bounded, no terminal I/O in caller
    Notice(OperatorNotice)
    Finish(FinalView) error // final view, flush, restore; idempotent
    Close() error
}
```

Это внутренний контракт; конкретные имена могут совпасть с существующим package API. `Publish` не выполняет write под lock и не создаёт goroutine на событие.

TTY: весь текущий frame заменяется на том же участке экрана. Реальные notices допускаются через owner, routine events — нет. Stable rows canonical `anthropic, google-genai, openai`; завершённые targets остаются на своих местах, пока хватает места.

Состояния `DONE / FAIL`, `FAILED_RUNTIME`, `INTERRUPTED` различаются. Target trace end с outcome `completed` сам по себе не знает verdict; его нельзя превращать в зелёный PASS.

Last job finished → FINALIZING. COMPLETED — только после trace verification и последнего успешного artifact commit. После final newline запрещены поздние redraws/heartbeat. `Close`/second signal не должны ломать cursor state.

## 7. Визуальная приёмка: использовать согласованные кадры

`cli-preview.html` и §10 `next.md` остаются источником layout. Сцены из HTML извлечены в `approved-scenes.json` в наборе аудита. **Это expected fixtures, не запись нового работающего UI.** Все числа ниже синтетические; они не реконструируют 27-job запуск из clipboard.

### 7.1. Wide, ≥110 columns

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

Именно этот кадр является whole-frame View. Нельзя создавать независимый bar writer для каждой строки.

### 7.2. Cooldown tail

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

Через1s при неизменном расписании: elapsed09:04, resumes01:00, waited00:09; whole-run ETA ориентировочно01:17. Неизменный `(1m1s)` — FAIL приёмки.

### 7.3. 60–109 columns

```text
AIQA RUNNING  27/30 jobs (90%)
Elapsed 09:03 | ETA ~01:18 | HTTP 0/16
anthropic     DONE       10/10  PASS
google-genai  DONE        8/8   PASS-WITH-GAPS
openai        THROTTLED   9/12  ETA ~01:16
  Retry-After: resumes in 01:01; waited 00:08
```

### 7.4. <60 columns

```text
AIQA 27/30 jobs | 90%
Elapsed 09:03 | ETA ~01:18
HTTP 0/16 | targets 2/3 done
openai THROTTLED
Retry-After: resumes in 01:01
```

При ограничении высоты сначала удалять secondary detail/range, затем done rows, но не скрывать phase, primary blocker или runtime error. Не просто `lines = lines[:height]`: нижний важный blocker может исчезнуть.

### 7.5. Finalization и финал

```text
AIQA  baseline-compat-report                                       FINALIZING
Jobs 146/146 (100%)   Elapsed 07:48   ETA estimating
Requests complete. Verifying private trace and committing report files...
```

После окончательной успешной записи:

```text
AIQA completed in 07:52  |  Run verdict PASS-WITH-GAPS  |  Exit 0
Targets 3/3 finished  |  Jobs 146/146  |  Requests 238  |  Retries 4
Reports: baseline-compat-report.md, baseline-compat-report.json
Private diagnostics: enabled  |  Trace integrity PASS
```

Никакого `completed — completed`. Trace integrity отсутствует при verbosity0 — отображать `not requested`, не PASS.

### 7.6. Appearance contract

- Default terminal background/foreground, без full-screen заливки.
- Header/ключевые labels выделены; RUNNING cyan/blue, THROTTLED/WARN amber, FAIL/ERROR red, PASS green, SKIP/N/A neutral. Bar показывает работу, не «качество», и не зеленеет по факту процента.
- `--color=never` и auto + nonempty NO_COLOR убирают стили, но **не in-place cursor navigation**. `--progress=plain` убирает и navigation.
- `--unicode=never` — ASCII; UTF-8 layout с grapheme-safe width, не `len(bytes)`.
- Каждая строка ≤width−1 terminal cells; safe middle truncation labels, неизменные raw IDs.
- Color/ASCII views должны нести те же факты. Нельзя использовать цвет как единственное обозначение state.
- Animations не добавлять до acceptance parity; обязательны динамические значения, не spinner ради активности.

## 8. Точный план исправления по файлам

| Change | Файлы | Обязательный результат |
|---|---|---|
| R1: identity bridge | `internal/app/execution_progress.go`, `internal/progress/progress.go`, `internal/protocol/registry.go`, SDK lifecycle adapter | Trace IDs не считаются jobs; настоящие plan errors не скрыты |
| R2: runtime facts | `internal/protocol/{openai,anthropic,googlegenai}`, `internal/extsuite`, `internal/api`, `internal/scheduler` | Явный owner каждого attempt; подключённые waits и group updates; no double counting |
| R3: view model | `internal/terminal`, `internal/eta`, `internal/execution` | Поля и сцены §7, ETA реального remaining work, не hard-coded UI values |
| R4: renderer owner | Новый `internal/terminal/controller.go` + Bubble Tea adapter; app factory; CLI output policy | Единственный in-place TTY writer, no raw manual repaint path |
| R5: lifecycle | app finalization, cmd signals | FINALIZING удерживается до commit; graceful cancel/close/restoration |
| R6: acceptance | `test/terminal` или `internal/app` subprocess tests; Makefile/CI | Production binary → PTY → VT screen + scrollback assertions |
| R7: documentation | `docs/adr/cli-renderer.md`, `docs/monitoring.md`, `tasks/next.md` | Новая implementation policy и подтверждаемые delivery evidence |

R1 можно выпустить небольшим hotfix, если terminal spam мешает работе. Но R1 не закрывает N08/N09. R2–R6 должны дать один работающий вертикальный результат, а не очередные неиспользуемые packages.

Не менять API profile revisions, model routing, cost permission, concurrency semantics или scoring ради получения зелёных UI-тестов. Renderer не выбирает модели, не отправляет запросы и не меняет outcomes.

## 9. Обязательные критерии приёмки

Все критерии ниже — блокирующие для завершения monitoring UI, если не обозначено иное. Они дополняют A01–A44 `next.md`, не подменяют старые safety gates.

### 9.1. План и события

| ID | Сценарий | Проверяемое условие |
|---|---|---|
| UI01 | 1 frozen job + 500 корректных trace span starts/ends | 0 `unknown job` diagnostics; jobs total не меняется; trace остаётся полным |
| UI02 | Настоящий unknown JobStarted или conflicting terminal | Структурная runner error видима; denominator не меняется; provider verdict не выдумывается; graceful exit2 при нарушенном execution contract |
| UI03 | Duplicate terminal одного JobID/outcome | Один finished job, без второго terminal effect |
| UI04 | Native + SDK + retries | JobID из frozen binding, уникальные AttemptID; retries не увеличивают jobs total |
| UI05 | SDK fast/slow siblings | Fast suite terminal попадает в snapshot до release slow suite; final report ordering стабилен |
| UI06 | Prepare + runtime requests | Независимый ledger совпадает с owner/attempt counts; preparation не маркируется runtime job |

### 9.2. Экран и scrollback

| ID | Сценарий | Проверяемое условие |
|---|---|---|
| UI07 | Настоящий `./bin/aiqa` под PTY, `test=compat,sdk` | Wide header, общий bar, три постоянные target rows, HTTP/ETA/wait поля, а не old single-line Reporter |
| UI08 | 1000 updates, fixed terminal size, без durable notices | Одна live region; scrollback не растёт линейно с updates. На пустом терминале запас допустимого роста ≤2×max frame height на весь run |
| UI09 | Long label → shorter label | Никаких suffix fragments `genai.models.list`, `rn`, `letionstput`, `onsesam` из старого frame |
| UI10 | Detail rows 3→0 | Все исчезнувшие строки стёрты; предыдущий текст не остаётся ниже панели |
| UI11 | Resize120→80→48→120, height24→12→24 | Нет autowrap/garbage; каждый кадр помещается; primary blocker не потерян |
| UI12 | ASCII/UTF-8/CJK/combining/bidi/ESC labels | ≤width−1 cells, целые graphemes, no escape injection, сырой JobID в state не усечён |
| UI13 | Golden fixtures всех согласованных сцен | Plain screen text и порядок полей соответствуют §10 next / approved-scenes, не произвольному новому дизайну |
| UI14 | NO_COLOR или --color=never в TTY | Цветов нет, но inline redraw продолжает работать; не превращается в append-only |
| UI15 | Slow stderr / burst200 domain events/s | Broker/reducer locks не удерживаются writer; domain terminals не потеряны; display mailbox bounded; ≤4fps |

UI08 измеряет **терминальное состояние и scrollback**, а не количество LF в raw capture. Многострочный renderer законно отправляет LF вместе с cursor-up/erase; тест `count(raw,'\n')==0` сам был бы ошибочным. Для fixture с намеренным большим уменьшением высоты допускаются необходимые layout transitions; они не должны зависеть от числа completion events.

### 9.3. Ожидания, время, concurrency

| ID | Сценарий | Проверяемое условие |
|---|---|---|
| UI16 | Cooldown61s +3×5s +finalization2s | Run ETA78s при известных durations; никогда ETA14s; на tick без completion elapsed/waited растут, resumes уменьшается |
| UI17 | Deadline продлён, затем wait end | Countdown обновлён на следующем display tick; старый label исчезает; local pacing не назван Retry-After |
| UI18 | Unknown adaptive context остаток | ETAunknown/lower-bound с причиной; bounds/budget отображаются; бюджет не выдан за percent finished |
| UI19 | 3 независимых origins, C16 | Barriers подтверждают одновременный старт, ledger доказывает summed HTTP≤16; cooldown одного не тормозит другие |
| UI20 | Shared quota + C1 / opaque reservation / stream | UI отражает ту же capacity, что broker; slot удерживается до body terminal; opaque отдельно; queued не in-flight |
| UI21 | System wall-clock jump | Monotonic elapsed/countdown корректны и неотрицательны |
| UI22 | 100% jobs + искусственно заблокированный trace/report commit | Экран FINALIZING, никаких COMPLETED до release commit barrier |
| UI23 | Normal fail verdict и cancel | DONE/FAIL отличается от FAILED_RUNTIME и INTERRUPTED; отмена не дорисовывает100% |

### 9.4. Потоки и завершение

| ID | Сценарий | Проверяемое условие |
|---|---|---|
| UI24 | stderr PTY, stdout redirected | Interactive panel остаётся; `--stdout=json` — один parseable JSON, равный финальному файлу по содержимому |
| UI25 | stderr pipe / `/dev/null` / TERM=dumb / CI=true или1 | Никаких ESC/CR; plain milestones и30s heartbeat без completion; --progress=tty на pipe даёт fallback один раз |
| UI26 | --no-progress / --progress=off / --quiet | Routine UI отсутствует; explicit stdout и fatal errors сохраняются |
| UI27 | --verbose0 versus4 на одном fixture | Те же jobs/state/layout semantics; verbosity4 не порождает probe diagnostics в UI; sidecars отличаются только по контракту |
| UI28 | First SIGINT, SIGTERM; second signal | Cancellation + partial result; cursor/attrs восстановлены; нет redraw после закрытия; log --follow тоже останавливается |
| UI29 | Success / handled panic / renderer failure | Единственный финал, корректная terminal cleanup; нельзя доказать обработку SIGKILL и не требуется |
| UI30 | Golden empty plan / missing instrumentation | Нет0/0=100%, фиктивных HTTP0 или PASS; unavailable явно отличено от измеренного нуля |
| UI31 | Secret/URL/body canaries в mock errors/IDs | Ни UI, ни capture, ни public execution не содержат secrets, URLs, prompts, outputs или media |
| UI32 | Binary provenance | Build SHA, module pin и точная команда записаны рядом с captured evidence; запускается абсолютный путь проверенной сборки |
| UI33 | API success outcome | Transport `succeeded` остаётся success в reducer; unknown enum не маскируется под failed provider test |

### 9.5. Что приложить к завершённой реализации

Для review обязательно предоставить:

1. `build-info.txt`: полный commit, `./bin/aiqa --version`, `go version -m ./bin/aiqa`, OS, effective progress mode, TERM/locale и dimensions; без API credentials.
2. Raw PTY recording (`.cast` или byte stream + timestamps) **настоящего CLI**, не Python/HTML макета.
3. Decoded screen states 120/80/48 columns и промежуточные frames подготовки, параллельного исполнения, cooldown, финализации, cancel.
4. Snapshot/event fixture и независимый mock HTTP ledger, подтверждающие показанные цифры.
5. CI output для `make qa` и нового `make ui-acceptance`, плюс результаты A01–A44, затронутых этим изменением.
6. Короткую запись terminal session или последовательность кадров для визуального review. Красивый static HTML не является evidence реализованного продукта.

Эти файлы должны быть доступны как CI artifacts. В summary задачи перечислить acceptance IDs → test names → artifact paths; не писать только «all tests passed».

## 10. Как строить настоящий PTY gate

Создать `make ui-acceptance` как новый target; сейчас не выдавать его за существующую команду.

Test harness обязан:

- собрать актуальный CLI с Go toolchain проекта;
- запустить три независимых loopback mock origins и контролировать slow/fast/cooldown с barriers/fake-clock seams;
- задать только fixture credentials в очищенном окружении, не наследовать настоящие API keys;
- подключить **stderr к slave PTY**, stdout к отдельному pipe/file; сохранить обоих;
- установить winsize через ioctl и TERM/locale; открыть terminal-emulator reader на master FD;
- интерпретировать cursor/erase sequences полноценным VT emulator и проверять screen + scrollback;
- не использовать regex-strip-ANSI как замену terminal emulation;
- проверять resize и terminal cleanup в subprocess, не на fake `applicationRunner`;
- сохранить промежуточные кадры до завершения процесса, чтобы detect99% plateau/FINALIZING нельзя было обойти только final output;
- не использовать `2>stderr.log` для проверки TTY — это намеренно включает plain branch;
- не зависеть от скорости сети или «в среднем быстрее»: concurrency доказывается независимым ledger/barriers.

Сборка/provenance для приёмки:

```bash
make build VERSION=ui-review COMMIT="$(git rev-parse HEAD)" DATE=ci
./bin/aiqa --version
go version -m ./bin/aiqa
```

Команда из пользовательского приложения должна работать **без новых обязательных UI flags**:

```bash
./bin/aiqa --verbose 4 --concurrency 16 --timeout=60 \
  test=compat,sdk \
  --format=markdown,json --name=compat-report
```

Большой сценарий из `next.md` тоже остаётся regression fixture:

```bash
./bin/aiqa --verbose 4 --concurrency=16 --timeout=120 \
  test=compat,model-param,sdk \
  --model=claude-sonnet-5,gemini-3.8-flash,gpt-5.4-mini,gpt-image-2 \
  --module=auto,images-generations \
  --allow-billable-artifacts --max-artifacts-per-model=2 \
  --format=markdown,json --name=baseline-compat-report
```

В CI это mock model IDs, не утверждение о текущих публичных моделях. Реальные платные вызовы не нужны для доказательства корректной перерисовки.

## 11. Порядок поставки и запреты на ложное закрытие

**Первый шаг:** исправить U01, добавить regression test через фактический `executionProgress.ObserveLifecycle`; три positive controls сохранить. Подавление stdout/stderr диагностик без исправления identity path не принимать.

**Второй шаг:** подключить runtime owners / waits / group limits и view mapper. Добиться корректных numbers на ledger-backed fixture. Нельзя подставлять фиксированные значения ради скриншота.

**Третий шаг:** включить Bubble Tea controller в ту фабрику, которой действительно пользуется CLI; удалить старый production TTY path. Довести `terminal.Render` до согласованных сцен.

**Четвёртый шаг:** PTY gates UI01–UI33, затем existing regression suite. Сохранить записи и скрин-состояния в CI artifacts.

**Пятый шаг:** обновить ADR, docs и status ledger `next.md` только на основании прохождения нужных acceptance tests.

Не являются исправлением:

- `--progress=plain/off` по умолчанию;
- принудительная concurrency1, отключение SDK или verbosity4;
- включение alternate screen, чтобы спрятать scrollback-spam;
- удаление всех unknown-event/integrity checks;
- `\r\x1b[K` как единственная закрывающая правка;
- красивый standalone demo, который не вызывается из `./bin/aiqa`;
- наличие новой dependency в `go.mod` без actual runtime-path;
- повторное включение библиотечной ETA по среднему jobs/min;
- зелёный unit CI без настоящего PTY gate.

## 12. Вывод

Основной визуальный finding первого аудита остаётся открытым. Текущая копипаста и текущий source согласуются: telemetry-span IDs ошибочно превращаются в jobs, diagnostic helper фиксирует каждый repaint новой строкой, а CR-only writer оставляет хвосты. Новый layout не подключён и пока недостаточен сам по себе.

Исправление должно завершить **пользовательскую функцию целиком**: корректные события → измеренный snapshot → честная ETA → согласованный frame → проверенный inline renderer → работающий настоящий CLI. До этого N07–N09 нельзя закрывать как выполненные, даже если внутренние scheduler исправления уже полезны и CI зелёный.

Репозиторий этим аудитом не изменялся. Новый renderer, ADR и перечисленные acceptance targets — прямые требования к следующему исправлению, а не заявление об уже выполненной реализации.

## Sources

Все ссылки AIQA привязаны к проверенному immutable commit, а не к движущемуся main.

- [S1 — HEAD](https://github.com/ilyar/aiqa/commit/c66dbd8d51f6f828173bbc71e78801c47f7bf56f).
- [S2 — diff от первого аудита](https://github.com/ilyar/aiqa/compare/6b92b97cb09f7840a41d0e8aa66922f8787ca2ba...c66dbd8d51f6f828173bbc71e78801c47f7bf56f).
- [S3 — actual app orchestration / factory](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/app/app.go).
- [S4 — legacy progress renderer / lifecycle](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/progress/progress.go#L264-L529).
- [S5 — execution bridge](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/app/execution_progress.go).
- [S6 — pure terminal renderer](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/terminal/terminal.go).
- [S7 — current progress tests](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/progress/progress_test.go).
- [S8 — dependencies](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/go.mod).
- [S9 — normative next.md](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/tasks/next.md#L429-L632).
- [S10 — scheduler wakeups and facts](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/scheduler/scheduler.go).
- [S11 — API admission / owners / memory](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/api/client.go).
- [S12 — committed artifact rejection](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/scheduler/scheduler.go#L455-L480).
- [S13 — executable reservation / SDK terminal](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/extsuite/runner.go#L385-L510).
- [S14 — prepared plan and observability contract](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/internal/protocol/registry.go).
- [S15 — log signal routing](https://github.com/ilyar/aiqa/blob/c66dbd8d51f6f828173bbc71e78801c47f7bf56f/cmd/aiqa/main.go#L44-L110).
- [S16 — CI exact HEAD](https://github.com/ilyar/aiqa/actions/runs/34377516746).
- [L1 — Bubble Tea v2.0.9 release](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.9).
- [L2 — pinned Program options](https://github.com/charmbracelet/bubbletea/blob/v2.0.9/options.go).
- [L3 — pinned v2 Model / View](https://github.com/charmbracelet/bubbletea/blob/v2.0.9/tea.go#L54-L175).
- [L4 — published pinned Go package documentation](https://pkg.go.dev/charm.land/bubbletea/v2@v2.0.9).

Локальные основания: [out.txt](out.txt), предыдущий [aiqa-next-implementation-audit.md](aiqa-next-implementation-audit.md), [next.md](next.md), [cli-preview.html](cli-preview.html). Их происхождение и SHA указаны в audit-manifest/approved-scenes; `.cast` настоящего нового UI в этом аудите не создавался и не подменяется статическим макетом.
