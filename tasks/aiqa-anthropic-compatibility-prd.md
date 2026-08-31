# PRD: профиль совместимости Anthropic Messages API и многопротокольный запуск AIQA

**Статус:** Proposed  
**Версия документа:** 1.0  
**Дата:** 2026-09-01  
**Целевой репозиторий:** [`ilyar/aiqa`](https://github.com/ilyar/aiqa)  
**Проанализированный commit:** [`39dd330d235174c0500986b3628e8ebe85d4e409`](https://github.com/ilyar/aiqa/commit/39dd330d235174c0500986b3628e8ebe85d4e409)  
**Предлагаемый профиль:** `profile/anthropic-messages-compat-2026-09.json`  
**Предлагаемый контракт отчёта:** `aiqa.report.v2`  
**Версия Anthropic API:** `anthropic-version: 2023-06-01`  
**Версия официального Go SDK:** [`anthropics/anthropic-sdk-go v1.68.0`](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)

---

## 1. Краткое решение

AIQA должна поддерживать Anthropic Messages API как самостоятельную цель проверки наряду с уже существующей целью OpenAI.

Команда:

```bash
OPENAI_BASE_URL="https://api.openai.com/v1" \
OPENAI_API_KEY="$OPENAI_API_KEY_QA" \
ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic" \
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY_QA" \
aiqa --verbose test=compat \
  --format=markdown,json \
  --name=compat-report
```

должна:

1. обнаружить две активные цели — `openai` и `anthropic`;
2. выбрать для каждой собственный профиль;
3. получить независимый каталог моделей каждой цели;
4. запустить только применимые к модели модули;
5. собрать native evidence и evidence официального SDK;
6. отдельно вычислить verdict, core conformance, optional coverage и evidence coverage;
7. отдельно сформировать рекомендации;
8. записать:

```text
compat-report.md
compat-report.json
```

Для указанного `ANTHROPIC_BASE_URL` AIQA должна обращаться по адресам:

```text
GET  http://127.0.0.1:8080/anthropic/v1/models
GET  http://127.0.0.1:8080/anthropic/v1/models/{model_id}
POST http://127.0.0.1:8080/anthropic/v1/messages
POST http://127.0.0.1:8080/anthropic/v1/messages/count_tokens
```

Anthropic нельзя реализовывать как ещё один OpenAI module. Это отдельный протокол с другой семантикой:

- `ANTHROPIC_BASE_URL` указывает на префикс **до** `/v1`;
- используется `x-api-key`, а не OpenAI Bearer;
- обязателен заголовок `anthropic-version`;
- ответ содержит `content[]`, а не `choices[]`;
- системная инструкция передаётся top-level полем `system`;
- tools представлены блоками `tool_use` и `tool_result`;
- streaming — это последовательность именованных SSE events без `[DONE]`;
- usage использует `input_tokens` и `output_tokens`;
- ошибки имеют Anthropic envelope;
- совместимость проверяется официальным Anthropic SDK, а не OpenAI SDK.

Корневой отчёт может показывать общий run verdict, но не должен усреднять проценты OpenAI и Anthropic в один «общий score». У каждой цели остаётся свой авторитетный результат.

---

## 2. Проблема

Текущая реализация AIQA уже содержит хорошие основания:

- модульный OpenAI profile;
- capability classification и routing;
- native модули Chat Completions и image generation;
- canonical requirements поверх raw probes;
- раздельные conformance и guidance;
- внешний checker через официальный OpenAI Go SDK;
- deterministic findings и recommendations;
- status `not_applicable`;
- `FAIL`, `INCONCLUSIVE`, `PARTIAL`, `PASS-WITH-GAPS`, `PASS`;
- дедупликацию внешних артефактов.

Но все эти механизмы пока построены вокруг одной OpenAI-цели.

В коде присутствуют сквозные single-target assumptions:

```text
Config.BaseURL
Config.APIKey
один ProfileInfo в корне Report
один Discovery в корне Report
один Models[]
один OpenAI-oriented HTTP client
один capability namespace
один external checker
один provider subject из Config.BaseURL
одна OpenAI-shaped структура Usage
```

Простое добавление метода `Messages()` в текущий `internal/api/client.go` создаст ложную абстракцию:

1. пути будут собираться по OpenAI-правилам;
2. Anthropic key может утечь в OpenAI target или наоборот;
3. OpenAI SSE parser будет ждать `[DONE]`;
4. валидатор станет искать `choices[]`;
5. OpenAI usage исказит Anthropic usage;
6. одинаковый model ID в двух каталогах сольётся в один subject;
7. текущий `aiqa.report.v1` не сможет указать, к какому профилю относится finding;
8. OpenAI guidance начнёт предлагать неверные исправления Anthropic facade;
9. внешний `openai-compatibility-tester` будет принят за доказательство Anthropic compatibility.

Задача PRD — снять эти assumptions системно, а не засыпать их `if protocol == "anthropic"` по всей кодовой базе.

---

## 3. Что уже есть в AIQA и должно быть сохранено

### 3.1 Canonical requirements вместо подсчёта probes

Несколько probes могут подтверждать одно requirement, но не должны голосовать за него несколько раз.

Для Anthropic сохраняется цепочка:

```text
profile requirement
    ├── native HTTP/schema/semantic probe
    └── official Anthropic SDK probe
          ↓
canonical requirement assessment
          ↓
finding
          ↓
recommendation
```

### 3.2 Applicability отдельно от результата

Сохраняются:

```text
applicable
not_applicable
unknown

executed
skipped
```

и результаты:

```text
pass
fail
warn
unsupported
skip
error
not_applicable
```

Например:

- text Messages применимы к обычной Anthropic text model;
- vision module неприменим к явно text-only model;
- thinking module может быть применим, но пропущен из-за high-token policy;
- 400 на применимую optional capability — `unsupported`;
- 529 или timeout — `error`;
- непроверенная capability — не `pass`.

### 3.3 Guidance не влияет на conformance

Recommendations строятся только после фиксации canonical assessments. Они не могут менять:

```text
status
required/optional
verdict
coverage
exit code
```

### 3.4 Стандартная библиотека в основном binary

Native AIQA остаётся dependency-light. Официальные SDK подключаются внешними checker-процессами.

### 3.5 Существующий dual-facade сценарий

В `script/model-provider.sh` уже разворачивается Bifrost, который отдаёт:

```text
/openai/v1/chat/completions
/anthropic/v1/messages
```

и использует один virtual key одновременно как OpenAI Bearer и Anthropic `x-api-key`.

Следовательно, предложенный dual-target запуск соответствует реальной инфраструктуре проекта.

---

## 4. Цели

### G1. Native Anthropic conformance

Реализовать native probes для стабильной поверхности:

```text
GET  /v1/models
GET  /v1/models/{model_id}
POST /v1/messages
POST /v1/messages/count_tokens
```

### G2. Автоматическое обнаружение целей

OpenAI запускается при наличии `OPENAI_BASE_URL`, Anthropic — при наличии `ANTHROPIC_BASE_URL`. Обе цели могут работать в одном запуске.

### G3. Official SDK interoperability

Проверять Anthropic endpoint официальным Go SDK отдельным checker-процессом.

### G4. Правильный Anthropic streaming

Поддержать именованные SSE events, text deltas, tool JSON deltas, pings, unknown events, usage и mid-stream errors.

### G5. Независимые оценки целей

OpenAI и Anthropic получают разные:

```text
profile
inventory
modules
requirements
verdict
coverage
findings
recommendations
usage
telemetry
```

### G6. Понятные рекомендации

Каждый failure или gap должен быть связан с:

- стабильным signal code;
- evidence;
- probable owner;
- scope;
- конкретным исправлением;
- способом повторной проверки.

### G7. Backward compatibility

OpenAI-only вызовы должны сохраняться в течение migration cycle.

### G8. Воспроизводимость

Отчёт фиксирует Anthropic API version, профиль, SDK version/commit и набор модулей.

---

## 5. Не входит в первую реализацию

MVP не должен:

- заявлять официальную Anthropic certification;
- тестировать качество ответов или intelligence;
- тестировать legacy Text Completions;
- включать Message Batches, Files, Skills, Admin, Compliance и Managed Agents;
- автоматически тестировать server tools;
- включать beta headers в стабильный core;
- тестировать Bedrock/Vertex/Foundry-native APIs;
- брать Anthropic models из OpenAI catalog;
- сравнивать текст ответа между двумя facade;
- сохранять полные prompts, tool results, thinking, signatures, signed URLs или media;
- запускать дорогие thinking/PDF/image probes без разрешения;
- автоматически менять конфигурацию gateway;
- добавлять Anthropic `model-param` в первый PR;
- растягивать `aiqa.report.v1` до многопротокольного формата.

Профиль специально называется `anthropic-messages-compat`, а не просто `anthropic-compat`: он не притворяется тестом всей Anthropic Platform API.

---

## 6. Термины

### Target — цель проверки

Один endpoint одного протокола, проверяемый одним профилем.

```text
target_id: openai
protocol: openai
base_url: https://api.openai.com/v1

target_id: anthropic
protocol: anthropic
base_url: http://127.0.0.1:8080/anthropic
```

### Profile

Версионированный нормативный manifest:

- protocol;
- API version;
- scope;
- modules;
- requirement IDs;
- core/optional;
- applicability;
- source provenance.

### Probe

Исполняемый эксперимент, создающий evidence.

### Requirement

Каноническая единица контракта. Несколько probes могут подтверждать одно requirement.

### Capability

Model-level возможность, используемая для routing:

```text
anthropic.messages.text
anthropic.messages.image_input
anthropic.messages.tools
anthropic.messages.structured_output
anthropic.messages.thinking.adaptive
anthropic.messages.thinking.manual
```

### Target verdict

Авторитетный verdict одной цели.

### Run verdict

Сводка по всем активным целям. Она не отменяет target verdict.

---

## 7. Пользовательский интерфейс

## 7.1 Обязательный сценарий

```bash
OPENAI_BASE_URL="https://api.openai.com/v1" \
OPENAI_API_KEY="$OPENAI_API_KEY_QA" \
ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic" \
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY_QA" \
aiqa --verbose test=compat \
  --format=markdown,json \
  --name=compat-report
```

Ожидаемый progress:

```text
aiqa: active targets: openai, anthropic
aiqa: [openai] profile openai-compat@2026-09-01
aiqa: [openai] discovered 42 model(s)
aiqa: [anthropic] profile anthropic-messages-compat@2026-09-01
aiqa: [anthropic] discovered 3 model(s)
aiqa: [openai:model-a] start
aiqa: [anthropic:model-a] start
...
aiqa: wrote compat-report.md
aiqa: wrote compat-report.json
```

Значения ключей нигде не выводятся.

## 7.2 Правило активации target

Сигналом активации является base URL, а не наличие ключа.

| Environment | Поведение |
|---|---|
| `OPENAI_BASE_URL` + `OPENAI_API_KEY` | активировать OpenAI |
| `ANTHROPIC_BASE_URL` + `ANTHROPIC_API_KEY` | активировать Anthropic |
| обе полные пары | активировать обе |
| base URL задан, key отсутствует | config error, exit `2` |
| key задан, base URL отсутствует | target не активируется; заметка только при `--verbose` |
| ни одного base URL | config error, exit `2` |

Так AIQA не начинает платные вызовы только потому, что пользователь хранит API key в shell.

## 7.3 Явный выбор target

Добавить:

```text
--target auto|openai|anthropic|openai,anthropic
```

Default:

```text
--target=auto
```

`auto` использует env-правила выше.

Явно выбранная цель обязана иметь полную конфигурацию.

## 7.4 Выбор моделей

Добавить:

```text
OPENAI_MODEL
ANTHROPIC_MODEL

--openai-model ID[,ID]
--anthropic-model ID[,ID]
```

Приоритет:

```text
protocol-specific CLI
> protocol-specific env
> protocol discovery
```

Существующий `--model`:

- работает при одной активной цели;
- при двух целях возвращает понятную ошибку об ambiguity;
- предлагает `--openai-model` и `--anthropic-model`.

В исходной команде `--model` отсутствует, поэтому обе цели independently вызывают `/v1/models`.

## 7.5 Семантика base URL

### OpenAI

Сохраняется:

```text
OPENAI_BASE_URL включает /v1
```

### Anthropic

Следует convention официального Anthropic SDK:

```text
ANTHROPIC_BASE_URL — префикс до /v1
```

Допустимо:

```text
https://api.anthropic.com
http://127.0.0.1:8080/anthropic
```

AIQA добавляет `/v1/...`.

Normalization:

1. удалить trailing `/`;
2. сохранить path prefix;
3. если URL заканчивается ровно на `/v1`, снять этот segment и записать non-failing warning;
4. запретить query и fragment;
5. разрешить HTTP loopback;
6. remote HTTP — только с `--allow-http`;
7. никогда не добавлять `/anthropic` автоматически.

Normalized URL передаётся native client и SDK checker.

## 7.6 Profile selection

Добавить:

```text
--openai-profile openai-compat-2026-09.json
--anthropic-profile anthropic-messages-compat-2026-09.json
```

Общий `--profile` допустим только при одной цели.

## 7.7 Modules

В multi-target режиме используются qualified IDs:

```text
openai.discovery
openai.chat-completions
openai.images-generations

anthropic.discovery
anthropic.messages
anthropic.tools
anthropic.token-counting
anthropic.structured-output
anthropic.vision
anthropic.thinking
```

Примеры:

```bash
aiqa --module=auto test=compat
aiqa --module=anthropic.messages,anthropic.tools test=compat
```

Legacy OpenAI module names без prefix поддерживаются только в OpenAI-only режиме.

---

## 8. Нормативные источники и versioning

## 8.1 API version

Каждый stable Anthropic request отправляет:

```http
anthropic-version: 2023-06-01
```

Parser обязан быть forward-compatible с разрешёнными additive changes:

- новые optional поля;
- новые enum-like values;
- новые SSE event types.

Неизвестное поле или event само по себе не является failure.

## 8.2 Beta headers

Stable profile по умолчанию не отправляет `anthropic-beta`.

Правила:

- ambient `ANTHROPIC_BETA` и custom headers не влияют на native probes;
- beta feature получает отдельный opt-in module/profile;
- profile фиксирует точный beta ID;
- report перечисляет отправленные beta IDs;
- beta evidence не переписывает stable requirements;
- invalid-beta canary допустим как optional test.

## 8.3 Provenance профиля

У Anthropic нет эквивалента публичного репозитория `openai/openai-openapi`. Официальный Go SDK сообщает, что сгенерирован из Anthropic OpenAPI.

Профиль фиксирует два вида источника:

```json
{
  "sources": [
    {
      "kind": "api_version",
      "url": "https://platform.claude.com/docs/en/api/versioning",
      "revision": "2023-06-01",
      "reviewed_at": "2026-09-01"
    },
    {
      "kind": "official_sdk",
      "url": "https://github.com/anthropics/anthropic-sdk-go",
      "version": "v1.68.0",
      "commit": "d19dea9ed85bbb5fdb2d6f20fb6f903920ed23fa"
    }
  ]
}
```

Для временной совместимости с текущим loader:

```json
{
  "source": "https://github.com/anthropics/anthropic-sdk-go",
  "source_commit": "d19dea9ed85bbb5fdb2d6f20fb6f903920ed23fa",
  "source_ref_revision": "sdk:v1.68.0; anthropic-version:2023-06-01"
}
```

В profile schema v2 авторитетен `sources[]`.

## 8.4 Проверка актуальности

`script/check-profile-upstream.go` превращается в:

```text
check-profile-upstream --profile <file>
```

Стратегии:

```text
github_latest_commit
github_latest_release
manual_api_version
```

Для Anthropic:

- сравнить pinned SDK release с latest stable release;
- version docs пометить как manual-review source;
- exit `1`, если upstream изменился;
- exit `2`, если maintenance check не выполнился.

---

## 9. Новый профиль

Файл:

```text
profile/anthropic-messages-compat-2026-09.json
```

Skeleton:

```json
{
  "schema_version": "aiqa.profile.v2",
  "id": "anthropic-messages-compat",
  "protocol": "anthropic",
  "revision": "2026-09-01",
  "api_version": "2023-06-01",
  "base_url_semantics": "prefix_before_v1",
  "source": "https://github.com/anthropics/anthropic-sdk-go",
  "source_commit": "d19dea9ed85bbb5fdb2d6f20fb6f903920ed23fa",
  "source_ref_revision": "sdk:v1.68.0; anthropic-version:2023-06-01",
  "scope": "GET /v1/models, GET /v1/models/{model_id}, POST /v1/messages, POST /v1/messages/count_tokens",
  "modules": [
    {
      "id": "anthropic.discovery",
      "subject": "provider",
      "always_applicable": true,
      "default_enabled": true,
      "cost_class": "none",
      "probe_ids": []
    },
    {
      "id": "anthropic.messages",
      "subject": "model",
      "capabilities": ["anthropic.messages.text"],
      "default_enabled": true,
      "cost_class": "token",
      "probe_ids": []
    },
    {
      "id": "anthropic.tools",
      "subject": "model",
      "capabilities": ["anthropic.messages.tools"],
      "default_enabled": true,
      "cost_class": "token",
      "probe_ids": []
    },
    {
      "id": "anthropic.token-counting",
      "subject": "model",
      "capabilities": ["anthropic.messages.text"],
      "default_enabled": true,
      "cost_class": "metadata",
      "probe_ids": []
    },
    {
      "id": "anthropic.structured-output",
      "subject": "model",
      "capabilities": ["anthropic.messages.structured_output"],
      "default_enabled": true,
      "cost_class": "token",
      "probe_ids": []
    },
    {
      "id": "anthropic.vision",
      "subject": "model",
      "capabilities": ["anthropic.messages.image_input"],
      "default_enabled": true,
      "cost_class": "token-media-input",
      "probe_ids": []
    },
    {
      "id": "anthropic.thinking",
      "subject": "model",
      "capabilities": [
        "anthropic.messages.thinking.adaptive",
        "anthropic.messages.thinking.manual"
      ],
      "default_enabled": false,
      "cost_class": "high-token",
      "probe_ids": []
    }
  ],
  "probes": []
}
```

Profile остаётся нормативным manifest, а не универсальным DSL для HTTP requests. Typed request construction остаётся в Go.

---

## 10. Capability classification

## 10.1 Namespace

Добавить:

```text
anthropic.messages.text
anthropic.messages.image_input
anthropic.messages.tools
anthropic.messages.structured_output
anthropic.messages.thinking.adaptive
anthropic.messages.thinking.manual
anthropic.messages.prompt_caching
```

Нельзя использовать OpenAI capability `chat.completions.text`: поддержка Anthropic Messages не доказывает Chat Completions.

## 10.2 Источники evidence

Приоритет:

1. `--anthropic-model-capability` или target-qualified capability file;
2. structured `capabilities` из Anthropic Models API;
3. другие structured fields — supported inputs, `max_input_tokens`, `max_tokens`;
4. осторожные model-name rules только для optional routing;
5. unknown/default policy.

Sibling OpenAI catalog никогда не используется.

## 10.3 Unknown policy

Для модели, полученной именно из Anthropic `/v1/models`, разрешено назначить:

```text
anthropic.messages.text
```

с confidence `low`, если нет evidence другой природы и выбран Messages profile.

Добавить:

```text
--anthropic-unknown-model=messages|inventory
```

Default:

```text
messages
```

## 10.4 Tools

Если granular metadata нет:

- tools module остаётся optional;
- его можно попробовать на text Messages model;
- подходящий 4xx → `UNSUP`;
- 408/429/5xx/timeout/mid-stream error → `ERROR`;
- malformed 2xx tool block → `FAIL`;
- `N/A` только при положительном evidence неприменимости.

## 10.5 Thinking

Thinking нельзя выводить только из имени `claude-*`.

Приемлемы:

- structured model capability;
- explicit override;
- reviewed non-normative official model map;
- иначе high-token module пропускается.

Manual и adaptive thinking — разные capabilities.

---

## 11. Каталог probes

`Core` обязателен внутри применимого module. `Optional` может дать `UNSUP`, не ломая minimum profile.

## 11.1 Discovery, version и auth

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.models.list.http` | Core | `GET /v1/models?limit=1000` | 2xx JSON |
| `anthropic.models.list.schema` | Core | Проверить envelope и entries | `data[]`; `id`; `type=model`; RFC3339 `created_at`; `display_name`; корректные pagination fields |
| `anthropic.models.list.pagination` | Optional | `limit=1`, затем cursor | следующая page без duplicate |
| `anthropic.models.retrieve` | Optional | `GET /v1/models/{escaped_id}` | returned ID совпадает |
| `anthropic.header.version.accepted` | Core | Positive calls с `2023-06-01` | version syntax не отклонён |
| `anthropic.header.version.required` | Optional | Один request без version | подходящий 4xx Anthropic error |
| `anthropic.header.version.invalid` | Optional | Invalid version | подходящий 4xx |
| `anthropic.auth.x_api_key` | Core | Positive calls с `x-api-key` | valid key принят |
| `anthropic.error.invalid_auth` | Optional | Generated invalid key | 401/403 + Anthropic envelope |

Важно:

- не ждать OpenAI Unix `created`;
- неизвестные model fields разрешены;
- отсутствие optional `capabilities`, `max_input_tokens`, `max_tokens` не ломает schema;
- pagination не запускается при `has_more=false`.

## 11.2 Basic Messages

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.messages.basic` | Core | Minimal text request | 2xx Message с marker text |
| `anthropic.messages.schema` | Core | Validate non-stream response | корректные `id`, `type`, `role`, `model`, `content[]`, `stop_reason`, `usage` |
| `anthropic.messages.usage` | Core | Validate usage | non-negative input/output и согласованные optional details |
| `anthropic.messages.content.string` | Optional | `content` string | marker интерпретирован |
| `anthropic.messages.content.text_blocks` | Optional | array text blocks | marker интерпретирован |
| `anthropic.messages.system.top_level` | Optional | top-level `system` | semantic effect доказан |
| `anthropic.messages.multi_turn` | Optional | user/assistant/user | history использована |
| `anthropic.messages.assistant_prefill` | Optional | final assistant prefix | response продолжает prefix |
| `anthropic.messages.stop_sequences` | Optional | unique stop sequence | корректные stop fields |
| `anthropic.messages.max_tokens` | Optional | малый output limit | совместимый stop reason |
| `anthropic.messages.metadata` | Optional | bounded `metadata.user_id` | request принят |

Basic body:

```json
{
  "model": "<model>",
  "max_tokens": 32,
  "messages": [
    {
      "role": "user",
      "content": "Reply with exactly AIQA_OK and nothing else."
    }
  ]
}
```

В ответе может быть несколько blocks. Marker ищется среди `text` blocks; нельзя предполагать, что `content[0]` всегда text.

Unknown content-block types и будущие stop reasons допускаются структурно.

## 11.3 Errors

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.error.invalid_model` | Core | Generated missing model | client error с Anthropic envelope |
| `anthropic.error.invalid_json` | Core | 3 malformed JSON variants | 4xx, не 5xx |
| `anthropic.error.invalid_types` | Optional | wrong types | 4xx, не upstream 5xx |
| `anthropic.error.shape` | Core | Validate representative error | `type=error`, `error.type`, `error.message` |
| `anthropic.error.request_id` | Optional | Inspect header/body | request ID, если provider отдаёт |
| `anthropic.error.overload_mapping` | Mock/informative | Mock 529 | классифицируется `ERROR` |
| `anthropic.error.stream_event` | Mock/informative | `event:error` после 200 | `ERROR`, не capability failure |

Live suite не пытается искусственно вызвать:

- 32 MB overflow;
- billing exhaustion;
- revoked permissions;
- overload;
- internal error.

Эти ветки проверяются mock fixtures.

## 11.4 Streaming

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.messages.stream.http` | Core | `stream:true` | 2xx + `text/event-stream` |
| `anthropic.messages.stream.sequence` | Core | Parse named events | legal sequence до `message_stop` |
| `anthropic.messages.stream.event_type_match` | Core | Сравнить `event:` и JSON `type` | известные types совпадают |
| `anthropic.messages.stream.text` | Core | Собрать `text_delta` | marker восстановлен |
| `anthropic.messages.stream.usage` | Optional | Собрать usage | final compatible usage |
| `anthropic.messages.stream.ping` | Mock/informative | Inject ping | parser не ломается |
| `anthropic.messages.stream.unknown_event` | Mock/informative | Unknown event | безопасно сохранён/пропущен |
| `anthropic.messages.stream.no_done_sentinel` | Optional | Observe end | stream завершается `message_stop` |
| `anthropic.messages.stream.midstream_error` | Runtime | Получить `event:error` | probe `ERROR` |

State machine:

```text
message_start
  (ping)*
  (
    content_block_start(index)
    (content_block_delta(index))*
    content_block_stop(index)
    (ping)*
  )*
  message_delta+
message_stop
```

Правила:

- index неотрицательный;
- block start предшествует delta/stop;
- block не закрывается дважды;
- text deltas конкатенируются;
- `input_json_delta.partial_json` парсится после block stop;
- `message_delta.usage` cumulative;
- unknown events не являются failure;
- `[DONE]` не ожидается;
- provider `event:error` после HTTP 200 — inconclusive runtime error.

## 11.5 Tools

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.tools.auto` | Optional | `tool_choice:auto` | valid tool_use при выборе |
| `anthropic.tools.any` | Optional | `tool_choice:any` | хотя бы один tool_use |
| `anthropic.tools.specific` | Optional | forced named tool | name и object input корректны |
| `anthropic.tools.none` | Optional | `tool_choice:none` | tool_use отсутствует |
| `anthropic.tools.input_schema` | Optional | known required property | input проходит local schema |
| `anthropic.tools.continuation` | Optional | tool_use → tool_result | final answer использует result |
| `anthropic.tools.parallel` | Optional | две независимые сущности | distinct IDs, если поддерживается |
| `anthropic.tools.stream` | Optional | streamed forced tool | partial JSON собирается |
| `anthropic.tools.strict` | Optional | `strict:true` | schema-conforming input |

Tool:

```json
{
  "name": "get_status",
  "description": "Return status for a resource.",
  "input_schema": {
    "type": "object",
    "properties": {
      "resource_id": {"type": "string"}
    },
    "required": ["resource_id"],
    "additionalProperties": false
  }
}
```

Expected block:

```json
{
  "type": "tool_use",
  "id": "toolu_...",
  "name": "get_status",
  "input": {
    "resource_id": "resource-alpha"
  }
}
```

Continuation:

```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "toolu_...",
      "content": "resource-alpha is available"
    }
  ]
}
```

Если initial tool call не получен, continuation → `SKIP` + `blocked_by`.

## 11.6 Token counting

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.tokens.count.basic` | Optional | Count fixed text request | 2xx + positive integer |
| `anthropic.tokens.count.schema` | Optional | Validate response | compatible `{input_tokens}` |
| `anthropic.tokens.count.system` | Optional | Count с system | валидный увеличенный count |
| `anthropic.tokens.count.tools` | Optional | Count с tool schema | валидный count |
| `anthropic.tokens.count.consistency` | Informative | Compare с Message usage | записать delta/ratio |

