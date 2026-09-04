# Historical capability-routing research

> This document records the pre-v3 investigation that led to capability routing. Its descriptions of then-current code are historical, not operating instructions. For the implemented architecture, use [`docs/architecture.md`](../docs/architecture.md) and [`docs/extension-suites.md`](../docs/extension-suites.md).

# Вердикт

Диагноз верный: нынешний `openai-compat-2026-08` – это **профиль OpenAI Chat Completions**, а не профиль всей OpenAI API. Он содержит только discovery, `GET /models/{id}` и текстовые `POST /chat/completions` probes; ни image generation, ни embeddings, ни audio, ни Responses API в нём нет.

Проблема возникает в orchestration:

* `selectModels()` выбирает **все** модели из `/models`;
* для каждой безусловно вызывается `RunModelCompat()`;
* `model-param` тоже безусловно запускается для каждой модели;
* внешний checker получает один и тот же жёстко заданный список chat suites независимо от типа модели.

То есть image-модель сейчас не «проваливает OpenAI compatibility» – AIQA задаёт ей экзамен по санскриту, хотя она пришла рисовать банан.

Есть, однако, важное уточнение к предложенной классификации:

> `is_image: true → image_generation` – хороший provider-specific сигнал.
> `is_image: false → chat` – **небезопасное общее правило**.

`false` может также обозначать embeddings, ASR, TTS, video, realtime или иной тип. Для Vibecode это можно трактовать через отдельный adapter, потому что metadata и pricing действительно отчётливо разделяют token-модели и image-модели. Но в общем классификаторе отсутствие image capability не доказывает наличие chat capability.

Кроме того, официальный объект OpenAI `Model` содержит только базовые поля вроде `id`, `created`, `object`, `owned_by` и не публикует стандартного списка capabilities. Следовательно, универсально и безошибочно классифицировать модели только по стандартному `/models` невозможно – состояние `unknown` необходимо. ([OpenAI Platform][1])

---

# Рекомендуемое решение

Нужен не одиночный `model family`, а **набор endpoint-oriented capabilities**.

Одна модель может одновременно:

* отвечать текстом;
* принимать изображение;
* поддерживать Chat Completions;
* поддерживать Responses;
* не поддерживать `/images/generations`.

Поэтому структура:

```text
chat
image_generation
audio
embeddings
unknown
```

хороша как первый набросок, но как data model слишком груба.

Я бы ввела такие capability IDs:

```text
chat.completions.text
chat.completions.image_input
chat.completions.audio_input

responses.text
responses.image_input
responses.audio_input

images.generations
images.edits
images.variations

embeddings

audio.transcriptions
audio.speech
audio.translations

videos.generations

realtime
```

Это позволяет, например, классифицировать vision-модель так:

```json
[
  "chat.completions.text",
  "chat.completions.image_input"
]
```

а не ошибочно отправлять её в `images.generations`.

---

# 1. Разделить applicability и test result

Это два разных вопроса:

1. **Applicability:** следует ли вообще запускать этот module для модели?
2. **Result:** что произошло, когда применимый тест был запущен?

Предлагаемая семантика:

| Состояние        | Значение                                                                              |
| ---------------- | ------------------------------------------------------------------------------------- |
| `applicable`     | Module соответствует заявленной или выведенной capability модели                      |
| `not_applicable` | Module не относится к этой модели                                                     |
| `unknown`        | AIQA не смог надёжно определить применимость                                          |
| `executed`       | Применимый module был запущен                                                         |
| `skipped`        | Применимый module не запускался из-за policy, стоимости или пользовательского фильтра |

А существующие probe-status остаются:

| Probe status  | Значение                                                     |
| ------------- | ------------------------------------------------------------ |
| `pass`        | Поведение подтверждено                                       |
| `fail`        | Применимое требование нарушено                               |
| `unsupported` | Применимая optional capability явно отклонена подходящим 4xx |
| `skip`        | Проверка не исполнена                                        |
| `error`       | Инфраструктурная или provider-ошибка, результат неопределён  |
| `warn`        | Запрос принят, но семантика недостаточно доказана            |

## Почему `not_applicable` лучше не добавлять прямо в `TestResult.Status`

В текущей модели `Status` используется для отдельных probes, а `Summary` автоматически подсчитывает эти статусы и строит score. Прямое добавление `not_applicable` потребует менять enum, JSON Schema, Markdown и агрегацию и легко приведёт к очередной каше в denominator.

