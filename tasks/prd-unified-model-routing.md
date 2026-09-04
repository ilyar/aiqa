# PRD: единая маршрутизация моделей и достоверные результаты AIQA — аудит

**Статус:** реализовано и локально проверено

**Дата аудита:** 2026-09-04

Этот файл редуцирован после реализации. Канонический пользовательский и
инженерный контракт находится в
[`docs/unified-model-routing.md`](../docs/unified-model-routing.md). Здесь
сохранена только трассировка требований и границы доказательства.

## Итог

AIQA принимает единый глобальный `--model`, получает inventory каждой активной
protocol target, пересекает списки по точному target-local ID и передаёт в
native/Official SDK suites только фактически разрешённые target/model пары.
Неоднозначная явно запрошенная text-модель получает один минимальный preflight;
route rejection блокирует каскад, а transient failure остаётся inconclusive.
Text- и image-routing разделены, billable/high-token/persistent gates сохранены,
assessment и guidance больше не маскируют пропуски или зависимые ошибки, а
telemetry считается транспортом.

## Аудит user stories

| История | Результат | Локальное доказательство |
|---|---|---|
| US-001 Единый CLI-список | PASS | `internal/config/config.go`, `internal/config/config_test.go`: CSV trim/empty/duplicate, удалённые flags и env migration error |
| US-002 Selection по inventory | PASS | `internal/protocol/selection.go`, `selection_test.go`, `internal/app/app_test.go:357`: exact match, Google `models/`, отсутствующий ID без model calls |
| US-003 Ambiguous route | PASS | `internal/app/app_test.go:392`, `:440`, `:477`: один preflight, route rejection/transient blocking, image без billable preflight |
| US-004 Multi-protocol модель | PASS | `internal/app/anthropic_test.go:140`: один ID независимо проходит OpenAI и Anthropic, target-qualified evidence и independent provider FAIL |
| US-005 Text + image | PASS | `internal/capability/router_test.go`, `internal/app/app_test.go:477`: capability-local modules, image authorization и отсутствие Chat-запроса image-модели |
| US-006 Materialization | PASS | `internal/assessment/assessment.go`, `assessment_test.go:180`, image routing fixture: applicable skipped modules видимы, executed missing probe даёт ERROR |
| US-007 Dependency evidence | PASS | `internal/suites/compat_test.go:160`, `images_test.go`: dependents остаются SKIP с `blocked_by_requirement` |
| US-008 Anthropic SDK tools | PASS | `internal/extsuite/builtin/anthropic/anthropic.go`, `anthropic_test.go`: общий prompt/schema/validator для `get_status(resource-alpha)` |
| US-009 Anthropic Models schema | PASS | `internal/protocol/anthropic/validate.go`, `validate_test.go`: pinned v1.68.0 fields и nested capability groups |
| US-010 HTTP telemetry | PASS | `internal/api/client.go`, `pacing.go`, `client_test.go`, `pacing_test.go`: logical/physical/retry/recovery/exhaustion и unique latency |
| US-011 Root-cause guidance | PASS | `internal/guidance/engine.go`, `engine_test.go`, fixture goldens: grouping по subject/root и transitive blocked dependents |
| US-012 Report provenance | PASS | `internal/app/app.go`, `internal/report`, `schema/report-v3.schema.json`, report fixtures: global request один раз, target rows selected фактически |
| US-013 Docs и migration | PASS | README, `docs/`, profile/packs/examples, `.github/workflows/ci.yml`, `CHANGELOG.md` обновлены на global `--model` |
| US-014 Сквозная проверка | PASS local | Focused fixtures и repository gates ниже; live provider acceptance не входит в локальный доказательный контур |

## Аудит functional requirements