Consistency — heuristic, а не normative equality:

```json
{
  "count_tokens_input": 42,
  "message_usage_input": 44,
  "absolute_delta": 2,
  "relative_delta": 0.045
}
```

Initial warning threshold:

```text
absolute delta > max(16 tokens, 10% larger value)
```

Поле помечается `aiqa_heuristic`, policy version фиксируется, core score не меняется.

## 11.7 Structured output

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.structured.json_schema` | Optional | `output_config.format` | text block содержит schema-valid JSON |
| `anthropic.structured.stop_reason` | Optional | Inspect completion | normal completion; refusal/max_tokens inconclusive |
| `anthropic.tools.strict_schema` | Optional | strict tool | schema-valid tool input |

Request fragment:

```json
{
  "output_config": {
    "format": {
      "type": "json_schema",
      "schema": {
        "type": "object",
        "properties": {
          "ok": {"type": "boolean"},
          "value": {"type": "string"}
        },
        "required": ["ok", "value"],
        "additionalProperties": false
      }
    }
  }
}
```

Правила:

- локально валидировать JSON Schema;
- `stop_reason=max_tokens` → `WARN`;
- `stop_reason=refusal` → `WARN`;
- 4xx на optional field → `UNSUP`;
- normal 2xx с malformed/schema-violating result → `FAIL`.

## 11.8 Vision

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.vision.image_base64` | Optional/applicable | Embedded tiny PNG | image принят и semantic answer получен |
| `anthropic.vision.image_schema` | Optional/applicable | Validate request | нет translation/schema failure |
| `anthropic.vision.usage` | Informative | Observe usage | non-negative |