Лучше представить N/A на уровне module:

```go
type Applicability string

const (
    ApplicabilityApplicable    Applicability = "applicable"
    ApplicabilityNotApplicable Applicability = "not_applicable"
    ApplicabilityUnknown       Applicability = "unknown"
)

type ExecutionState string

const (
    ExecutionExecuted ExecutionState = "executed"
    ExecutionSkipped  ExecutionState = "skipped"
)

type ModuleAssessment struct {
    ID            string            `json:"id"`
    Capability    string            `json:"capability"`
    Applicability Applicability     `json:"applicability"`
    Execution     ExecutionState    `json:"execution,omitempty"`
    Reason        string            `json:"reason"`
    Source        string            `json:"source,omitempty"`
    Confidence    string            `json:"confidence,omitempty"`
    TestIDs       []string          `json:"test_ids,omitempty"`
    Summary       *result.Summary   `json:"summary,omitempty"`
}
```

Неприменимые probes вообще не создаются. В Markdown module отображается как `N/A`.

---

# 2. Добавить capability classifier

Новый package:

```text
internal/capability
├── classifier.go
├── types.go
├── generic_rules.go
├── overrides.go
└── adapters/
    ├── vibecode.go
    └── dashscope.go
```

## Порядок источников истины

| Приоритет | Источник                          | Пример                                         |
| --------: | --------------------------------- | ---------------------------------------------- |
|         1 | Явный CLI/config override         | `gpt-image-2=images.generations`               |
|         2 | Структурированное vendor metadata | `is_image`, `capabilities`, `output_modalities` |
|         3 | Provider adapter                  | интерпретация `is_image` именно у Vibecode     |
|         4 | Консервативные name rules         | `embedding`, `tts`, `asr`, `image-edit`        |
|         5 | Default policy                    | text fallback или `unknown` при конфликте      |

Результат классификации должен содержать evidence:

```json
{
  "capability": "images.generations",
  "applicability": "applicable",
  "source": "vendor_metadata",
  "rule": "vibecode.is_image.true",
  "confidence": "high",
  "reason": "metadata is_image=true"
}
```

## Generic metadata

Можно понимать распространённые расширения:

```text
capabilities
input_modalities
output_modalities
modalities
type
model_type
task
pipeline_tag
```

Но это всё vendor extensions, а не часть стандартного OpenAI `Model`. Одно лишь `input_modalities=["text"]` не доказывает chat capability: такой input используют и embedding-модели. Поэтому generic classifier использует endpoint/task/capability и output metadata, а input metadata сохраняет только как дополнительный контекст.

## Name rules

Name rules должны давать только положительные и достаточно узкие выводы:

```text
embedding                 → embeddings
asr | transcription      → audio.transcriptions
tts | speech             → audio.speech
image-edit               → images.edits
image | dall-e | flux    → images.generations
video | sora             → videos.generations
```

Осторожно с:

```text
vision
vl
omni
multimodal
```

Они чаще означают **image input в chat/responses**, а не image generation.

И главное:

```text
не содержит "image" ≠ chat model
```

## Conflicts

Если metadata и имя противоречат друг другу:

```json
{
  "applicability": "unknown",
  "confidence": "low",
  "reason": "classification conflict",
  "evidence": [
    "metadata is_image=true",
    "model id contains text-embedding"
  ]
}
```

AIQA не должна выбирать одну карму случайным броском монеты.

---

# 3. Capability routing

Вместо текущего:

```go
for _, model := range selected {
    RunModelCompat(model)
    RunModelParameters(model)
    external.Run(model)
}
```

нужен execution plan:

```go
inventory := discoverModels()
classifications := classifier.Classify(inventory, overrides)
plan := router.Build(profile.Modules, classifications, config)

for _, modelPlan := range plan.Models {
    for _, modulePlan := range modelPlan.Modules {
        switch modulePlan.Applicability {
        case NotApplicable:
            recordNA(modulePlan)

        case Unknown:
            recordUnclassified(modulePlan)

        case Applicable:
            if !modulePlan.Enabled {
                recordPolicySkip(modulePlan)
                continue
            }

            runNativeModule(modulePlan)
            runExternalModule(modulePlan)
        }
    }
}
```

## Важная правка provider-level probes

Сейчас:

```go
RunEndpointCompat(ctx, env, selected[0])
```

использует первую выбранную модель как `validModel`. Если первой окажется image/embedding/audio model, даже endpoint-level error probes становятся методологически неправильными.