| Требование | Результат | Доказательство / граница |
|---|---|---|
| FR-1 | PASS | Global `--model` — единственный model-selection option |
| FR-2 | PASS | `Config.Models` передаётся всем активным runners |
| FR-3 | PASS | `--target` выбирает protocol, модель target не активирует |
| FR-4 | PASS | Removed flags дают migration error до `flag.Parse` failure |
| FR-5 | PASS | `GOOGLE_GENAI_MODEL` больше не выбирает; active Google target даёт migration error |
| FR-6 | PASS | Capability overrides применяются после inventory membership |
| FR-7 | PASS | `TargetConfig.Models` используется только как internal resolved-list fallback |
| FR-8 | PASS | Raw discovery entries и discovery validation сохраняются до selection |
| FR-9 | PASS | Selection требует exact normalized membership |
| FR-10 | PASS | Missing ID не получает synthetic metadata или probe |
| FR-11 | PASS | Trust считается по записи; duplicate-safe metadata не затирает trusted entry |
| FR-12 | PASS | Anthropic/Google используют только protocol-local classifiers и metadata |
| FR-13 | PASS | Default unknown policy — inventory; fallback только explicit opt-in |
| FR-14 | PASS | Preflight вызывается только для explicit ambiguous candidate |
| FR-15 | PASS | Preflight minimal, non-persistent, non-high-token, non-billable |
| FR-16 | PASS | Explicit route rejection останавливает downstream model suite |
| FR-17 | PASS | Transient preflight сохраняет один ERROR/inconclusive root |
| FR-18 | PASS | Каждый target выполняет одну подтверждённую модель независимо |
| FR-19 | PASS | Successful zero-match discovery — допустимый target result |
| FR-20 | PASS | Discovery failure видим как ERROR/inconclusive, не silent absence |
| FR-21 | PASS | Module plan строится на каждой target/model pair |
| FR-22 | PASS | `auto` включает applicable non-billable default modules |
| FR-23 | PASS | Billable auto-module требует `--allow-billable-artifacts` |
| FR-24 | PASS | Explicit module не обходит cost/safety gates |
| FR-25 | PASS | Artifact budget scoped target/model и общий native/SDK |
| FR-26 | PASS | Exhausted budget — skip reason, не provider FAIL |
| FR-27 | PASS | Каждый applicable profile probe materialized в canonical assessment |
| FR-28 | PASS | Dependent evidence — SKIP с `blocked_by_requirement` |
| FR-29 | PASS | Provider defect создаётся только прямым observation |
| FR-30 | PASS | Missing core evidence даёт inconclusive, если нет direct FAIL |
| FR-31 | PASS | Executed module без declared probe даёт coverage ERROR |
| FR-32 | PASS | 429/5xx/timeout/transport/runner failure — ERROR |
| FR-33 | PASS | Optional stable endpoint rejection — UNSUP только по profile policy |
| FR-34 | PASS | Schema/semantic mismatch остаётся FAIL после routing |
| FR-35 | PASS | Globally absent ID не создаёт requirement/finding и не меняет exit |
| FR-36 | PASS | Anthropic SDK input и validator используют один contract |
| FR-37 | PASS | Native и SDK tools требуют `get_status(resource-alpha)` |
| FR-38 | PASS | Extensions получают только resolved target models |
| FR-39 | PASS | Native/SDK evidence связываются одним requirement с разными IDs |
| FR-40 | PASS | Request stats принадлежат transport, не TestResult rows |
| FR-41 | PASS | Один successful physical attempt добавляет один latency sample |
| FR-42 | PASS | Retry attempts/recovered/exhausted — отдельные counters |
| FR-43 | PASS | Model-addressed URL/body/raw operations сохраняют attribution |
| FR-44 | PASS | Report называет adaptive client pacing state, не provider limit |
| FR-45 | PASS | При zero successes safe rate/concurrency отсутствуют и в JSON, и как N/S в Markdown |
| FR-46 | PASS | Native и Official SDK telemetry разделены |
| FR-47 | PASS | Guidance группируется по stable root cause и subject |
| FR-48 | PASS | Blocked dependents входят в root finding, но не в actionable coverage |
| FR-49 | PASS | Global requested list один раз; target rows — selected фактически |
| FR-50 | PASS | Markdown не предупреждает о global silent absence |
| FR-51 | PASS | JSON/Markdown разделяют conformance и telemetry semantics |
| FR-52 | PASS | Schema, profiles, manifests, docs, examples и workflow синхронизированы |

## Acceptance и остающиеся границы

| Сценарий | Результат |
|---|---|
| Mixed inventory, text/image, без billable authorization | PASS local: routing и authorization fixtures; generation без opt-in отсутствует |
| Mixed inventory с billable authorization | PASS local: authorized image path, artifact limit и native/SDK shared budget |
| ID отсутствует во всех inventories | PASS local: `TestMissingRequestedModelIsSilentAndDoesNotProbe` |
| Discovery failure | PASS local: discovery остаётся видимым, model work не синтезируется |

Live Scenario B, реальные provider credentials и billable artifact authorization
не выполнялись: это отдельная операция с расходом и не разрешена этим локальным
изменением. Merge, push, publication и deployment также не выполнялись.

## Финальные локальные gates

```text
GOCACHE=/tmp/aiqa-gocache go test ./...
GOCACHE=/tmp/aiqa-gocache make check
GOCACHE=/tmp/aiqa-gocache make race
GOCACHE=/tmp/aiqa-gocache make security-check
```

Каноническая спецификация поведения и команды проверки: [`docs/unified-model-routing.md`](../docs/unified-model-routing.md).