Используется repository fixture, без внешнего URL.

В report не хранить base64. Только:

```text
media_type
decoded_bytes
width
height
sha256
```

## 11.9 Thinking

Module выключен по умолчанию:

```bash
aiqa test=compat \
  --module=anthropic.thinking \
  --allow-high-token-probes
```

| Requirement ID | Level | Действие | PASS |
|---|---|---|---|
| `anthropic.thinking.adaptive` | Optional/applicable | `thinking:{type:adaptive}` | compatible blocks/usage |
| `anthropic.thinking.effort` | Optional/applicable | supported effort variants | accepted/effective evidence |
| `anthropic.thinking.manual` | Optional/applicable | enabled + budget 1024 | compatible blocks |
| `anthropic.thinking.manual.validation` | Optional/applicable | invalid budget | suitable 4xx |
| `anthropic.thinking.stream` | Optional/applicable | thinking stream | legal deltas + final usage |

Нельзя сохранять thinking text или redacted payload. Только:

```text
block_type
character/byte length
thinking_tokens
signature_present
signature_sha256
```

Manual и adaptive проверяются отдельно. Model-specific 4xx не становится gateway defect.

---

## 12. Native Anthropic client

## 12.1 Новая структура packages

Разделить generic transport и protocol logic:

```text
internal/transport/
  client.go
  pacing.go
  response.go
  sse.go
  telemetry.go

internal/protocol/openai/
  client.go
  stream.go
  validate.go

internal/protocol/anthropic/
  client.go
  headers.go
  models.go
  messages.go
  stream.go
  validate.go
  errors.go
  usage.go
```