Нужно выбирать representative model отдельно для каждого module:

```text
chat module       → первая applicable chat-модель
images module     → первая applicable image-generation-модель
embeddings module → первая applicable embedding-модель
audio module      → соответствующая audio-модель
```

И разнести ошибки по module:

```text
chat.error.invalid_model
chat.error.invalid_json

images.error.invalid_model
images.error.invalid_json

embeddings.error.invalid_model
embeddings.error.invalid_json
```

`error.invalid_model` не является абстрактным свойством вселенной – он всегда проверяет конкретный endpoint.

---

# 4. Новый modular profile

Текущий профиль нужно оставить неизменным для воспроизводимости старых отчётов и добавить новую ревизию:

```text
profile/
├── openai-compat-2026-08.json
└── openai-compat-2026-09.json
```

Новая структура:

```json
{
  "id": "openai-compat",
  "revision": "2026-09-01",
  "source": "https://github.com/openai/openai-openapi",
  "source_commit": "...",
  "modules": [
    {
      "id": "discovery",
      "subject": "provider",
      "always_applicable": true,
      "default_enabled": true,
      "cost_class": "none",
      "probes": []
    },
    {
      "id": "chat-completions",
      "subject": "model",
      "capabilities": [
        "chat.completions.text"
      ],
      "default_enabled": true,
      "cost_class": "token",
      "probes": []
    },
    {
      "id": "images-generations",
      "subject": "model",
      "capabilities": [
        "images.generations"
      ],
      "default_enabled": false,
      "cost_class": "billable-artifact",
      "probes": []
    },
    {
      "id": "embeddings",
      "subject": "model",
      "capabilities": [
        "embeddings"
      ],
      "default_enabled": true,
      "cost_class": "token",
      "probes": []
    },
    {
      "id": "audio-transcriptions",
      "subject": "model",
      "capabilities": [
        "audio.transcriptions"
      ],
      "default_enabled": false,
      "cost_class": "billable-media",
      "probes": []
    }
  ]
}
```

Поля `required` теперь означают:

> Requirement обязателен **внутри применимого module**.

Required image probe не должен учитываться для chat-модели.

---

# 5. Image generation suite

Официальная OpenAI API использует:

```text
POST /images/generations
```

а результат содержит `data[]`, где элемент может иметь `b64_json` или `url`. Для GPT Image models обычно возвращается base64, для некоторых DALL-E вариантов может использоваться URL. ([OpenAI Platform][2])

## Минимальный набор probes

| Probe                           | Required | Проверка                                                          |
| ------------------------------- | -------: | ----------------------------------------------------------------- |
| `images.generate.basic`         |       да | `{model, prompt}` принят и вернул 2xx JSON                        |
| `images.generate.schema`        |       да | `data` – непустой массив                                          |
| `images.generate.artifact`      |       да | Есть `b64_json` или `url`, данные являются настоящим изображением |
| `images.generate.invalid_model` |       да | Неизвестная модель даёт OpenAI-shaped client error                |
| `images.generate.invalid_json`  |       да | Сломанный JSON не приводит к 5xx                                  |
| `images.generate.usage`         |      нет | Проверить usage, если он присутствует                             |
| `images.generate.output_format` |      нет | Реальный PNG/JPEG/WebP соответствует заявленному формату          |
| `images.generate.size`          |      нет | Наблюдаемые размеры соответствуют запросу                         |
| `images.generate.quality`       |      нет | Параметр принят; его семантический эффект не объявлять доказанным |

Базовый запрос лучше делать только с обязательными полями:

```json
{
  "model": "gpt-image-2",
  "prompt": "A single blue square centered on a plain white background."
}
```

Не надо сразу добавлять `size`, `quality`, `background`, `n`: provider может поддерживать основной OpenAI contract, но не конкретный optional variant.

## Artifact validation

В JSON-отчёт нельзя помещать полный base64 или подписанный URL.

Добавить:

```go
type ArtifactObservation struct {
    Kind      string `json:"kind"`
    Source    string `json:"source"` // b64_json | url
    MediaType string `json:"media_type"`
    Format    string `json:"format"`
    Bytes     int64  `json:"bytes"`
    SHA256    string `json:"sha256"`
    Width     int    `json:"width"`
    Height    int    `json:"height"`
}
```

Для `b64_json`:

1. strict base64 decode;
2. ограничение decoded size;
3. magic bytes;
4. MIME;
5. width/height;
6. SHA-256.

Для URL:

1. только HTTP(S), предпочтительно HTTPS;
2. ограничение redirects;
3. блокировка localhost/private/link-local адресов;
4. ограничение размера;
5. проверка MIME и magic bytes;
6. не сохранять query string подписанного URL.

Markdown:

```text
PNG, 1024×1024, 1.8 MiB, sha256:7c0f…
```

## Stdlib policy

Архитектура AIQA намеренно сохраняет нативное ядро на стандартной библиотеке Go.

Поэтому:

* PNG/JPEG/GIF можно проверять через `image.DecodeConfig`;
* WebP – небольшим собственным RIFF/VP8/VP8L/VP8X header parser;
* необязательно добавлять тяжёлую image dependency только ради dimensions.

---

# 6. Image input – отдельный module

Нельзя смешивать:

```text
images.generations
```

и:

```text
chat.completions.image_input
```

Это разные API-контракты.

Для vision input нужен отдельный probe:

```text
chat.content_parts.image_url
```

с маленьким встроенным PNG data URL:

```json
{
  "model": "...",
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "Identify the dominant color."
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "data:image/png;base64,..."
          }
        }
      ]
    }
  ]
}
```

У него два уровня:

* transport/shape – image part принят;
* semantic – модель действительно интерпретировала изображение.

Semantic failure здесь разумнее давать как `WARN`, если транспортный контракт выполнен, но ответ недостаточно однозначен.

То же отдельно потребуется для:

```text
responses.image_input
```

Responses API в современной OpenAI API является самостоятельной поверхностью и не должна считаться синонимом Chat Completions. ([OpenAI Platform][2])

---

# 7. Внешний official-SDK checker

The removed pre-v3 external runner hard-coded only these suites:

```text
models
models_get
chat_completions
chat_completions_stream
chat_completions_stream_usage
chat_completions_json
chat_completions_tools
chat_completions_tools_stream
chat_completions_multi_turn
error_responses
```

и они запускаются для каждой модели.

При этом используемый checker уже имеет suites для images, embeddings, vision, Responses и audio, а также отдельные model environment variables.

Нужно заменить константу на mapping:

```go
var externalModules = map[string]ExternalModule{
    "discovery": {
        Suites: []string{"models"},
    },

    "chat-completions": {
        ModelEnv: "OPENAI_MODEL",
        Suites: []string{
            "chat_completions",
            "chat_completions_stream",
            "chat_completions_stream_usage",
            "chat_completions_json",
            "chat_completions_tools",
            "chat_completions_tools_stream",
            "chat_completions_multi_turn",
        },
    },

    "chat-image-input": {
        ModelEnv: "OPENAI_VISION_MODEL",
        Suites: []string{
            "chat_completions_vision",
        },
    },

    "images-generations": {
        ModelEnv: "OPENAI_IMAGE_MODEL",
        Suites: []string{
            "images_generations",
        },
    },

    "embeddings": {
        ModelEnv: "OPENAI_EMBEDDING_MODEL",
        Suites: []string{
            "embeddings",
            "embeddings_batch",
        },
    },

    "responses": {
        ModelEnv: "OPENAI_RESPONSES_MODEL",
        Suites: []string{
            "responses",
            "responses_stream",
            "responses_json",
            "responses_tools",
        },
    },

    "audio-speech": {
        ModelEnv: "OPENAI_TTS_MODEL",
        Suites: []string{
            "audio_speech",
        },
    },

    "audio-transcriptions": {
        ModelEnv: "OPENAI_WHISPER_MODEL",
        Suites: []string{
            "audio_transcriptions",
        },
    },
}
```

`models` нужно запускать один раз на provider, а не повторять для каждой модели.

---

# 8. Что делать с billable suites

Автоматически создать по картинке на каждой image-модели – хороший способ получить идеальный отчёт и неидеальный счёт от провайдера.

В документации AIQA уже записано:

> Suites, создающие billable assets, должны быть opt-in.

Поэтому правильная семантика по умолчанию:

```text
image model обнаружена
images module applicable
images module SKIP: billable module not enabled
chat module N/A
```

Запуск:

```bash
aiqa test=compat \
  --module=images-generations \
  --allow-billable-artifacts \
  --model=gpt-image-2
```

Для полного каталога:

```bash
aiqa test=compat \
  --module=auto,images-generations \
  --allow-billable-artifacts \
  --max-artifacts-per-model=1
```

