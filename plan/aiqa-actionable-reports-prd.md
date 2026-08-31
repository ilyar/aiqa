# PRD: Actionable AIQA Reports

**Документ:** `docs/actionable-reports-prd.md`  
**Статус:** Draft  
**Целевой продукт:** AIQA  
**Целевая схема:** совместимое расширение `aiqa.report.v1`, затем `aiqa.report.v2`  
**Основание:** [`docs/scoring.md`](https://github.com/ilyar/aiqa/blob/main/docs/scoring.md)  
**Дата:** 2026-08-31

---

## 0. Решение

AIQA должен перейти от отчёта вида «вердикт + проценты + таблица сырых тестов» к **доказательному отчёту с планом исправления**.

Каждый отчёт обязан отвечать на пять вопросов в таком порядке:

1. **Соответствует ли инфраструктура целевому профилю?**
2. **Какие обязательные и дополнительные требования нарушены или не доказаны?**
3. **Что исправлять первым?**
4. **Где именно вероятнее всего находится проблема и насколько уверенно это установлено?**
5. **Как проверить, что исправление сработало?**

Scoring остаётся таким, как определено в `docs/scoring.md`:

- authoritative categorical verdict;
- `Core conformance`;
- `Optional capability coverage`;
- `Evidence coverage`;
- raw counts;
- временный `legacy_score`, не используемый как основной рейтинг.

Рекомендации образуют отдельный слой и **никогда не изменяют результаты probes, canonical requirements, verdict или coverage**.

Целевая архитектура:

```text
raw probe observations
        ↓
canonical requirement assessments
        ↓
normalized findings
        ↓
curated recommendations
        ↓
JSON source of truth
        ↓
Markdown / HTML / SARIF renderers
```

Ключевой принцип, заимствованный у Lighthouse: отчёт должен содержать не только оценку, но и структурированные audit results с объяснением, подробностями, предупреждениями, документацией и guidance. При этом AIQA не должен копировать модель единого weighted score Lighthouse: conformance-профиль остаётся hard gate.

---

## 1. Контекст

Текущий AIQA уже умеет:

- выполнять нативные behavioral probes;
- подключать проверки через официальный OpenAI SDK;
- различать `pass`, `fail`, `warn`, `unsupported`, `skip`, `error`;
- сохранять HTTP-наблюдения, usage, metrics и фрагменты ответов;
- формировать Markdown и JSON;
- показывать общий verdict и числовые показатели;
- фиксировать точную ревизию OpenAI-профиля.

Однако текущий отчёт в основном сообщает **что произошло**, но не приводит пользователя к безопасному и проверяемому ответу **что делать дальше**.

Примеры из реальных отчётов:

- `created is missing in 30/30 model entries`;
- invalid request type приводит к `502`;
- `logprobs` принят, но данные не возвращены;
- `tool_choice=required` отклоняется только в thinking mode;
- нативный stream probe проходит, а проверка через официальный SDK сообщает `stream chunk missing choices`;
- один и тот же внешний log повторяется в Markdown несколько раз.

Читатель должен самостоятельно:

1. понять, относится ли проблема к gateway, model adapter, upstream model, client configuration или runner;
2. отличить контрактное нарушение от опционального capability gap;
3. найти нормативный источник;
4. сформулировать исправление;
5. определить порядок работ;
6. придумать проверку после исправления.

Это слишком много ручной герменевтики для инструмента, который уже собрал почти все исходные доказательства.

---

## 2. Что именно берём из Lighthouse

Lighthouse используется как архитектурный ориентир, а не как шаблон scoring.

### 2.1. JSON является источником истины

Lighthouse Result Object содержит всё, что затем отображается в HTML, включая audit metadata, результаты, warnings, explanations и details.

**Требование к AIQA:** Markdown и будущий HTML должны быть чистыми renderers одного JSON-отчёта. Renderer не должен придумывать диагнозы, приоритеты или команды.

### 2.2. Audit хранит не только outcome

У Lighthouse audit result включает:

- стабильный `id`;
- success/failure title;
- описание важности;
- `explanation`;
- `warnings`;
- `errorMessage`;
- `displayValue`;
- `scoreDisplayMode`;
- структурированные `details`;
- ссылку на документацию;
- `guidanceLevel`.

**Требование к AIQA:** canonical requirement и finding должны нести эквивалентный объяснительный контракт, а recommendation — точный action plan.

### 2.3. Не всё обязано иметь score

Lighthouse различает:

- binary/numeric;
- informative;
- manual;
- not applicable;
- error.

Для `informative`, `manual`, `notApplicable` и `error` score равен `null` и игнорируется.

**Требование к AIQA:** `not_applicable`, `skip`, `error` и неопределённые результаты нельзя маскировать под нулевую совместимость или усреднять с доказанными outcomes.

### 2.4. Opportunities отделены от Diagnostics

Lighthouse отделяет:

- **opportunities/insights** — конкретные действия и потенциальный эффект;
- **diagnostics** — сведения, которые помогают исследовать проблему, но не гарантируют готовое исправление;
- passed, manual и not-applicable checks — отдельные группы.

**Требование к AIQA:** не каждый non-pass должен изображать готовый fix. Когда причина не доказана, отчёт обязан предложить investigation или rerun, а не убедительно выдуманный патч.

### 2.5. Guidance level отделён от impact

Lighthouse использует `guidanceLevel` 1–3: насколько конкретно инструмент объясняет исправление. В renderer рекомендации могут сортироваться с учётом потенциального impact и уровня guidance.

**Требование к AIQA:** отдельно хранить:

- **priority/impact** — насколько проблема влияет на conformance;
- **guidance level** — насколько конкретно AIQA умеет помочь;
- **confidence** — насколько надёжны диагноз и attribution.

Высокий impact не означает, что причина известна. Высокая confidence не означает, что исправление простое.

### 2.6. Stack Packs

Lighthouse Stack Packs добавляют технологически специфичные рекомендации поверх общих audits.

**Требование к AIQA:** поддержать versioned advice packs для конкретных gateways, runtimes и adapters. Pack может уточнить шаги или workaround, но не вправе менять факты, status, requirement level, scoring или verdict.

### 2.7. Что не копируем

Lighthouse использует weighted category scores для performance и других web-аудитов. Для AIQA это неприемлемо как основной conformance verdict:

- required failure не может быть компенсирован optional passes;
- `ERROR` не равен fail;
- `N/A` не участвует в расчёте;
- capability coverage не является core conformance.

Scoring и recommendation ranking остаются независимыми системами.

---

## 3. Проблема продукта

### 3.1. Основная проблема

AIQA сообщает технические симптомы, но не формирует:

- нормализованный finding;
- вероятную область ответственности;
- приоритет исправления;
- конкретные действия;
- нормативное обоснование;
- способ проверки;
- безопасный workaround;
- ожидаемое изменение conformance после исправления.

### 3.2. Проблемы текущего представления

1. Raw probes и external suites выглядят как независимые проблемы, даже когда являются двумя доказательствами одного canonical requirement.
2. Одна root cause создаёт несколько строк и может породить несколько одинаковых советов.
3. Сырые внешние logs повторяются в Markdown.
4. Невозможно быстро отличить:
   - дефект контракта;
   - model capability gap;
   - configuration conflict;
   - runner/network error;
   - конфликт двух тестовых инструментов.
5. Нет чёткого target audience:
   - gateway maintainer;
   - model integrator;
   - client developer;
   - QA operator.
6. Нет ответа «как доказать, что исправлено».
7. Нет стабильных finding fingerprints для сравнения отчётов во времени.
8. Рекомендации, построенные из свободного текста `summary`, были бы хрупкими и недетерминированными.

---

## 4. Цели

### G1. Actionability

Пользователь должен определить первое действие не более чем за 30 секунд после открытия отчёта.

### G2. Evidence-first

Каждая рекомендация должна ссылаться на конкретные canonical requirements и evidence.

### G3. Safe attribution

AIQA должен явно различать observed scope и предполагаемый component owner и показывать confidence.

### G4. Determinism

Одинаковый report + одинаковая версия каталога должны давать одинаковые findings, рекомендации, порядок и fingerprints.

### G5. Verifiability

Каждая P0/P1-рекомендация должна содержать процедуру проверки после исправления.

### G6. Scoring integrity

Guidance не должен менять scoring, verdict или applicability.

### G7. Deduplication

Одна root cause должна отображаться как один finding/action plan с перечислением затронутых требований и моделей.

### G8. Maintainability

Рекомендации должны обновляться независимо от кода probes и независимо от нормативной ревизии профиля, с явным versioning.

### G9. Machine readability

JSON должен позволять CI, dashboards, issue bots и будущий HTML renderer использовать те же данные без parsing Markdown.

### G10. Graceful uncertainty

Когда безопасное исправление неизвестно, AIQA должен честно вернуть diagnostic/investigation guidance, а не имитировать знание.

---

## 5. Не-цели

AIQA Actionable Reports не должны:

- автоматически менять gateway, provider configuration или model routing;
- утверждать root cause, если доказан только симптом;
- считать model-level capability gap gateway-level defect без evidence;
- оценивать качество ответа модели как benchmark;
- выдавать security certification;
- обещать точный effort или cost без измеримых данных;
- генерировать команды с реальными API keys;
- отправлять report во внешний LLM по умолчанию;
- позволять LLM менять status, priority, verdict, requirement mapping или evidence;
- превращать каждый warning в срочную задачу;
- рекомендовать реализацию неприменимого (`not_applicable`) capability;
- дублировать canonical requirements из-за нескольких probes или SDK clients.

---

## 6. Пользователи

### 6.1. Gateway maintainer

Нужен ответ:

- какой endpoint нарушает контракт;
- какое поле/событие/HTTP-поведение отличается;
- какое изменение внести на boundary;
- какие модели затронуты;
- каким тестом подтвердить исправление.

### 6.2. Model integrator

Нужен ответ:

- является ли ограничение свойством модели или adapter;
- зависит ли оно от reasoning/thinking mode;
- возможна ли трансляция параметров;
- какой compatibility matrix документировать.

### 6.3. Client developer

Нужен ответ:

- можно ли использовать официальный SDK;
- какая feature требует workaround;
- какие параметры нельзя передавать;
- как избежать silent acceptance.

### 6.4. QA operator

Нужен ответ:

- является ли run conclusive;
- что перезапустить;
- какие проверки заблокированы prerequisite failure;
- где лежит полное evidence;
- какие finding появились или исчезли относительно baseline.

### 6.5. CI consumer

Нужны:

- стабильные IDs;
- однозначные statuses;
- machine-readable priority;
- fingerprint;
- projected impact;
- ссылки на evidence;
- отсутствие breaking changes без schema bump.

---

## 7. Продуктовые принципы

1. **Verdict first, actions second, raw evidence last.**
2. **Requirement — единица conformance; probe — единица evidence.**
3. **Finding — диагноз наблюдаемого несоответствия; recommendation — отдельный план действия.**
4. **Никаких рекомендаций из parsing человеческого `summary`.**
5. **Никакого автоматического blame без attribution confidence.**
6. **Unsupported, failed, inconclusive и not applicable требуют разных советов.**
7. **Один symptom может иметь несколько hypotheses; одна hypothesis не становится fact от красивой формулировки.**
8. **Рекомендация обязана иметь verification.**
9. **Проектируемый эффект не является гарантией.**
10. **Advice packs улучшают конкретность, но не переписывают результаты теста.**
11. **Renderer ничего не анализирует.**
12. **Полные сырые данные хранятся один раз и переиспользуются через references.**

---

## 8. Термины

| Термин | Определение |
|---|---|
| **Profile** | Версионированный целевой контракт совместимости |
| **Canonical requirement** | Нормативная единица conformance, учитываемая в verdict и coverage один раз |
| **Probe** | Исполняемый эксперимент, создающий evidence |
| **Evidence** | Нормализованное наблюдение: request shape, response shape, status, metrics, SDK result |
| **Finding** | Дедуплицированное описание подтверждённой проблемы, gap или неопределённости |
| **Recommendation** | Проверяемый план исправления, настройки, документирования или исследования |
| **Diagnostic** | Finding, для которого безопасный конкретный fix не доказан |
| **Advice catalog** | Версионированный набор общих remediation rules |
| **Advice pack** | Provider/runtime-specific расширение общих рекомендаций |
| **Guidance level** | Степень конкретности рекомендации, 1–3 |
| **Confidence** | Надёжность finding/attribution/recommendation |
| **Potential impact** | Условное изменение verdict/coverage при успешном исправлении |
| **Observed scope** | Где симптом был фактически наблюдён |
| **Attribution** | Где предположительно требуется изменение |
| **Fingerprint** | Стабильный идентификатор логически того же finding между runs |

---

## 9. Целевая модель результата

### 9.1. Три независимых слоя

#### Layer A — Conformance

Определён в `docs/scoring.md`:

```text
verdict
core conformance
optional capability coverage
evidence coverage
raw status counts
```

#### Layer B — Findings

Нормализует и объединяет evidence:

```text
what failed / is unsupported / is inconclusive
where it was observed
which requirements are affected
what evidence supports it
whether sources conflict
```

#### Layer C — Guidance

Определяет:

```text
what to do
who should do it
why
how urgent
how confident
how to verify
what changes in conformance are expected
```

### 9.2. Запрещённая зависимость

```text
recommendation -> score/verdict
```

никогда не допускается.

Разрешённая зависимость:

```text
score/verdict + requirement assessments + evidence
    -> finding
    -> recommendation
```

---

## 10. Finding pipeline

### 10.1. Нормализация probes

Каждый probe или external adapter должен вернуть не только human text, но и typed outcome:

```json
{
  "probe_id": "aiqa.native.models.list.schema",
  "requirement_id": "models.list.schema",
  "status": "fail",
  "signals": [
    {
      "code": "contract.models.list.entry.created_missing",
      "parameters": {
        "missing_entries": 30,
        "total_entries": 30,
        "json_pointer": "/data/*/created"
      }
    }
  ]
}
```

`summary`, `observation` и `error` остаются presentation fields, но не используются как rule input.

### 10.2. Finding classes

Stable JSON values:

```text
contract_violation
behavior_mismatch
unsupported_capability
configuration_constraint
evidence_conflict
evidence_gap
execution_failure
documentation_gap
runner_defect
```

### 10.3. Evidence state

```text
confirmed
probable
conflicting
inconclusive
```

Правила:

- `confirmed`: прямое воспроизводимое evidence без противоречий;
- `probable`: несколько косвенных наблюдений указывают на одну причину;
- `conflicting`: trustworthy probes дают несовместимые выводы;
- `inconclusive`: run/network/provider state не позволил сделать вывод.

### 10.4. Subject и attribution

```json
{
  "subject": {
    "kind": "provider",
    "id": "https://vibecode.moe/v1"
  },
  "observed_scope": {
    "endpoint": "GET /v1/models",
    "models": ["*"]
  },
  "attribution": {
    "component": "provider_gateway",
    "confidence": "high",
    "basis": "response schema is produced by the provider inventory endpoint"
  }
}
```

Допустимые components:

```text
provider_gateway
model_adapter
upstream_model
client_configuration
network_or_runtime
aiqa_runner
external_checker
unknown
```

Наличие одного model failure не даёт права автоматически выставить `provider_gateway`.

### 10.5. Finding fingerprint

Рекомендуемая формула:

```text
sha256(
  profile.id
  + profile.revision-major
  + requirement_id
  + subject.kind
  + normalized subject id
  + finding_code
  + normalized root-cause parameters
)
```

Не включать:

- timestamp;
- request ID;
- latency;
- случайный test model marker;
- raw error text целиком;
- порядок probes.

### 10.6. Дедупликация

Finding объединяется, если совпали:

- `finding_code`;
- subject/observed scope;
- root-cause key;
- совместимые evidence states.

Пример: `chat.tools.required`, `chat.tools.forced` и `chat.tools.stream` возвращают один и тот же 400 о запрете `tool_choice` в thinking mode. Отчёт создаёт **один** finding с тремя affected requirements.

Dependent `skip`, например tool continuation, не создаёт отдельного action. Он получает `blocked_by` на исходный finding.

### 10.7. Конфликт evidence

Если native probe проходит, а official-SDK probe падает:

- не выбирать победителя по умолчанию;
- создать `evidence_conflict`;
- сохранить оба evidence records;
- указать различия request settings и parser expectations;
- предложить targeted investigation;
- не выдавать provider fix, пока причина не локализована.

---

## 11. Recommendation pipeline

### 11.1. Источники рекомендаций

В порядке приоритета:

1. exact provider/runtime advice pack;
2. generic curated catalog entry;
3. deterministic heuristic diagnostic;
4. explicit `no_safe_automated_guidance`.

LLM-generated advice не используется в MVP.

### 11.2. Action kinds

Stable JSON values:

```text
fix
configure
workaround
document
investigate
rerun
manual_review
```

### 11.3. Priority

| Priority | Правило |
|---|---|
| `p0` | Core FAIL либо core INCONCLUSIVE, блокирующий authoritative verdict |
| `p1` | Optional FAIL, подтверждённый SDK interoperability defect, evidence conflict с высоким пользовательским impact |
| `p2` | Optional UNSUP, configuration constraint, optional WARN/ERROR, documentation gap |
| `p3` | Optional SKIP, manual check, низкоуверенное улучшение |

Priority означает порядок внимания, а не severity конкретного security incident.

### 11.4. Guidance level

| Level | Требование |
|---:|---|
| `3` | Известно что изменить, где изменить и как автоматически проверить |
| `2` | Точно известен target и desired behavior, но implementation зависит от архитектуры |
| `1` | Доступно только направление investigation/manual review |

### 11.5. Confidence

Отдельно для finding, attribution и recommendation:

```text
high
medium
low
```

Пример:

```text
Impact: P0
Finding confidence: high
Attribution confidence: high
Guidance level: 3
```

или:

```text
Impact: P1
Evidence state: conflicting
Attribution confidence: low
Guidance level: 1
```

### 11.6. Обязательные части рекомендации

Каждая P0/P1-рекомендация содержит:

1. `title` — imperative, короткий;
2. `why` — связь с профилем и реальным user impact;
3. `observed` — краткое evidence;
4. `target` — предполагаемый component owner;
5. `steps` — ordered actions;
6. `verification` — probe IDs или точная rerun procedure;
7. `potential_impact` — только условный;
8. `references` — первичный стандарт/официальная документация;
9. `confidence`;
10. `caveats` — если есть uncertainty.

Для P2/P3 допускается guidance level 1, но должна быть хотя бы проверяемая следующая операция.

### 11.7. Workaround не равен fix

Рекомендация может содержать:

```json
{
  "primary_action": "Implement developer-role handling with correct precedence",
  "workarounds": [
    "For clients that control prompts, use system role and document the semantic difference"
  ]
}
```

Workaround нельзя описывать как восстановление полного conformance.

### 11.8. Potential impact

Вместо сомнительного `+12.4 points` показывать изменение профильных мер:

```json
{
  "blocks_core": true,
  "core_requirements_recovered": 1,
  "optional_requirements_recovered": 0,
  "evidence_requirements_recovered": 0,
  "current_verdict": "fail",
  "projected_verdict": "partial",
  "assumptions": [
    "Only models.list.schema changes",
    "No other requirement regresses"
  ]
}
```

`projected_verdict` равен `null`, если:

- finding conflicting/inconclusive;
- одна рекомендация не гарантирует requirement PASS;
- исправление зависит от недоказанной root cause.

### 11.9. Ranking

Порядок recommendations определяется lexicographic tuple:

```text
priority
→ blocks_authoritative_verdict
→ projected_verdict_gain
→ affected canonical requirements
→ observed reach
→ guidance_level
→ confidence
→ stable recommendation id
```

Не вводить единый скрытый «recommendation score».

---

## 12. Status-to-guidance policy

| Requirement level | Result | Finding | Guidance |
|---|---|---|---|
| Core | `fail` | contract/behavior failure | `p0 fix` |
| Core | `warn` | evidence uncertain | `p0 investigate` |
| Core | `skip` | evidence missing | `p0 rerun/configure` |
| Core | `error` | execution inconclusive | `p0 investigate/rerun` |
| Optional | `fail` | behavior mismatch | `p1 fix` |
| Optional | `unsupported` | capability gap | `p2 enable-or-document` |
| Optional | `warn` | semantic uncertainty | `p2 investigate` |
| Optional | `error` | inconclusive | `p2 rerun/investigate` |
| Optional | `skip` | not executed | `p3 rerun/manual` |
| Any | `not_applicable` | none | no recommendation |
| Any | `pass` | none by default | hidden under Passed |

Required `unsupported` нормализуется в `fail` с `failure_kind=required_unsupported`, как определено scoring policy.

---

## 13. Advice catalog

### 13.1. Размещение

```text
guidance/
├── catalog/
│   ├── openai-models.json
│   ├── chat-completions.json
│   ├── responses.json
│   ├── images.json
│   ├── embeddings.json
│   └── runtime.json
├── packs/
│   ├── generic-openai-gateway.json
│   ├── dashscope.json
│   ├── vibecode.json
│   ├── vllm.json
│   ├── llama-cpp.json
│   └── litellm.json
└── locales/
    ├── en.json
    └── ru.json
```

MVP обязан включать generic catalog. Provider packs могут поставляться постепенно.

### 13.2. Почему catalog отделён от profile

Profile является нормативным контрактом и влияет на scoring. Guidance:

- меняется чаще;
- может улучшать wording;
- может добавлять implementation examples;
- имеет собственную версию;
- не должен заставлять пересматривать исторический conformance result.

Report фиксирует обе ревизии:

```json
{
  "profile": {
    "id": "openai-compat",
    "revision": "2026-09-01"
  },
  "guidance_engine": {
    "catalog_revision": "2026-09-01.1",
    "packs": [
      {"id": "generic-openai-gateway", "revision": "1"}
    ]
  }
}
```

### 13.3. Связь profile и catalog

Canonical requirement содержит:

```json
{
  "id": "models.list.schema",
  "level": "core",
  "documentation_url": "https://platform.openai.com/docs/api-reference/models/list",
  "finding_codes": [
    "contract.models.list.entry.created_missing",
    "contract.models.list.object_invalid",
    "contract.models.list.data_invalid"
  ]
}
```

Catalog содержит remediation entry для каждого `finding_code`.

### 13.4. Пример catalog entry

```json
{
  "id": "openai.models.created-missing",
  "revision": "1",
  "match": {
    "finding_code": "contract.models.list.entry.created_missing"
  },
  "title": "Add `created` to every model object",
  "default_priority": "p0",
  "action_kind": "fix",
  "guidance_level": 3,
  "audiences": ["gateway_maintainer"],
  "why": "The target OpenAI model schema requires a Unix creation timestamp.",
  "steps": [
    {
      "id": "populate-created",
      "text": "Populate `created` with a stable Unix timestamp in seconds for every item in `data`."
    },
    {
      "id": "avoid-request-time",
      "text": "Use model publication or gateway-registration time; do not generate a new value on every request."
    }
  ],
  "verification": [
    {
      "kind": "requirements",
      "ids": ["models.list.schema"]
    },
    {
      "kind": "external_suites",
      "ids": ["official-sdk.models"]
    }
  ],
  "references": [
    {
      "title": "OpenAI Models API",
      "url": "https://platform.openai.com/docs/api-reference/models"
    }
  ]
}
```

### 13.5. Pack constraints

Advice pack может:

- заменить generic implementation notes;
- добавить provider-specific file/config references;
- добавить workaround;
- повысить guidance level при наличии точных шагов;
- добавить официальный provider reference.

Advice pack не может:

- поменять probe/requirement status;
- поменять core/optional;
- изменить verdict или coverage;
- скрыть finding;
- снизить priority core blocker ниже P0;
- объявить root cause confirmed без нового evidence.

### 13.6. Detection

Pack selection sources:

1. explicit CLI override;
2. profile/provider configuration;
3. trusted structured metadata;
4. conservative host/header fingerprint;
5. generic pack.

Автодетекция всегда сохраняет:

```json
{
  "pack": "dashscope",
  "detection_source": "base_url_host",
  "confidence": "high"
}
```

---

## 14. JSON contract

### 14.1. Additive v1 shape

В `aiqa.report.v1` добавить top-level поле `guidance`. Существующие поля остаются неизменными на один deprecation cycle.

```json
{
  "schema_version": "aiqa.report.v1",
  "summary": {
    "status": "fail",
    "score": 77.1,
    "score_method": "legacy-pass-rate-v1",
    "score_deprecated": true,
    "assessment": {
      "verdict": "fail",
      "core": {},
      "optional": {},
      "evidence": {}
    }
  },
  "guidance": {
    "schema_version": "aiqa.guidance.v1",
    "status": "complete",
    "engine": {
      "version": "1.0.0",
      "catalog_revision": "2026-09-01.1",
      "packs": [
        {
          "id": "generic-openai-gateway",
          "revision": "1",
          "source": "builtin"
        }
      ]
    },
    "summary": {
      "eligible_findings": 5,
      "findings": 5,
      "recommendations": 5,
      "by_priority": {
        "p0": 1,
        "p1": 3,
        "p2": 1,
        "p3": 0
      },
      "guidance_coverage_percent": 100.0,
      "actionable_coverage_percent": 80.0
    },
    "findings": [],
    "recommendations": [],
    "artifacts": {}
  }
}
```

### 14.2. Guidance status

```text
complete
partial
disabled
error
```

Ошибка guidance engine:

- записывается в `guidance.status=error`;
- не меняет conformance verdict;
- не меняет CLI exit code, если не задан отдельный future policy flag;
- отображается как проблема самого AIQA.

### 14.3. Finding schema

```json
{
  "id": "finding:models-created-missing",
  "fingerprint": "sha256:...",
  "code": "contract.models.list.entry.created_missing",
  "class": "contract_violation",
  "evidence_state": "confirmed",
  "title": "Model entries omit the `created` field",
  "subject": {
    "kind": "provider",
    "id": "https://vibecode.moe/v1"
  },
  "observed_scope": {
    "endpoint": "GET /v1/models",
    "affected": 30,
    "total": 30
  },
  "requirement_ids": ["models.list.schema"],
  "probe_refs": [
    "aiqa.native.models.list.schema",
    "official-sdk.models"
  ],
  "expected": "Every model entry includes a Unix creation timestamp.",
  "observed": "`created` was absent in 30 of 30 entries.",
  "attribution": {
    "component": "provider_gateway",
    "confidence": "high",
    "basis": "The inventory endpoint emits these objects."
  },
  "details": {
    "json_pointer": "/data/*/created",
    "missing_entries": 30,
    "total_entries": 30
  },
  "artifact_refs": [],
  "blocked_requirement_ids": [],
  "baseline_state": "unknown"
}
```

### 14.4. Recommendation schema

```json
{
  "id": "recommendation:models-created-missing",
  "fingerprint": "sha256:...",
  "finding_ids": ["finding:models-created-missing"],
  "title": "Add `created` to every model object",
  "priority": "p0",
  "action_kind": "fix",
  "guidance_level": 3,
  "confidence": "high",
  "audiences": ["gateway_maintainer"],
  "target": {
    "component": "provider_gateway",
    "endpoint": "GET /v1/models"
  },
  "why": "This required schema mismatch blocks the target compatibility profile.",
  "steps": [
    {
      "id": "add-created",
      "text": "Return a stable Unix timestamp in seconds in `created` for every model."
    }
  ],
  "verification": [
    {
      "kind": "requirement",
      "ids": ["models.list.schema"]
    },
    {
      "kind": "command",
      "shell": "aiqa test=compat --model \"$MODEL\" --format=markdown,json --name=verify"
    }
  ],
  "workarounds": [],
  "caveats": [],
  "potential_impact": {
    "blocks_core": true,
    "core_requirements_recovered": 1,
    "current_verdict": "fail",
    "projected_verdict": "partial",
    "assumptions": [
      "No other requirement changes"
    ]
  },
  "references": [
    {
      "title": "OpenAI Models API",
      "url": "https://platform.openai.com/docs/api-reference/models"
    }
  ],
  "source": {
    "kind": "catalog",
    "id": "openai.models.created-missing",
    "revision": "1"
  }
}
```

### 14.5. Shared artifacts

Большой external output хранится один раз:

```json
{
  "artifacts": {
    "sha256:abcd": {
      "kind": "external_checker_log",
      "media_type": "text/plain",
      "bytes": 4187,
      "sha256": "abcd...",
      "content": "...redacted...",
      "truncated": false
    }
  }
}
```

Evidence и findings используют `artifact_ref`. Markdown рендерит один `<details>` на уникальный artifact.

---

## 15. Markdown UX

### 15.1. Порядок разделов

```text
1. Run metadata
2. Executive summary
3. Fix first
4. Improve compatibility
5. Investigate / rerun
6. Capability gaps
7. Model/module details
8. Passed checks
9. Raw evidence
10. Methodology and limitations
```

### 15.2. Executive summary

```markdown
## Summary

| Overall | Core conformance | Optional coverage | Evidence | Open actions |
|---|---:|---:|---:|---:|
| FAIL | 7/8 (87.5%) | 20/27 (74.1%) | 35/35 (100.0%) | 1 P0 · 3 P1 · 1 P2 |

**Fix first:** Add `created` to every object returned by `GET /v1/models`.

Projected after this fix: **FAIL → PARTIAL**, assuming no regressions.
```

Не показывать legacy score рядом с Overall. Его допускается оставить в collapsed migration block.

### 15.3. Action table

```markdown
## Fix first

| Priority | Recommendation | Scope | Expected impact | Confidence |
|---:|---|---|---|---:|
| P0 | Add `created` to every model object | Provider · `GET /v1/models` · 30/30 entries | Recover 1 core requirement; projected PARTIAL | High |
| P1 | Return a client error for invalid field types | Gateway request validation | Remove optional contract failure | High |
| P1 | Implement `logprobs` or reject it explicitly | `gpt-5.4-mini` | Remove silent feature mismatch | High |
| P1 | Resolve native/SDK streaming evidence conflict | Stream adapter or checker | Establish SDK interoperability | Low |
```

### 15.4. Recommendation detail

````markdown
### P0 · Add `created` to every model object

**Why this matters:** `models.list.schema` is required and currently blocks the
target profile.

**Observed:** `created` was missing in 30/30 `data[]` entries.

**Likely owner:** provider gateway  
**Attribution confidence:** high  
**Guidance level:** 3/3

#### Change

1. Add a stable Unix timestamp in seconds to every model object.
2. Use model publication or gateway-registration time.
3. Do not generate a new timestamp for each response.

#### Verify

```bash
export OPENAI_BASE_URL="https://vibecode.moe/v1"
export OPENAI_API_KEY="$VIBECODE_API_KEY"

aiqa test=compat \
  --model=gpt-5.4-mini \
  --format=markdown,json \
  --name=verify-models
```

Expected:

```text
models.list.schema       PASS
official-sdk.models      PASS
Core conformance         8/8
```

**Potential impact:** Overall may move from `FAIL` to `PARTIAL`. This projection
assumes all other findings remain unchanged.

**Evidence:** `models.list.schema`, `official-sdk.models`  
**Reference:** OpenAI Models API
````

### 15.5. Diagnostics

Evidence conflicts and runtime errors идут отдельно:

```markdown
## Investigate

### P1 · Resolve conflicting streaming evidence

Native SSE validation passed, while the official SDK checker reported
`stream chunk missing choices`.

No provider fix is recommended yet because the observations conflict.

Next steps:

1. Compare the exact request bodies used by both probes.
2. Check whether `stream_options.include_usage=true` produced a final usage
   chunk with an empty `choices` array.
3. Replay the captured stream through the official SDK parser.
4. Fix either the stream adapter or the checker after localizing the mismatch.
```

### 15.6. Passed/N/A/Skipped

- Passed checks collapsed by default.
- `N/A` grouped separately and never gets remediation.
- `SKIP` appears under «Run or review» rather than «Broken».
- `ERROR` appears under «Investigate» and never under «Unsupported».

---

## 16. Concrete recommendations for current fixtures

### 16.1. Vibecode: missing `created`

**Finding**

```text
contract.models.list.entry.created_missing
```

**Recommendation**

- Priority: P0
- Action: fix
- Target: provider inventory endpoint
- Guidance: 3
- Confidence: high
- Deduplication: one finding for 30 entries plus official-SDK corroboration
- Projected effect: recover one core requirement; possible `FAIL → PARTIAL`

### 16.2. Vibecode: invalid types produce `502`

**Finding**

```text
contract.chat.invalid_request.upstream_5xx
```

**Recommendation**

1. Validate request field types at the OpenAI-compatible gateway boundary.
2. Return `400` or `422` with an OpenAI-shaped error.
3. Do not forward structurally invalid requests to an upstream model.
4. Rerun invalid-types probes.

- Priority: P1 because the current requirement is optional; profile owners may later promote it.
- Target: provider gateway.
- Confidence: high.

### 16.3. Vibecode: `logprobs` silently accepted

**Finding**

```text
behavior.chat.logprobs.accepted_but_missing
```

**Recommendation**

Preferred:

- implement `choices[].logprobs` according to the accepted request.

Acceptable capability-gap behavior for an optional feature:

- reject unsupported `logprobs` with a suitable 4xx OpenAI-shaped error.

Do not accept the parameter and silently omit the promised result.

- Priority: P1.
- Target: model adapter/gateway, attribution medium until upstream behavior is isolated.
- Guidance: 2.

### 16.4. DashScope/Qwen: tool choice restricted in thinking mode

Three probes expose one normalized finding:

```text
configuration.chat.tools.tool_choice_conflicts_with_thinking
```

Recommendation:

1. Retest `required`, forced object and streamed tool call with thinking disabled.
2. If the backend supports them in that mode, expose/document a compatible model mode.
3. If the gateway owns parameter translation, preserve the explicit client choice or return a documented 4xx.
4. Publish the limitation in a model capability matrix.
5. Rerun all affected requirements.

This is not automatically a general gateway defect; it may be a model-mode constraint.

### 16.5. DashScope/Qwen: developer role

**Finding**

```text
unsupported.chat.role.developer
```

Recommendation:

- support the role while preserving its precedence semantics;
- where the upstream lacks it, implement an adapter strategy and document semantic loss;
- otherwise explicitly publish the capability gap and client workaround.

Do not present unconditional `developer → system` conversion as full conformance.

### 16.6. Native vs official SDK streaming conflict

**Finding**

```text
evidence.chat.stream.native_sdk_conflict
```

Recommendation type: `investigate`, not `fix`.

The external checker may be rejecting a valid final usage chunk, or the native parser may be too permissive. AIQA must first compare:

- exact request bodies;
- sequence of chunks;
- missing versus empty `choices`;
- location of `usage`;
- chunks after terminal `finish_reason`;
- SDK parser version.

Until localized, attribution remains `unknown` or `external_checker/model_adapter` with low confidence.

---

## 17. CLI behavior

### 17.1. Default

Recommendations are deterministic and generated from already collected evidence, so normal use remains unchanged:

```bash
aiqa test=compat \
  --timeout=120 \
  --format=markdown,json \
  --name=report
```

`report.md` and `report.json` include guidance by default.

### 17.2. Controls

Proposed flags:

```text
--guidance=full|summary|off
--guidance-pack=auto|generic|<id>
--guidance-locale=en|ru
--baseline=<previous-report.json>
```

Defaults:

```text
--guidance=full
--guidance-pack=auto
--guidance-locale=en
```

`--guidance=off` does not alter probes or scoring.

### 17.3. Offline rendering

Phase 2:

```bash
aiqa explain report.json \
  --guidance-pack=auto \
  --format=markdown,html \
  --name=report-explained
```

Это позволяет улучшить catalog и перегенерировать human report без повторного расхода API tokens.

Report должен фиксировать, что guidance мог быть рассчитан позже другой версией engine/catalog.

### 17.4. Targeted verification

Phase 2:

```bash
aiqa verify report.json \
  --recommendation=recommendation:models-created-missing \
  --format=markdown,json \
  --name=verify
```

Recommendation хранит необходимые requirement/probe IDs. CLI запускает только их и prerequisites.

До появления команды `verify` report использует обычный полный rerun command.

---

## 18. Архитектурные изменения

### 18.1. Новые packages

```text
internal/finding/
    normalize.go
    aggregate.go
    fingerprint.go
    attribution.go

internal/guidance/
    engine.go
    catalog.go
    matcher.go
    rank.go
    dedupe.go
    impact.go
    packs.go
    template.go

internal/report/
    markdown_guidance.go
    html.go               # phase 2
    sarif.go              # phase 3
```

### 18.2. Изменения профиля

Перейти от flat probes к canonical requirements:

```text
profile
  └── modules
       └── requirements
            └── probes
```

Каждый probe обязан ссылаться на `requirement_id` и перечислять допустимые normalized finding codes.

### 18.3. Изменения result types

Добавить:

```go
type EvidenceSignal struct { ... }
type Finding struct { ... }
type Recommendation struct { ... }
type GuidanceReport struct { ... }
type ArtifactRef struct { ... }
type Attribution struct { ... }
type PotentialImpact struct { ... }
```

### 18.4. External checker adapters

Текущий human log не должен быть источником precise remediation.

Приоритет:

1. structured JSON output внешнего checker;
2. typed adapter по suite/result fields;
3. generic external-tool diagnostic;
4. raw text только как redacted artifact.

Нельзя строить provider-specific fix только из regex над английским error summary.

### 18.5. Renderer

Renderer:

- сортирует только по уже рассчитанному rank tuple;
- не меняет priority;
- не вычисляет root cause;
- не создаёт команды;
- не повторяет artifacts;
- скрывает passed evidence в collapsed sections;
- сохраняет anchors по stable IDs.

### 18.6. Profile и catalog lint

CI должен проверять:

- unique requirement/probe/finding/recommendation IDs;
- каждый probe ссылается на существующий requirement;
- каждый emitted finding code зарегистрирован;
- каждый P0/P1-eligible finding имеет generic catalog entry или explicit `no_safe_automated_guidance`;
- guidance level 3 содержит verification;
- ссылки имеют HTTPS, кроме явно разрешённых localhost examples;
- template placeholders объявлены;
- pack не меняет forbidden fields;
- locale keys полны;
- snapshot reports детерминированы.

---

## 19. Безопасность и приватность

### 19.1. Redaction

Не включать:

- API key;
- Authorization headers;
- cookies;
- signed URL query parameters;
- полный base64;
- полный аудиo/изображение;
- потенциально секретные tool arguments;
- неограниченный response body;
- внутренние stack traces provider без opt-in.

### 19.2. Reproduction commands

Использовать только placeholders:

```bash
-H "Authorization: Bearer $OPENAI_API_KEY"
```

Не вставлять literal secret даже из environment.

### 19.3. External links

- primary standard or official provider docs preferred;
- pack links versioned where possible;
- report records link source;
- renderer marks third-party/community guidance.

### 19.4. SSRF и artifacts

При будущих image/audio suites URL artifacts должны загружаться только через безопасный fetch policy. Guidance layer не получает право выполнять URLs.

### 19.5. Optional LLM explainer

Future/opt-in only:

- получает sanitized structured findings, не raw report;
- может переводить или упрощать текст;
- не меняет IDs, facts, status, priority, impact и commands;
- output маркируется `source.kind=generated`;
- curated catalog remains authoritative.

---

## 20. Versioning и миграция

### Phase A — additive `aiqa.report.v1`

Добавить:

```json
{
  "guidance": {},
  "summary": {
    "score_method": "legacy-pass-rate-v1",
    "score_deprecated": true,
    "assessment": {}
  }
}
```

Текущая JSON Schema допускает дополнительные поля; старые consumers продолжают читать существующий subset.

Markdown:

- убрать `Score` из главной таблицы;
- добавить `Core`, `Optional`, `Evidence`, `Open actions`;
- `N/S` переименовать визуально в `UNSUP`;
- добавить Recommendations и Diagnostics;
- legacy score спрятать в migration details.

### Phase B — один deprecation cycle

Поддерживать:

```text
--report-schema=v1
--report-schema=v2
```

Публиковать fixtures обеих версий.

### Phase C — `aiqa.report.v2`

В одном breaking migration объединить:

- canonical requirements;
- modular profiles/applicability;
- `not_applicable`;
- authoritative `verdict`;
- core/optional/evidence measures;
- findings;
- guidance;
- artifact references;
- subject kinds;
- external evidence normalization.

Не делать отдельный breaking schema bump сначала для scoring, а затем ещё один для guidance.

### Baseline migration

Future reports могут использовать:

```text
new
unchanged
updated
resolved
regressed
unknown
```

Fingerprint делает возможным diff без сравнения human strings.

---

## 21. Functional requirements

| ID | Requirement |
|---|---|
| AR-001 | JSON является единственным источником данных для Markdown/HTML |
| AR-002 | Advice строится на canonical requirements, не на raw probe count |
| AR-003 | Probes emit typed finding codes |
| AR-004 | Human summaries не используются как rule input |
| AR-005 | Findings дедуплицируются по stable fingerprint/root cause |
| AR-006 | Каждая P0/P1 recommendation содержит evidence, steps и verification |
| AR-007 | Guidance не меняет status, verdict или scoring |
| AR-008 | `not_applicable` не создаёт finding/recommendation |
| AR-009 | `error` создаёт investigation/rerun, не unsupported/fix |
| AR-010 | Evidence conflict создаёт diagnostic, не автоматический blame |
| AR-011 | Target component и confidence всегда явны |
| AR-012 | Potential impact выражается через verdict/core/optional/evidence |
| AR-013 | Legacy score не используется для recommendation ranking |
| AR-014 | External artifacts хранятся и рендерятся один раз |
| AR-015 | Advice catalog и packs версионируются |
| AR-016 | Packs не могут менять normative fields |
| AR-017 | Recommendations сортируются детерминированным tuple |
| AR-018 | Passed checks collapsed by default |
| AR-019 | SKIP, ERROR, UNSUP и N/A визуально разделены |
| AR-020 | Report содержит methodology/limitations |
| AR-021 | Recommendation IDs и fingerprints стабильны между runs |
| AR-022 | Guidance engine failure не меняет conformance exit code |
| AR-023 | Все snippets/commands проходят redaction |
| AR-024 | Report показывает observed scope отдельно от attribution |
| AR-025 | Model-level finding не повышается до gateway defect без evidence |
| AR-026 | Blocked dependent checks ссылаются на parent finding |
| AR-027 | Guidance coverage считается по canonical requirements |
| AR-028 | Advice generation не требует сетевого или LLM-вызова в MVP |
| AR-029 | Report records exact catalog/pack versions |
| AR-030 | Каждая reference имеет title, URL и source kind |

---

## 22. Non-functional requirements

### Determinism

Без учёта `generated_at` и runtime timings один и тот же input должен создавать byte-equivalent normalized guidance JSON.

### Performance

- Guidance generation: менее 100 ms для 1,000 requirement assessments на обычной developer machine.
- Не выполняет API calls.
- Не увеличивает token usage.

### Size

- Deduplicated external artifacts.
- Default Markdown не более чем в 2 раза больше текущего отчёта при одинаковом числе уникальных findings.
- Raw details collapsed.

### Compatibility

- Additive v1 fields.
- Stable lowercase JSON enums.
- Unknown future fields ignored by consumers.
- Breaking enum changes только с schema bump.

### Auditability

Для каждой recommendation возможно проследить:

```text
recommendation
→ catalog entry/pack revision
→ finding
→ requirement
→ probe/evidence
→ raw sanitized observation
```

---

## 23. Acceptance criteria

### 23.1. Общие

1. Каждый applicable core/optional non-pass canonical requirement имеет finding.
2. Каждый eligible finding имеет:
   - recommendation; либо
   - explicit diagnostic `no_safe_automated_guidance`.
3. Каждый P0/P1 содержит verification.
4. Ни одна recommendation не изменяет report summary.
5. Одинаковый report/catalog даёт одинаковые IDs и порядок.
6. N/A не участвует в guidance coverage.
7. ERROR не превращается в capability gap.
8. UNSUP не описывается как infrastructure failure.
9. Provider pack не может изменить normative outcome.
10. Secrets отсутствуют во всех форматах.

### 23.2. Fixture: Vibecode

Из текущего fixture должны получиться:

- одна P0 recommendation для missing `created`, а не 30;
- external `models` failure служит evidence того же finding;
- одна P1 для invalid-types `502`;
- одна P1 для accepted-but-missing logprobs;
- один conflict diagnostic для native/SDK streaming mismatch;
- `models_get` unsupported остаётся P2 capability gap;
- projected verdict после `created` fix — `PARTIAL`, не `PASS`;
- raw external output отображается один раз.

### 23.3. Fixture: DashScope/Qwen

Должны получиться:

- одна grouped recommendation для трех tool-choice failures;
- tool continuation SKIP помечен как blocked, без отдельного fix;
- developer role — отдельный P2 capability recommendation;
- native/SDK stream disagreement — diagnostic;
- 8/8 core remains visible and не смешивается с optional gaps;
- рекомендации не называют все ограничения gateway defects.

### 23.4. Fixture: infrastructure error

При timeout/429/5xx:

- finding class `execution_failure`;
- evidence state `inconclusive`;
- action `rerun` или `investigate`;
- projected verdict `null`;
- никакого совета «реализуйте capability».

### 23.5. Fixture: non-applicable model

Для image-only модели в chat module:

- `not_applicable`;
- нет finding;
- нет recommendation;
- нет влияния на guidance coverage или scoring.

---

## 24. Test strategy

### Unit tests

- signal → finding mapping;
- fingerprint stability;
- dedup;
- priority mapping;
- impact projection;
- catalog matching;
- pack overlay restrictions;
- template escaping;
- redaction;
- deterministic sorting.

### Property tests

```text
PASS/N/A never produces fix recommendation
core FAIL always yields P0
optional UNSUP never yields P0/FAIL attribution by itself
ERROR never yields unsupported
duplicate evidence never changes requirement count
pack never changes normative result
same normalized finding always has same fingerprint
```

### Golden tests

- Vibecode report;
- DashScope/Qwen report;
- mock full-pass endpoint;
- rate-limited endpoint;
- conflicting native/SDK endpoint;
- mixed chat/image/embedding inventory.

### Schema tests

- Draft 2020-12 validation;
- old v1 fixture remains readable;
- new v1 additive fixture accepted;
- v2 fixtures reject missing canonical IDs.

### Usability test

Дать report без объяснения пяти участникам ролей gateway maintainer/QA:

- не менее 80% правильно называют first action за 60 секунд;
- не менее 80% находят verification;
- ни один участник не интерпретирует ERROR как unsupported;
- ни один участник не ранжирует FAIL выше PARTIAL по legacy score.

---

## 25. Success metrics

### Product metrics

- `P0/P1 guidance coverage = 100%`;
- `all eligible guidance coverage ≥ 95%`;
- duplicate recommendation rate `< 5%`;
- recommendations with automated verification `≥ 80%` для P0/P1;
- false high-confidence attribution: `0` в maintained fixture corpus;
- raw external log duplication: `0`;
- deterministic golden diff: `0` без catalog/profile changes.

### User metrics

- median time to first action `< 30s`;
- median time to locate evidence `< 60s`;
- ≥80% maintainers оценивают top recommendation как «actionable without additional report interpretation».

### Engineering metrics

- zero extra provider requests for guidance;
- guidance generation `<100ms / 1,000 requirements`;
- no secret leakage regressions;
- additive v1 does not break existing fixture consumers.

---

## 26. Delivery plan

### Milestone 0 — scoring and requirement foundation

Prerequisites:

- implement `assessment` from `docs/scoring.md`;
- canonical requirement IDs;
- external probes as evidence, not extra votes;
- `not_applicable`;
- subject kinds.

### Milestone 1 — Actionable Markdown/JSON MVP

Scope:

- normalized finding signals;
- generic catalog;
- P0–P3;
- guidance level/confidence;
- evidence linkage;
- deterministic ranking;
- deduplication;
- potential impact;
- Markdown Recommendations/Diagnostics;
- external artifact dedup;
- Vibecode and Qwen golden fixtures.

Exit criterion: acceptance criteria 23.1–23.3 pass.

### Milestone 2 — Packs, baseline and targeted verify

Scope:

- advice packs;
- explicit pack override/detection;
- `aiqa explain`;
- `aiqa verify`;
- baseline fingerprints and new/resolved/regressed;
- Russian locale;
- capability/module-specific guidance.

### Milestone 3 — HTML and CI integrations

Scope:

- self-contained HTML report;
- filters by priority/model/module/status;
- copy-safe reproduction commands;
- SARIF export for issue/code-scanning ecosystems where mapping is meaningful;
- report comparison view.

### Milestone 4 — Optional generated explainer

Only after curated guidance is mature:

- opt-in;
- sanitized findings only;
- no normative influence;
- generated text visibly marked;
- deterministic curated fallback always present.

---

## 27. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Advice confidently blames wrong component | observed scope and attribution stored separately; confidence mandatory |
| Recommendations become stale | versioned catalog/packs; references; fixtures; catalog lint |
| Report becomes enormous | root-cause dedup, shared artifacts, collapsed passed/raw sections |
| New advice changes historical score | hard architectural separation |
| External tool text changes and breaks rules | typed adapter/JSON; no semantic regex over free text |
| Provider-specific workaround is mistaken for conformance | mark workaround separately; projected impact excludes it unless requirement passes |
| LLM fabricates commands | no LLM in MVP; future generated layer cannot alter facts/actions |
| One failure creates dozens of actions across models | group by finding fingerprint and observed reach |
| Generic advice is too vague | guidance level; pack mechanism; explicit no-safe-guidance |
| Fix verification costs money | show cost class; respect billable suite opt-in; use targeted probes |
| Catalog wording update makes reports non-reproducible | record catalog and pack revisions |
| Guidance engine itself fails | `guidance.status=error`; conformance result remains intact |

---

## 28. Open decisions

These decisions do not block Milestone 1, but must be fixed before `aiqa.report.v2`:

1. Top-level JSON name: `guidance` versus `remediation`. This PRD recommends `guidance`.
2. Whether generic catalog ships as JSON, YAML or Go-embedded data. Recommendation: JSON + embedded FS.
3. Whether catalog revision follows date (`2026-09-01.1`) or SemVer. Recommendation: date revision for content, SemVer for engine.
4. Whether Markdown locale defaults to English regardless of OS locale. Recommendation: English for reproducibility; explicit `--guidance-locale`.
5. Whether external SDK conflicts are P1 or P2 by default. Recommendation: P1 only when the profile explicitly promises official-SDK interoperability.
6. Whether guidance coverage appears in executive summary. Recommendation: show action counts; put AIQA guidance coverage under Methodology to avoid confusing it with provider quality.
7. Whether potential impact may project `PASS`. Recommendation: only when every remaining applicable requirement is already PASS and evidence is conclusive.

---

## 29. Definition of Done

Feature считается готовой, когда:

- AIQA report показывает authoritative verdict и три профильные меры из `docs/scoring.md`;
- top actions видны до raw test tables;
- real fixtures дают дедуплицированные, точные и проверяемые рекомендации;
- missing `created`, invalid `502`, silent logprobs, thinking/tool conflict и stream evidence conflict получают разные корректные action types;
- advice не влияет на scoring;
- JSON содержит полный structured guidance contract;
- Markdown полностью рендерится из JSON;
- все P0/P1 связаны с requirements и evidence;
- каждый P0/P1 имеет verification;
- raw logs не повторяются;
- old JSON consumers продолжают работать в v1;
- `go test`, race tests, schema tests и golden tests проходят;
- documentation explains statuses, priorities, confidence, guidance level and limitations.

---

## 30. Источники

### AIQA

- [AIQA scoring and verdict recommendation](https://github.com/ilyar/aiqa/blob/main/docs/scoring.md)
- [AIQA architecture](https://github.com/ilyar/aiqa/blob/main/docs/architecture.md)
- [AIQA adding tests](https://github.com/ilyar/aiqa/blob/main/docs/adding-tests.md)

### Lighthouse

- [Understanding Lighthouse results](https://github.com/GoogleChrome/lighthouse/blob/main/docs/understanding-results.md)
- [Lighthouse audit result type](https://github.com/GoogleChrome/lighthouse/blob/main/types/lhr/audit-result.d.ts)
- [Lighthouse audit metadata and product](https://github.com/GoogleChrome/lighthouse/blob/main/types/audit.d.ts)
- [Lighthouse structured audit details](https://github.com/GoogleChrome/lighthouse/blob/main/types/lhr/audit-details.d.ts)
- [Lighthouse category renderer](https://github.com/GoogleChrome/lighthouse/blob/main/report/renderer/category-renderer.js)
- [Lighthouse performance category prioritization](https://github.com/GoogleChrome/lighthouse/blob/main/report/renderer/performance-category-renderer.js)
- [Lighthouse Stack Packs](https://github.com/GoogleChrome/lighthouse-stack-packs)

### API contract and reporting conventions

- [OpenAI OpenAPI specification](https://github.com/openai/openai-openapi)
- [OpenAI Models API](https://platform.openai.com/docs/api-reference/models)
- [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