`internal/transport` отвечает за:

- TLS policy;
- bounded body reads;
- context и timeouts;
- retry scheduling;
- redaction-safe HTTP observations;
- rate-limit telemetry;
- generic JSON decoder;
- generic SSE line framing.

Protocol package отвечает за:

- endpoint paths;
- auth;
- version headers;
- request/response schema;
- stream state machine;
- usage mapping;
- error classification.

Старый `internal/api` можно сохранить как compatibility facade на время migration, но новая Anthropic logic не должна зависеть от OpenAI types.

## 12.2 Headers

Каждый stable positive Anthropic request:

```http
Content-Type: application/json
Accept: application/json
x-api-key: <secret>
anthropic-version: 2023-06-01
User-Agent: aiqa/<version>
```

Для streaming:

```http
Accept: text/event-stream
```

Внутри report header names canonical lower-case, values ключей всегда redacted.

Mapping:

```text
ANTHROPIC_API_KEY → x-api-key
```

`ANTHROPIC_AUTH_TOKEN` stable direct profile не использует. Его поддержка возможна отдельным auth mode, но ambient value не может override API key.

## 12.3 Path join

Пути собираются через URL path segments, не string concatenation.

```text
base: http://127.0.0.1:8080/anthropic
path: /v1/messages

result:
http://127.0.0.1:8080/anthropic/v1/messages
```

Обязательные tests:

```text
base с trailing slash
base без trailing slash
path prefix
model ID со slash
base, заканчивающийся /v1
query/fragment rejection
IPv4 loopback
IPv6 loopback
```

## 12.4 Retry policy

Native AIQA управляет retries сама.

Transient classes:

```text
connection error
408
429
500
502
503
504
529
```

Правила:

- default максимум 3 attempts;
- учитывать `retry-after`;
- exponential backoff + jitter;
- deterministic 4xx не retry;
- exhausted transient failure → probe `ERROR`;
- retry telemetry не меняет conformance;
- mid-stream error после partial output автоматически не retry;
- official SDK checker запускается с SDK retries = 0.

Если response содержит `x-should-retry: false`, transport не должен повторять deterministic 5xx.

## 12.5 Error parser