`SKIP` здесь правильный: module применим, но не исполнен из-за policy.

`N/A` был бы неправильным: image generation как раз применима.

---

# 9. CLI overrides

Минимально необходимые параметры:

```text
--module
--model-capability
--capability-file
--unknown-model
--allow-billable-artifacts
```

Примеры:

```bash
aiqa test=compat \
  --model=gpt-image-2 \
  --model-capability='gpt-image-2=images.generations' \
  --module=images-generations \
  --allow-billable-artifacts
```

```bash
aiqa test=compat \
  --model=custom-llm \
  --model-capability='custom-llm=chat.completions.text'
```

```bash
aiqa test=compat \
  --capability-file=aiqa-capabilities.json
```

Файл:

```json
{
  "models": {
    "gpt-image-2": [
      "images.generations"
    ],
    "qwen3.7-text-embedding": [
      "embeddings"
    ],
    "deepseek-v4-flash-vision-exp": [
      "chat.completions.text",
      "chat.completions.image_input"
    ]
  }
}
```

Default policy:

```text
--unknown-model=inventory
```

Strict inventory override:

```text
--unknown-model=inventory
```

Модель без capability evidence по умолчанию считается текстовой с низкой уверенностью. Явные признаки non-text capability блокируют этот fallback, а конфликтующие признаки оставляют классификацию `unknown`. Low-confidence fallback должен быть явно отмечен в отчёте как assumption.

---

# 10. Отчёт

## Markdown

```markdown
## Model inventory and routing

| Model | Detected capabilities | Source | Confidence | Chat | Images | Embeddings | Evaluation |
|---|---|---|---:|---:|---:|---:|---|
| `gpt-5.4-mini` | `chat.completions.text` | Vibecode adapter | high | EXECUTED | N/A | N/A | PARTIAL |
| `gpt-image-2` | `images.generations` | `is_image=true` | high | N/A | SKIP | N/A | NOT EVALUATED |
| `unknown-model` | unknown | insufficient metadata | low | UNKNOWN | UNKNOWN | UNKNOWN | INVENTORY ONLY |
```

У model с одной только discovery-проверкой не должно быть `PASS`. Правильный model-level verdict:

```text
NOT-EVALUATED
```

или:

```text
INVENTORY-ONLY
```

Иначе «мы не проверяли модель» превращается в «модель совместима», а это уже не QA, а ведическая бухгалтерия.

## JSON

```json
{
  "id": "gpt-image-2",
  "metadata": {
    "is_image": true
  },
  "capabilities": [
    {
      "id": "images.generations",
      "applicability": "applicable",
      "source": "vendor_metadata",
      "rule": "vibecode.is_image.true",
      "confidence": "high"
    }
  ],
  "modules": [
    {
      "id": "chat-completions",
      "capability": "chat.completions.text",
      "applicability": "not_applicable",
      "reason": "classified as image-generation-only by Vibecode adapter"
    },
    {
      "id": "images-generations",
      "capability": "images.generations",
      "applicability": "applicable",
      "execution": "skipped",
      "reason": "billable artifact module was not enabled"
    }
  ],
  "assessment": {
    "verdict": "not-evaluated"
  }
}
```

N/A modules не входят в score.

Skipped applicable modules уменьшают evidence coverage, но не становятся failure.

---

# 11. `model-param` тоже надо маршрутизировать

Сейчас `RunModelParameters()` также безусловно запускается для каждой выбранной модели.

Для image/embedding/audio моделей это столь же неверно, как chat-suite.

Нужно сделать parameter modules:

```text
chat.parameters
images.parameters
embeddings.parameters
audio.speech.parameters
audio.transcription.parameters
```

Примеры:

| Module        | Параметры                                                                  |
| ------------- | -------------------------------------------------------------------------- |
| chat          | `temperature`, `top_p`, `reasoning_effort`, `max_completion_tokens`, tools |
| images        | `size`, `quality`, `background`, `output_format`, `n`                      |
| embeddings    | `dimensions`, `encoding_format`, array input                               |
| speech        | `voice`, `response_format`, `speed`, `instructions`                        |
| transcription | `response_format`, `language`, `prompt`, timestamps                        |

OpenAI API действительно использует отдельные endpoints и разные contracts для embeddings, transcription и speech. ([OpenAI Platform][3])

До реализации этих modules:

```text
model-param для non-chat модели → N/A
```