Ожидаемый envelope:

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "..."
  },
  "request_id": "req_..."
}
```

Сохранять:

```text
HTTP status
error.type
sanitized error.message
request_id из body/header
retry-after
```

Extra fields разрешены.

## 12.6 Request limits

В protocol metadata фиксируются документированные limits:

```text
Messages API: 32 MB
Token Counting API: 32 MB
```

Live probe не отправляет 32 MB. Classification 413 проверяется на mock.

---

## 13. Anthropic streaming parser

OpenAI parser можно переиспользовать только на уровне разбиения SSE lines. Accumulation и validation должны быть отдельными.

Предлагаемый state:

```go
type MessageStreamState struct {
    Started       bool
    Stopped       bool
    MessageID     string
    Model         string
    Role          string
    StopReason    string
    StopSequence  *string
    Blocks        map[int]*BlockState
    OrderedBlocks []ContentBlock
    Usage         AnthropicUsage
    Pings         int
    UnknownEvents []UnknownEvent
    Warnings      []string
    StreamError   *AnthropicError
}

type BlockState struct {
    Index       int
    Type        string
    Started     bool
    Stopped     bool
    Text        strings.Builder
    PartialJSON strings.Builder
    ThinkingLen int
    Signature   hash.Hash
}
```

Outcome distinction:

| Наблюдение | Status |
|---|---|
| malformed SSE framing | `FAIL` |
| illegal ordering known events | `FAIL` |
| undecodable JSON event | `FAIL` |
| unknown future event | informative / `WARN` только при воздействии |
| provider `event:error` | `ERROR` |
| client cancellation | `ERROR` |
| EOF до `message_stop` | `ERROR`, если нет доказанного structural violation |
| normal `message_stop` | завершение |

Parser возвращает normalized accumulated Message, чтобы native и SDK evidence сравнивались по structure, а не по generated prose.

---

## 14. Checker на официальном Anthropic Go SDK

## 14.1 Process boundary

Root `go.mod` не получает зависимость от Anthropic SDK.

Добавить отдельный module:

```text
tools/anthropic-sdk-checker/
  go.mod
  go.sum
  cmd/anthropic-sdk-checker/main.go
  internal/suites/
  Dockerfile
```

Pin:

```text
github.com/anthropics/anthropic-sdk-go v1.68.0
```

## 14.2 SDK construction

Checker обязан:

- явно задать normalized base URL;
- явно задать API key;
- выключить environment defaults;
- выключить SDK retries;
- использовать bounded HTTP client;
- не печатать key.

Концептуально:

```go
client := anthropic.NewClient(
    option.WithoutEnvironmentDefaults(),
    option.WithBaseURL(baseURL),
    option.WithAPIKey(apiKey),
    option.WithMaxRetries(0),
    option.WithHTTPClient(httpClient),
)
```

Даже если официальный SDK умеет читать `ANTHROPIC_BASE_URL`, checker не должен зависеть от ambient process state.

## 14.3 Suites

```text
models_list
models_get
messages
messages_stream
messages_system
messages_multi_turn
messages_tools
messages_tools_stream
messages_count_tokens
messages_structured_output
error_responses
```

Vision и thinking — opt-in и capability-routed.

## 14.4 Machine-readable output

Не повторять regex parsing текстового OpenAI checker.

Anthropic checker выдаёт JSON Lines:

```json
{"schema_version":"aiqa.external.v1","tool":"anthropic-official-sdk","suite":"messages","status":"pass","duration_ms":431,"summary":"pass"}
{"schema_version":"aiqa.external.v1","tool":"anthropic-official-sdk","suite":"messages_stream","status":"fail","duration_ms":512,"summary":"stream event could not be accumulated","signals":[{"code":"anthropic.sdk.stream_parse_failed"}]}
```

Exit codes:

```text
0 — suites passed или явно skipped
1 — хотя бы один suite failed
2 — checker configuration/runner error
```

AIQA читает каждую JSONL entry независимо. Broken line не должна терять уже распарсенные evidence.

## 14.5 Checker registry

```go
type Checker interface {
    ID() string
    Supports(protocol string, module string) bool
    ResolveMode(...) ...
    RunModule(...) []result.ExternalResult
}
```

Registry:

```text
openai-official-sdk
anthropic-official-sdk
```

Общие и protocol overrides:

```text
--external auto|off|binary|docker

--openai-external auto|off|binary|docker
--anthropic-external auto|off|binary|docker

--openai-external-image IMAGE
--anthropic-external-image IMAGE
```

Precedence:

```text
protocol override
> shared --external
> auto
```

`auto` не скачивает image. Используется installed binary или уже присутствующий local image.

## 14.6 Requirement mapping

Примеры:

```text
models_list
  → official-sdk.anthropic.models.list
  related: anthropic.models.list.schema

messages
  → official-sdk.anthropic.messages
  related: anthropic.messages.basic
           anthropic.messages.schema

messages_stream
  → official-sdk.anthropic.messages.stream
  related: anthropic.messages.stream.sequence

messages_tools
  → official-sdk.anthropic.tools
  related: anthropic.tools.specific
           anthropic.tools.continuation
```

Native/SDK disagreement создаёт evidence conflict на SDK interoperability requirement. Native core requirement не переписывается.

---

## 15. Multi-target config model

```go
type Protocol string

const (
    ProtocolOpenAI    Protocol = "openai"
    ProtocolAnthropic Protocol = "anthropic"
)

type TargetConfig struct {
    ID              string
    Protocol        Protocol
    BaseURL         string
    NormalizedURL   string
    APIKey          string
    ModelIDs        []string
    ProfileFile     string
    Modules         []string
    ExternalMode    string
    ExternalImage   string
    RequestInterval time.Duration
}

type Config struct {
    Targets                []TargetConfig
    Tests                  []string
    Timeout                time.Duration
    Formats                []string
    Name                   string
    Concurrency            int
    FailOn                 string
    GuidanceMode           string
    GuidancePack           string
    GuidanceLocale         string
    ReportSchema           string
    AllowHTTP              bool
    AllowBillableArtifacts bool
    AllowHighTokenProbes   bool
    MaxArtifactsPerModel   int
}
```

Secrets не копируются в public config.

Default target IDs:

```text
openai
anthropic
```

Они уникальны в одном report.

---

## 16. Runner abstraction

```go
type TargetRunner interface {
    Protocol() Protocol
    Profile() profile.Profile
    Discover(context.Context) DiscoveryResult
    Classify(Model) capability.Classification
    Plan(Model, profile.Profile, config.TargetConfig) []ModulePlan
    RunProviderModule(context.Context, ModulePlan) []result.TestResult
    RunModelModule(context.Context, Model, ModulePlan) []result.TestResult
    RunExternal(context.Context, Model, ModulePlan) []result.ExternalResult
    Telemetry() result.RateLimitStats
}
```

Flow:

```text
parse common config + targets
load profile на target
create runner на target

для каждого target:
    discover inventory
    select models
    classify capabilities
    build module plan
    execute bounded work
    normalize external evidence
    calculate target assessment
    generate target guidance

aggregate run verdict
write reports
apply exit policy
```

Порядок:

```text
explicit --target order
иначе openai, anthropic
```

Execution:

- targets могут идти параллельно под общим global semaphore;
- у каждой цели собственный adaptive pacer;
- одна 429 не throttle другую цель;
- report order всегда deterministic.

---

## 17. Report schema v2

## 17.1 Зачем v2

V1 требует singular:

```text
profile
config
discovery
models
summary
```

В нём невозможно честно представить два контракта. Anthropic нельзя помещать в тот же `models[]`, иначе теряется protocol context.

## 17.2 Root contract

```json
{
  "schema_version": "aiqa.report.v2",
  "generated_at": "2026-09-01T12:00:00Z",
  "tool": {},
  "config": {
    "tests": ["compat"],
    "targets": [
      {
        "id": "openai",
        "protocol": "openai",
        "base_url": "https://api.openai.com/v1",
        "requested_models": []
      },
      {
        "id": "anthropic",
        "protocol": "anthropic",
        "base_url": "http://127.0.0.1:8080/anthropic",
        "requested_models": [],
        "api_version": "2023-06-01"
      }
    ]
  },
  "targets": [
    {
      "id": "openai",
      "protocol": "openai",
      "profile": {},
      "discovery": {},
      "models": [],
      "requirements": [],
      "summary": {},
      "guidance": {},
      "rate_limit": {}
    },
    {
      "id": "anthropic",
      "protocol": "anthropic",
      "profile": {},
      "discovery": {},
      "models": [],
      "requirements": [],
      "summary": {},
      "guidance": {},
      "rate_limit": {}
    }
  ],
  "summary": {
    "verdict": "pass-with-gaps",
    "verdict_reason": "one_or_more_targets_have_optional_gaps",
    "target_counts": {
      "pass": 1,
      "pass_with_gaps": 1,
      "partial": 0,
      "fail": 0,
      "inconclusive": 0
    }
  }
}
```

## 17.3 Subject identity

```json
{
  "target_id": "anthropic",
  "protocol": "anthropic",
  "kind": "model",
  "id": "provider/model-a"
}
```

`target_id` входит в:

- requirement key;
- evidence ID;
- finding fingerprint;
- artifact reference;
- recommendation;
- verification target.

## 17.4 Requirement IDs

Новые Anthropic IDs qualified:

```text
anthropic.models.list.schema
anthropic.messages.basic
anthropic.messages.stream.sequence
anthropic.tools.continuation
```

Existing OpenAI IDs можно сохранить внутри OpenAI target. Глобально уникальная ссылка в v2:

```text
target_id + requirement.id
```

## 17.5 Provider-neutral usage

```go
type Usage struct {
    InputTokens              int64 `json:"input_tokens,omitempty"`
    OutputTokens             int64 `json:"output_tokens,omitempty"`
    CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
    CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
    ThinkingTokens           int64 `json:"thinking_tokens,omitempty"`

    PromptTokens             int64 `json:"prompt_tokens,omitempty"`
    CompletionTokens         int64 `json:"completion_tokens,omitempty"`
    ReasoningTokens          int64 `json:"reasoning_tokens,omitempty"`

    ProviderFields map[string]int64 `json:"provider_fields,omitempty"`
}
```

Правила:

- Anthropic → input/output;
- OpenAI → prompt/completion;
- не выдумывать provider field `total_tokens`;
- display может показать `computed_total`;
- сохранить cache/thinking details;
- root не складывает разные протоколы в unlabeled total.

## 17.6 Target scoring

Каждая цель использует уже принятую модель:

```text
verdict
core conformance
optional capability coverage
evidence coverage
execution coverage
```

Cross-target weighted score отсутствует.

Run verdict:

```text
если есть target FAIL:
    FAIL
иначе если есть INCONCLUSIVE:
    INCONCLUSIVE
иначе если есть PARTIAL:
    PARTIAL
иначе если есть PASS-WITH-GAPS:
    PASS-WITH-GAPS
иначе:
    PASS
```

Root сообщает blockers:

```json
{
  "verdict": "fail",
  "blocking_targets": ["anthropic"]
}
```

## 17.7 Markdown mockup

```markdown
# AIQA compatibility report

## Targets

| Target | Protocol | Profile | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---|---|---:|---:|---:|---:|---:|---:|
| `openai` | OpenAI | `openai-compat@2026-09-01` | PASS | 12/12 (100%) | 18/18 (100%) | 30/30 (100%) | 4 | 0 |
| `anthropic` | Anthropic | `anthropic-messages-compat@2026-09-01` | PASS-WITH-GAPS | 11/11 (100%) | 13/17 (76.5%) | 27/28 (96.4%) | 3 | 4 |

**Run verdict:** PASS-WITH-GAPS

Targets are assessed independently. Percentages are not combined into
a cross-protocol score.

## Fix first

No core failure was found.

## Improve Anthropic compatibility

### P2 — Support `POST /v1/messages/count_tokens`

**Observed:** `404 not_found_error`  
**Scope:** target `anthropic`, provider endpoint  
**Why it matters:** Anthropic SDK clients use token counting before message creation.  
**Suggested action:** expose `/anthropic/v1/messages/count_tokens` and return
`{"input_tokens": <non-negative integer>}`.  
**Verify:** `anthropic.tokens.count.basic`.

## Target: Anthropic

- Base URL: `http://127.0.0.1:8080/anthropic`
- API version: `2023-06-01`
- Auth: `x-api-key` (redacted)

### Models
...
```

Passing checks и raw evidence остаются collapsed.

---

## 18. Guidance и finding catalog

Добавить:

```text
guidance/catalog/anthropic-messages.json
```

Signal codes:

```text
anthropic.header.version.missing
anthropic.header.version.rejected
anthropic.auth.x_api_key.rejected
anthropic.models.list.invalid_envelope
anthropic.models.entry.created_at_missing
anthropic.models.entry.created_at_invalid
anthropic.message.invalid_envelope
anthropic.message.content_not_array
anthropic.message.role_not_assistant
anthropic.message.usage_missing
anthropic.message.usage_inconsistent
anthropic.error.openai_shaped
anthropic.error.invalid_shape
anthropic.stream.event_name_missing
anthropic.stream.type_mismatch
anthropic.stream.sequence_invalid
anthropic.stream.done_sentinel_only
anthropic.stream.tool_json_invalid
anthropic.stream.provider_error
anthropic.tools.use_invalid
anthropic.tools.result_roundtrip_failed
anthropic.tokens.endpoint_missing
anthropic.structured.schema_violation
anthropic.capability.unsupported
anthropic.sdk.parse_failed
anthropic.evidence.conflict
```

Примеры recommendations.

### Version header

```text
Priority: P0, если core request не проходит
Action: fix
Owner: Anthropic facade / request validator
Recommendation:
Accept `anthropic-version: 2023-06-01` and route under that stable contract.
Verify:
anthropic.header.version.accepted
```

### OpenAI-shaped response на Anthropic endpoint

```text
Priority: P0
Action: fix
Recommendation:
Translate `choices[].message` into `type=message`, `role=assistant`,
`content[]`, `stop_reason` and Anthropic usage.
Verify:
anthropic.messages.schema
```

### Неверный stream

```text
Priority: P0/P1
Action: fix
Recommendation:
Emit named Anthropic SSE events in legal order and finish with
`message_stop`; do not expose only OpenAI data chunks and `[DONE]`.
Verify:
anthropic.messages.stream.sequence
```

### Нет count_tokens

```text
Priority: P2
Action: implement or document
Recommendation:
Implement `/v1/messages/count_tokens`. If tokenizer parity is unavailable,
document estimation while keeping the compatible response shape.
Verify:
anthropic.tokens.count.basic
```

### Thinking не поддерживается моделью

```text
Priority: P2
Action: document
Attribution: model capability
Recommendation:
Advertise capability accurately or route the module as not applicable.
Do not classify this as a whole-gateway defect.
```

Guidance никогда не меняет conformance.

---

## 19. External artifacts

Anthropic SDK logs используют существующий content-addressed artifact mechanism.

```json
{
  "target_id": "anthropic",
  "tool": "anthropic-official-sdk",
  "media_type": "text/plain",
  "sha256": "...",
  "bytes": 1234,
  "redacted": true
}
```

Redaction:

```text
x-api-key
Authorization
ANTHROPIC_API_KEY
ANTHROPIC_AUTH_TOKEN
cookies
signed query strings
large tool results
thinking
redacted_thinking
base64 media
```

Один artifact рендерится один раз.

---

## 20. Exit policy

Reports записываются до применения exit policy.

Target policy:

| `--fail-on` | exit 1 |
|---|---|
| `core` | target `FAIL` или `INCONCLUSIVE` |
| `any` | target не `PASS` |
| `never` | conformance не ломает процесс |

Multi-target: process exit `1`, если хотя бы одна активная цель нарушает policy.

| OpenAI | Anthropic | `core` | Exit |
|---|---|---:|---:|
| PASS | PASS-WITH-GAPS | accepted | 0 |
| PASS | PARTIAL | accepted | 0 |
| PASS | FAIL | rejected | 1 |
| INCONCLUSIVE | PASS | rejected | 1 |
| PASS | PASS | accepted | 0 |

Exit `2` — config/orchestration/report error.

Явно выбранный, но некорректно настроенный target нельзя silently drop.

---

## 21. Security и privacy

1. API keys не попадают в JSON, Markdown, logs, artifacts и panic.
2. `x-api-key` отправляется только Anthropic target.
3. OpenAI Bearer отправляется только OpenAI target.
4. External checker не использует ambient SDK credentials.
5. Native suite не наследует beta/custom headers.
6. Response bodies и SSE storage bounded.
7. Provider messages sanitized/truncated.
8. Full prompts не сохраняются по умолчанию.
9. Tool results сохраняются только кратким sanitized observation.
10. Thinking text и signatures не сохраняются.
11. Media base64 не сохраняется.
12. Sensitive artifacts представлены hash + metadata.
13. Loopback HTTP разрешён; remote plaintext требует flag.
14. Model output и tool arguments считаются untrusted.
15. Markdown/HTML escaping обязателен.
16. Default vision probe не делает remote fetch.
17. SDK retries выключены.
18. Перед high-token thinking выводится cost warning.
19. Beta modules default off.
20. Target clients не делят mutable auth state.

---

## 22. Telemetry

На каждую цель отдельно:

```text
successful native requests
rate-limited responses
recovered retries
backoff duration
safe requests/second
safe concurrency
mean/P50/P95 successful latency
```

Добавить `request-id` в allowlist request ID headers.

529 → overload observation + `ERROR`, не `FAIL`.

Для streaming:

```text
time_to_message_start
time_to_first_text_delta
time_to_message_stop
```

Это diagnostics, не scoring.

---

## 23. Test strategy

## 23.1 Unit tests

### Config

- OpenAI pair активирует только OpenAI.
- Anthropic pair активирует только Anthropic.
- Обе пары активируют обе цели в deterministic order.
- Base без key → exit `2`.
- Key без base не активирует target.
- Нет целей → exit `2`.
- `--target` требует выбранные targets.
- `--model` rejected в multi-target.
- Protocol-specific model flag overrides env.
- Anthropic path prefix корректно соединяется с `/v1`.
- Trailing `/v1` normalization записывается.
- Secrets отсутствуют в public config.

### Profile

- Старый OpenAI profile v1 загружается.
- Anthropic profile v2 загружается.
- Wrong protocol rejected.
- Missing API version rejected.
- Duplicate module/probe/requirement rejected.
- Unqualified Anthropic ID rejected.
- Source provenance validated.
- Capability namespace validated.

### Models и Messages

- Valid/invalid Models fixtures.
- Retrieve model ID mismatch.
- Message с одним и несколькими blocks.
- Unknown content block tolerated.
- Unknown stop reason tolerated.
- Missing required usage fails.
- Error envelope variants.
- Count-token response.

### Streaming

- Normal text sequence.
- Multiple blocks.
- Ping events.
- Unknown events.
- Tool partial JSON.
- Thinking deltas без retention текста.
- Event-name/data-type mismatch.
- Duplicate start/stop.
- Delta before start.
- EOF before stop.
- `event:error` after HTTP 200.
- Отсутствие `[DONE]`.
- Accidental `[DONE]` диагностируется, но не используется как terminator.

### Assessment и guidance

- Target ID входит в key/fingerprint.
- Один model ID в двух целях не merge.
- OpenAI и Anthropic findings не deduplicate.
- Anthropic 529 → error/inconclusive.
- Optional 4xx → unsupported.
- Required unsupported → fail.
- Guidance не меняет verdict.
- Projected verdict остаётся target-local.

## 23.2 Dual-facade mock

Расширить mock:

```text
/openai/v1/models
/openai/v1/chat/completions