а не chat requests и не `unsupported`.

---

# 12. Конкретные изменения по файлам

| Файл/package                         | Изменение                                              |
| ------------------------------------ | ------------------------------------------------------ |
| `internal/capability/*`              | Новый classifier, overrides, adapters                  |
| `internal/profile/profile.go`        | Добавить modules, capabilities, cost class             |
| `profile/openai-compat-2026-09.json` | Новый modular profile                                  |
| `internal/result/types.go`           | Capability assessments и module assessments            |
| `internal/app/app.go`                | Построение execution plan вместо запуска всего на всех |
| `internal/suites/compat.go`          | Разделить на `discovery.go`, `chat.go`                 |
| `internal/suites/images.go`          | Native image generation probes                         |
| `internal/api/client.go`             | `ImagesGenerate`, безопасное получение URL artifact    |
| `internal/extsuite/`                 | Profile-bound built-in and executable extension suites |
| `internal/report/summary.go`         | Aggregation только применимых исполненных requirements |
| `internal/report/markdown.go`        | Capability routing matrix и N/A                        |
| `schema/report-v3.schema.json`       | Capability, module, and grouped extension fields       |
| `internal/app/app_test.go`           | Mixed catalog mock: chat + image + embedding + unknown |

---

# 13. Разбивка на PR

## PR 1 – capability routing, без image generation

Цель: немедленно убрать ложные chat failures.

* capability model;
* `applicable/not_applicable/unknown`;
* Vibecode adapter для `is_image`;
* name rules;
* text-by-default для моделей без evidence, с явным inventory-only override;
* chat и `model-param` запускаются только для chat candidates;
* external checker запускает только chat suites для chat models;
* routing matrix в отчёте.

Результат для `gpt-image-2`:

```text
Chat Completions: N/A
Images Generations: SKIP – suite not implemented/enabled
Model assessment: NOT-EVALUATED
```

## PR 2 – native images generation module

* `/images/generations`;
* schema validation;
* b64/URL artifact validation;
* SHA-256, MIME, dimensions;
* redaction base64 и signed URLs;
* optional official-SDK `images_generations`;
* billable opt-in.

## PR 3 – embeddings, vision input и Responses

* embeddings;
* `chat.content_parts.image_url`;
* Responses text/image input;
* module-specific external checks.

## PR 4 – audio и video

* transcription;
* speech;
* realtime отдельно;
* video как asynchronous/billable suite.

---

# Acceptance criteria

1. Все записи `/models` остаются в inventory.
2. `gpt-image-2` никогда не получает `POST /chat/completions`, если нет явного chat capability.
3. `chat module` для image-only модели имеет `N/A`, а не `N/S`.
4. `images module`, отключённый из-за стоимости, имеет `SKIP`, а не `N/A`.
5. Unknown model не получает автоматический `PASS`.
6. Explicit 4xx для применимого optional feature остаётся `unsupported`.
7. Timeout/429/5xx остаётся `error`, а не capability inference.
8. Одна модель может иметь несколько capabilities.
9. External checker получает только применимые suites.
10. Base64, binary content и signed URL query не попадают в отчёт.
11. N/A исключён из score.
12. Добавление нового module не меняет оценки неприменимых моделей.
13. `model-param` не запускает chat parameters для image/audio/embedding models.
14. Provider-level discovery failures не смешиваются с model capability failures.

И отдельная тонкость: исправление routing не отменяет настоящий provider-level дефект Vibecode – в приложенном отчёте `/models` не содержит обязательного `created` во всех 30 entries. Это самостоятельный inventory conformance failure и после capability routing должно остаться в отчёте.

Итоговая конструкция:

```text
Inventory
    ↓
Capability classification
    ↓
Applicability plan
    ↓
Module-specific native probes
    ↓
Module-specific official-SDK probes
    ↓
Per-module verdict
    ↓
Provider aggregation over applicable modules
```

Именно это я считаю правильным направлением для AIQA: **не определять “тип модели”, а строить воспроизводимый capability contract и запускать только соответствующие ему проверки**.

[1]: https://platform.openai.com/docs/api-reference/models/list "https://platform.openai.com/docs/api-reference/models/list"
[2]: https://platform.openai.com/docs/api-reference/images/create "https://platform.openai.com/docs/api-reference/images/create"
[3]: https://platform.openai.com/docs/api-reference/embeddings/create "https://platform.openai.com/docs/api-reference/embeddings/create"