/anthropic/v1/models
/anthropic/v1/models/{id}
/anthropic/v1/messages
/anthropic/v1/messages/count_tokens
```

Mock поддерживает:

- текущие positive OpenAI tests;
- Anthropic non-stream text;
- Anthropic named-event stream;
- tool use + continuation;
- token counting;
- structured output;
- invalid auth/version/model/JSON/types;
- configurable 429/529;
- malformed stream fixtures;
- независимые inventories.

## 23.3 Exact-command acceptance test

В integration test выполнить эквивалент:

```bash
OPENAI_BASE_URL="$MOCK/openai/v1" \
OPENAI_API_KEY="openai-test-key" \
ANTHROPIC_BASE_URL="$MOCK/anthropic" \
ANTHROPIC_API_KEY="anthropic-test-key" \
aiqa --verbose test=compat \
  --format=markdown,json \
  --name=compat-report
```

Assertions:

- оба файла существуют;
- JSON проходит v2 schema;
- targets ровно два;
- у каждого собственный profile и key;
- full mock даёт target PASS;
- Markdown содержит две target sections;
- keys отсутствуют;
- вызваны только ожидаемые protocol paths;
- exit code `0`.

## 23.4 OpenAI regression

Текущий сценарий:

```bash
OPENAI_BASE_URL="$MOCK/openai/v1" \
OPENAI_API_KEY="key" \
aiqa test=compat
```

должен сохраняться.

На migration phase:

- `--report-schema=v1` воспроизводит current shape;
- `--report-schema=auto` может вернуть v1 для OpenAI-only;
- old/new OpenAI results сравниваются semantic fixtures.

## 23.5 SDK checker

- explicit base URL и key;
- environment defaults off;
- retries off;
- JSONL без regex;
- malformed output → runner error;
- suite → requirement mapping;
- model IDs со slash;
- valid stream проходит SDK accumulator;
- invalid stream вызывает SDK evidence failure;
- key redacted;
- Docker loopback работает на Linux.

## 23.6 Optional live CI

Jobs:

```text
live-openai
live-anthropic
live-dual-bifrost
```

Live tests:

- запускаются только при secrets;
- сначала non-blocking;
- используют `OPENAI_MODEL` / `ANTHROPIC_MODEL`;
- не тестируют весь production catalog;
- публикуют sanitized artifacts;
- имеют token/cost budget.

---

## 24. Migration report schema

## Phase A — v2 рядом с v1

Добавить:

```text
--report-schema=auto|v1|v2
```

Default:

```text
auto
```

| Targets | `auto` |
|---|---|
| OpenAI only | v1 на один deprecation cycle |
| Anthropic only | v2 |
| OpenAI + Anthropic | v2 |

`--report-schema=v1` при активном Anthropic → config error.

## Phase B — migration consumers

Публиковать:

```text
schema/report-v1.schema.json
schema/report-v2.schema.json
examples/openai-v1.*
examples/openai-v2.*
examples/anthropic-v2.*
examples/dual-v2.*
```

Документировать JSON Pointer mapping.

## Phase C — v2 default

После объявленного цикла:

- `auto` всегда v2;
- explicit v1 ещё один цикл;
- singular legacy fields удаляются только в следующем major.

---

## 25. План реализации по PR

## PR 1 — Multi-target foundation и report v2

Включает:

- `TargetConfig`;
- env target activation;
- profile registry;
- `aiqa.report.v2`;
- target-qualified subjects;
- root verdict;
- Markdown target table;
- schema selection;
- пока без Anthropic network probes.

Gate:

- exact dual env парсится;
- combined mock report serializes;
- OpenAI regression green.

## PR 2 — Native Anthropic core

Включает:

- Anthropic client;
- base URL normalization;
- headers/version/auth;
- Models list/retrieve;
- basic Messages;
- schema/usage;
- errors;
- named-event streaming;
- stable profile core;
- dual mock.

Gate:

- Anthropic core PASS на mock;
- malformed streams deterministic;
- exact user command запускает обе native цели.

## PR 3 — Tools, token counting, structured output, vision

Включает:

- tool-use и continuation;
- streamed tool JSON;
- count_tokens;
- structured output;
- vision fixture;
- capability mapping;
- typed signals;
- guidance catalog.

Gate:

- optional modules PASS на mock;
- suitable rejections → `UNSUP`;
- guidance target/protocol-specific.

## PR 4 — Official Anthropic SDK checker

Включает:

- separate Go module/image;
- pinned SDK;
- JSONL output;
- checker registry;
- mappings;
- artifact dedupe;
- Docker tests.

Gate:

- SDK checker PASS на valid mock;
- ambient env не меняет target;
- conflicts отображаются без override native evidence.

## PR 5 — Thinking, maintenance, docs, live CI

Включает:

- adaptive/manual opt-in;
- high-token authorization;
- source-aware upstream checks;
- README/architecture/reports/adding-tests;
- live CI;
- migration notes.

Gate:

- thinking content redacted;
- stable core без beta;
- exact usage и cost warnings documented.

---

## 26. Карта изменений по файлам

| Path | Изменение |
|---|---|
| `internal/config/config.go` | target pairs, protocol flags, report schema |
| `cmd/aiqa/main.go` | OpenAI + Anthropic help |
| `internal/profile/profile.go` | registry, protocol, API version, sources |
| `profile/anthropic-messages-compat-2026-09.json` | новый profile |
| `internal/transport/*` | shared HTTP/pacing/telemetry |
| `internal/protocol/openai/*` | вынести OpenAI-specific logic |
| `internal/protocol/anthropic/*` | новый protocol client |
| `internal/capability/*` | qualified capabilities, target routing |
| `internal/suites/anthropic_*` | Anthropic probes |
| `internal/external/*` | checker registry + JSONL |
| `tools/anthropic-sdk-checker/*` | official SDK checker |
| `internal/result/types.go` | TargetReport, SubjectRef, neutral Usage |
| `internal/assessment/assessment.go` | per-target assessment |
| `internal/guidance/*` | Anthropic signals/findings |
| `guidance/catalog/anthropic-messages.json` | recommendations |
| `internal/report/markdown.go` | multi-target report |
| `internal/report/write.go` | v1/v2 |
| `schema/report-v2.schema.json` | report contract |
| `schema/profile-v2.schema.json` | profile contract |
| `script/check-profile-upstream.go` | generalized source strategy |
| `docs/architecture.md` | multi-protocol design |
| `docs/reports.md` | v2 semantics |
| `docs/adding-tests.md` | protocol/profile/checker extension |
| `README.md` | exact command |
| `tasks/backlog.md` | model-param и extra Anthropic surfaces |

---

## 27. Acceptance criteria

Реализация считается завершённой, когда:

1. Исходная dual-target команда работает без дополнительных flags на loopback HTTP.
2. OpenAI и Anthropic активируются независимо по base URLs.
3. Missing key у активного target даёт exit `2`.
4. Создаются один Markdown и один JSON.
5. Combined JSON имеет `aiqa.report.v2`.
6. У каждой цели отдельные profile, discovery, models, requirements, summary, guidance и telemetry.
7. `/anthropic` превращается в `/anthropic/v1/...`.
8. Anthropic использует `x-api-key`, OpenAI — Bearer.
9. Stable Anthropic requests отправляют `anthropic-version: 2023-06-01`.
10. Ambient beta/custom/auth-token не влияет на probes.
11. Models schema проверяется по Anthropic fields.
12. Message проверяется как `type=message` + content blocks.
13. Anthropic usage сохраняется без OpenAI naming distortion.
14. Streaming использует named events без `[DONE]`.
15. Ping и unknown future events tolerated.
16. Illegal known ordering обнаруживается.
17. Mid-stream provider error → `ERROR`.
18. Tool use и tool_result roundtrip проверены.
19. Partial tool JSON собирается.
20. Count Tokens проверяется отдельно.
21. Structured output локально schema-validated.
22. Vision запускается только applicable и не сохраняет base64.
23. Thinking default off и требует explicit high-token flag.
24. Thinking text/signatures не persist.
25. Official SDK checker out-of-process.
26. Checker использует explicit base/key и no retries/defaults.
27. Checker output — JSONL.
28. External evidence mapped к canonical requirements.
29. Native/SDK conflict не переписывает native result.
30. IDs включают target.
31. Нет cross-protocol compatibility percentage.
32. Exit policy учитывает любой blocking target.
33. OpenAI-only backward compatibility сохранена на migration cycle.
34. Обе report schemas имеют validation и golden fixtures.
35. Ни один key не появляется в output.
36. `go test ./...`, race, profile/schema checks и clean builds green.
37. Live tests secret-gated и default off.
38. Документация явно ограничивает scope и не заявляет полную certification.

---

## 28. Риски

| Риск | Mitigation |
|---|---|
| Gateway поддерживает `/v1/messages`, но не `/v1/models` | `ANTHROPIC_MODEL`; отдельный messages-only profile в backlog; не брать OpenAI inventory |
| Capability metadata неполная | conservative routing, overrides, `unknown` |
| API развивается additive | tolerate unknown fields/events, pin version/SDK |
| SDK retries скрывают behavior | `WithMaxRetries(0)` |
| Нет публичного Anthropic OpenAPI repo | pin official SDK + API version docs |
| Combined report слишком большой | target summary, target sections, collapse passed/raw |
| Cross-protocol score вводит в заблуждение | никаких aggregated percentages |
| Thinking дорого | default off + explicit budget permission |
| Tool behavior stochastic | forced choice, strong prompts, evidence levels |
| Count-token слегка отличается | schema normative, consistency heuristic |
| Proxy silently ignores fields | canaries + semantic checks |
| External output parsing ломается | JSONL |
| Shared gateway couples rate limits | per-target pacers + global semaphore |
| Пользователь добавляет `/v1` в Anthropic URL | normalization + visible warning |
| Gateway предпочитает Bearer | stable profile всё равно проверяет standard `x-api-key`; иной mode — отдельный profile |

---

## 29. Принятые решения

### Anthropic — OpenAI module?

Нет. Separate target/protocol.

### Passes одного target улучшают score другого?

Нет.

### Наличие key автоматически запускает платный test?

Нет. Activation по base URL.

### Использовать Bearer, раз Bifrost его может принять?

Нет. Stable Anthropic profile проверяет `x-api-key`.

### `ANTHROPIC_AUTH_TOKEN` имеет приоритет?

Нет, не в этом профиле.

### Beta в core?

Нет.

### 529 — incompatibility?

Нет, `ERROR`.

### Unknown SSE event — failure?

Нет.

### Thinking modes test automatically?

Нет, capability-routed opt-in.

### Можно оставить report v1?

Нет для Anthropic/multi-target.

### SDK link в main binary?

Нет, process boundary сохраняется.

---

## 30. Follow-up backlog

1. Anthropic `test=model-param`:
   - `max_tokens`;
   - `temperature`;
   - `top_p`;
   - `top_k` только где applicable;
   - `stop_sequences`;
   - adaptive effort;
   - manual budget;
   - context boundary по `max_input_tokens` и count_tokens.
2. Prompt caching module.
3. PDF/document input.
4. Message Batches profile.
5. Files API profile.
6. Messages-only profile для gateway без Models.
7. Claude Code behavioral profile отдельно от API conformance.
8. Cross-facade consistency diagnostics, non-scored.
9. Targeted verification:
   ```text
   aiqa verify --target anthropic --requirement ...
   ```
10. HTML/SARIF для multi-target.

---

## 31. Основные источники

### AIQA

- Baseline: <https://github.com/ilyar/aiqa/commit/39dd330d235174c0500986b3628e8ebe85d4e409>
- Architecture: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/docs/architecture.md>
- Reports/guidance: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/docs/reports.md>
- OpenAI profile: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/profile/openai-compat-2026-09.json>
- App: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/internal/app/app.go>
- Config: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/internal/config/config.go>
- Current OpenAI client: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/internal/api/client.go>
- External checker: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/internal/external/runner.go>
- Dual Bifrost facade: <https://github.com/ilyar/aiqa/blob/39dd330d235174c0500986b3628e8ebe85d4e409/script/model-provider.sh>

### Anthropic

- Versioning: <https://platform.claude.com/docs/en/api/versioning>
- Create Message: <https://platform.claude.com/docs/en/api/messages/create>
- Count Tokens: <https://platform.claude.com/docs/en/api/messages/count_tokens>
- Models: <https://platform.claude.com/docs/en/api/http/models>
- Streaming: <https://platform.claude.com/docs/en/build-with-claude/streaming>
- Errors: <https://platform.claude.com/docs/en/api/errors>
- Tools: <https://platform.claude.com/docs/en/build-with-claude/tool-use/overview>
- Structured outputs: <https://platform.claude.com/docs/en/build-with-claude/structured-outputs>
- Thinking: <https://platform.claude.com/docs/en/build-with-claude/extended-thinking>
- Official Go SDK: <https://github.com/anthropics/anthropic-sdk-go>
- Go SDK v1.68.0: <https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0>

---

## 32. Definition of Done

Работа завершена, когда exact dual-target команда создаёт воспроизводимый v2 report, где:

- OpenAI оценивается только OpenAI profile;
- Anthropic оценивается только Anthropic Messages profile;
- оба официальных SDK дают независимое interoperability evidence;
- у каждой цели свой authoritative verdict и actionable guidance;
- cross-protocol score не может скрыть failure;
- non-applicable model capability не становится defect;
- provider/runtime error не становится ложным unsupported;
- report ясно говорит, что проверено, что не проверено, почему возникла проблема, кто вероятный владелец и как проверить исправление.
