# PRD: Google GenAI compatibility target and profiles for AIQA

**Status:** Proposed  
**Document version:** 1.0  
**Date:** 2026-09-02  
**Target repository:** [ilyar/aiqa](https://github.com/ilyar/aiqa)  
**Repository baseline reviewed:** [`66cb14fa484e38ec4405383b613c37731ed1e219`](https://github.com/ilyar/aiqa/commit/66cb14fa484e38ec4405383b613c37731ed1e219)  
**Primary implementation language:** Go  
**Target report contract:** `aiqa.report.v3`  
**Target profile contract:** `aiqa.profile.v3`  
**Primary Google backend in scope:** Gemini Developer API  
**Explicitly out of scope for this PRD:** Vertex AI compatibility certification

---

## 1. Executive summary

AIQA already has a sound multi-target architecture for testing OpenAI-compatible and Anthropic-compatible APIs:

- each protocol is a separate target;
- each target has its own versioned compatibility profile;
- native probes produce protocol-level evidence;
- official SDK checks are independent extension suites;
- raw probes are aggregated into canonical requirements;
- verdict, core conformance, optional capability coverage and evidence completeness remain distinct;
- deterministic guidance turns findings into prioritized remediation advice;
- `test=compat` and `test=ext-suite` are deliberately separate, so SDK checks never run silently.

Google GenAI must be added as a **third independent protocol target**, not as:

- an OpenAI-compatible model family;
- another OpenAI capability module;
- a vendor-specific extension of `/v1/chat/completions`;
- a combined “Google” target that silently mixes Gemini Developer API and Vertex AI.

The target identifier SHALL be:

```text
google-genai
```

The first implementation SHALL target the **Gemini Developer API** hosted by, or compatible with, the Google Generative Language REST surface.

Google currently has two legitimate generation surfaces:

1. **Interactions API** — Generally Available since June 2026, recommended by Google for new projects, and the surface where new models and capabilities are expected to launch.
2. **`generateContent` API** — now described as legacy, but explicitly still fully supported and presently exposed by the official Google Gen AI Go SDK.

A single undifferentiated Google score would hide an important distinction. A gateway may implement `generateContent` perfectly while not implementing Interactions at all. Conversely, a modern Interactions-compatible service may intentionally omit some legacy endpoints. Therefore AIQA SHALL ship two selectable Google profiles:

```text
google-genai-compat-2026-09.json
google-genai-generate-content-compat-2026-09.json
```

Their policies SHALL be:

| Profile | Core surface | Secondary surface | Intended use |
|---|---|---|---|
| `google-genai-compat-2026-09.json` | Interactions API + Models | `generateContent`, token counting and embeddings as optional modules | Current Google GenAI compatibility |
| `google-genai-generate-content-compat-2026-09.json` | Models + `generateContent` + legacy streaming | Interactions as optional/informative | Existing gateways and parity with the current Go SDK |

The default Google profile SHALL be the current profile:

```text
google-genai-compat-2026-09.json
```

The official Google Go SDK extension SHALL use:

```text
module: google.golang.org/genai
version: v1.71.0
commit: bfe8a871478bdec16932d7809e8f8bff122569d3
extension id: official-sdk.google-genai
```

At this reviewed version, the public Go SDK exposes Models, `generateContent`, streaming generation, token counting, embeddings, tools, structured outputs and multimodal input. Its public client does **not** expose the Interactions API. AIQA SHALL report that as an SDK surface-coverage limitation, not as a provider failure and not as evidence that the tested endpoint lacks Interactions support.

The full intended invocation is:

```bash
OPENAI_BASE_URL="https://api.openai.com/v1" \
OPENAI_API_KEY="$OPENAI_API_KEY_QA" \
ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic" \
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY_QA" \
GOOGLE_GENAI_BASE_URL="http://127.0.0.1:8080/google" \
GOOGLE_GENAI_API_KEY="$GOOGLE_GENAI_API_KEY_QA" \
GOOGLE_GENAI_MODEL="gemini-3.8-flash" \
aiqa --verbose \
  test=compat,ext-suite \
  --ext-suite=official-sdk \
  --format=markdown,json \
  --name=compat-report
```

Expected artifacts:

```text
compat-report.md
compat-report.json
```

`test=compat` alone SHALL continue to run native probes only. Official SDK checks remain explicit through `test=ext-suite`.

---

## 2. Evidence basis

This PRD distinguishes three kinds of statements.

### 2.1 Repository-derived facts

The following are observations from the reviewed AIQA repository state:

- the current report contract is `aiqa.report.v3`;
- reports contain independent `targets[]`;
- OpenAI and Anthropic have independent profiles and target runners;
- target activation is environment/configuration driven;
- built-in SDK extensions exist for OpenAI and Anthropic;
- extension execution is explicit through `test=ext-suite`;
- extension results are bound to profile requirements rather than defining compatibility policy;
- the profile schema currently permits only `openai` and `anthropic`;
- the application protocol switch currently has only OpenAI and Anthropic branches;
- the built-in extension registry currently contains only OpenAI and Anthropic;
- `go.mod`, dependency checks, SBOM checks and upstream checks currently pin only those two SDKs;
- the current protocol-specific package exists for Anthropic and is the closest implementation template for Google.

Repository sources:

- [AIQA commit reviewed](https://github.com/ilyar/aiqa/commit/66cb14fa484e38ec4405383b613c37731ed1e219)
- [Current architecture](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/docs/architecture.md)
- [Extension suites](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/docs/extension-suites.md)
- [Anthropic profile](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/profile/anthropic-compat-2026-09.json)
- [Profile v3 schema](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/schema/profile-v3.schema.json)
- [Application orchestration](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/internal/app/app.go)
- [Anthropic protocol client](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/internal/protocol/anthropic/client.go)
- [Anthropic official SDK extension](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/internal/extsuite/builtin/anthropic/anthropic.go)
- [Makefile quality gates](https://github.com/ilyar/aiqa/blob/66cb14fa484e38ec4405383b613c37731ed1e219/Makefile)

### 2.2 Official Google facts

The following are taken from current official Google documentation and source repositories:

- Interactions is GA and recommended for new projects;
- `generateContent` is legacy but fully supported;
- Interactions is stateful by default because `store=true`;
- stateless behavior requires `store=false`;
- paid-tier stored interactions are retained for 55 days and free-tier interactions for one day unless otherwise configured;
- Gemini REST authentication uses `x-goog-api-key`;
- the Models API exposes `inputTokenLimit`, `outputTokenLimit`, `supportedGenerationMethods`, `thinking`, temperature and sampling metadata;
- legacy methods include `generateContent`, `streamGenerateContent`, `countTokens` and `embedContent`;
- Interactions streaming uses named SSE events and terminates with `event: done` / `data: [DONE]`;
- legacy `streamGenerateContent` is a different stream contract and must not be validated with the Interactions state machine;
- the official Google Gen AI Go SDK supports a custom base URL and API version;
- the Go SDK defaults the Gemini Developer API to `v1beta`;
- the Go SDK sends the API key as `x-goog-api-key`;
- the latest reviewed Go SDK release is `v1.71.0`;
- current Google Interactions documentation lists Python and JavaScript SDK availability and REST; the reviewed Go SDK source tree does not expose a public Interactions service.

Official sources:

- [Interactions overview](https://ai.google.dev/gemini-api/docs/interactions-overview)
- [Interactions getting started](https://ai.google.dev/gemini-api/docs/get-started)
- [Interactions streaming](https://ai.google.dev/gemini-api/docs/streaming)
- [Models REST reference](https://ai.google.dev/api/models)
- [`generateContent` REST reference](https://ai.google.dev/api/generate-content)
- [Token counting REST reference](https://ai.google.dev/api/tokens)
- [Embeddings REST reference](https://ai.google.dev/api/embeddings)
- [Gemini API key guidance](https://ai.google.dev/gemini-api/docs/api-key)
- [Google Gen AI Go SDK](https://github.com/googleapis/go-genai)
- [Go SDK release `v1.71.0`](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- [Generative Language protobuf definitions](https://github.com/googleapis/googleapis/tree/master/google/ai/generativelanguage/v1beta)

### 2.3 Product decisions introduced by this PRD

The following are AIQA design choices, not Google standards:

- protocol ID `google-genai`;
- canonical environment variable names;
- two-profile strategy;
- which requirements are core versus optional;
- default `store=false`;
- explicit opt-in for persistent interactions;
- representative-model selection rules;
- report presentation and finding codes;
- implementation milestones and acceptance criteria.

These decisions SHALL be documented in the profile and report so they cannot masquerade as divine revelation from Mountain View.

---

## 3. Problem statement

AIQA can currently evaluate:

```text
OpenAI target
Anthropic target
```

but cannot express or test a native Google GenAI contract.

Testing a Google-backed model only through an OpenAI compatibility facade answers a narrower question:

> Can this gateway make Google-backed inference look enough like OpenAI?

It does not answer:

> Does the gateway implement the Google GenAI protocol correctly?

Important Google semantics would remain invisible:

- `models/*` resource names;
- `supportedGenerationMethods`;
- Gemini content/parts representation;
- safety feedback and finish reasons;
- `usageMetadata`;
- thought-token accounting;
- `x-goog-api-key`;
- Interactions steps and storage semantics;
- Interactions SSE event ordering;
- function-call argument deltas;
- `countTokens`;
- `embedContent`;
- Google structured-output fields;
- Google-specific model capability routing.

Without a dedicated target, a gateway can pass an OpenAI profile while having a malformed or entirely absent Google-native facade.

---

## 4. Goals

The implementation SHALL:

1. Add `google-genai` as a first-class AIQA target.
2. Test Gemini Developer API-compatible endpoints independently from OpenAI and Anthropic.
3. Preserve one report containing any configured combination of:
   - OpenAI;
   - Anthropic;
   - Google GenAI.
4. Add a versioned current Google profile centered on Interactions.
5. Add a versioned legacy strict profile centered on `generateContent`.
6. Implement native REST probes for:
   - model discovery;
   - Interactions;
   - legacy generation;
   - both stream contracts;
   - tools;
   - structured output;
   - token counting;
   - embeddings;
   - image input;
   - thinking metadata and observable output where applicable.
7. Add a built-in extension using the official Google Gen AI Go SDK.
8. Bind SDK evidence through `aiqa.profile.v3` extension bindings.
9. Preserve the current conformance model:
   - hard-gated verdict;
   - core conformance;
   - optional coverage;
   - evidence completeness;
   - no cross-target percentage averaging.
10. Generate deterministic actionable guidance for Google findings.
11. Avoid persistent server-side state by default.
12. Avoid running billable artifact generation, server tools or long-running agents by default.
13. Keep API keys and media payloads out of reports.
14. Preserve current OpenAI and Anthropic behavior and JSON compatibility.
15. Make profile maintenance reproducible against pinned official sources.

---

## 5. Non-goals

The first implementation SHALL NOT:

- certify Vertex AI compatibility;
- authenticate with Google Cloud ADC, service accounts or workload identity;
- test project/location path construction;
- test Gemini Enterprise Agent Platform;
- test OpenAI-compatible Gemini endpoints;
- benchmark model intelligence or quality;
- judge answer truthfulness beyond deterministic marker tasks;
- test safety with harmful prompts;
- execute Google Search, Code Execution, Computer Use or remote MCP;
- run Deep Research or other long-running agents;
- create stored Interactions by default;
- upload files;
- create caches;
- create batches;
- create tuned models;
- generate images, audio or video by default;
- mutate or remediate gateway configuration;
- treat the absence of Go SDK support for Interactions as a provider defect;
- silently run official SDK suites during `test=compat`;
- infer that every model returned by `/models` supports every Google surface.

Vertex AI should later receive its own target, for example:

```text
google-vertex
```

because its authentication, resource names, project/location scoping, endpoints and operational failure modes differ materially.

---

## 6. User stories

### 6.1 Gateway maintainer

As a gateway maintainer, I can expose OpenAI, Anthropic and Google-native facades and receive one report with independent verdicts and remediation guidance for each.

### 6.2 Model provider operator

As an operator, I can see which Google model supports `generateContent`, embeddings, thinking or multimodal input and avoid false failures from inapplicable suites.

### 6.3 SDK consumer

As a Go developer, I can see whether the official Google Gen AI Go SDK can send and parse supported calls through the tested endpoint.

### 6.4 CI owner

As a CI owner, I can fail a pipeline only when a selected target violates its profile and can keep SDK execution explicit.

### 6.5 Security-conscious tester

As a tester, I can verify Interactions without causing server-side retention because AIQA sends `store=false` by default.

### 6.6 Compatibility researcher

As a researcher, I can compare current Interactions compatibility and legacy `generateContent` compatibility without pretending they are the same surface.

---

## 7. Terminology

| Term | Meaning |
|---|---|
| Target | One configured protocol endpoint with credentials and one selected profile |
| Protocol | `openai`, `anthropic`, or `google-genai` |
| Surface | A coherent API family such as Interactions or `generateContent` |
| Module | Profile grouping with applicability and execution policy |
| Probe | One concrete experiment |
| Requirement | Canonical compatibility claim supported by one or more probes |
| Native evidence | Raw HTTP behavior measured by AIQA |
| Extension evidence | Behavior observed through an official SDK or third-party checker |
| Core | Minimum profile requirement; failure blocks conformance |
| Optional | Additional capability; unsupported is a gap, not a core failure |
| N/A | Requirement does not apply to the model or surface |
| SKIP | Requirement applies but was not executed |
| ERROR | Infrastructure/provider/runner problem prevented a conclusion |
| Modern profile | Interactions-first Google profile |
| Legacy profile | `generateContent`-first Google profile |
| Gemini Developer API | API-key-authenticated `generativelanguage.googleapis.com` surface |
| Vertex AI | Google Cloud project/location-scoped surface, out of scope here |

---

## 8. Current AIQA architecture assessment

### 8.1 What is already suitable

The current repository already provides the essential foundation:

```text
config resolution
    ↓
independent target configurations
    ↓
per-target profile loading
    ↓
protocol-specific native runner
    ↓
extension planning and execution
    ↓
canonical requirements
    ↓
target assessment and guidance
    ↓
run summary
    ↓
Markdown + JSON
```

The following components should be reused:

- `result.ReportV3`;
- `result.TargetReport`;
- target-scoped configuration;
- profile v3 modules/probes/extension bindings;
- `internal/api` bounded HTTP and SSE transport;
- adaptive target pacing;
- `internal/probe` executor;
- `internal/assessment`;
- `internal/guidance`;
- `internal/extsuite`;
- explicit `test=ext-suite`;
- built-in extension registry;
- profile/schema/dependency/SBOM quality gates.

### 8.2 What must change

Current closed assumptions must be generalized:

| Area | Current state | Required change |
|---|---|---|
| Protocol enum | OpenAI, Anthropic | Add `google-genai` |
| Target env resolution | `OPENAI_*`, `ANTHROPIC_*` | Add Google target and aliases |
| Profile schema | Protocol enum has two values | Add Google and protocol-specific rules |
| Profile loader | Validation special-cases Anthropic | Introduce protocol validator registry |
| App orchestration | Switch has two branches | Register Google runner |
| Built-in SDK registry | OpenAI + Anthropic | Add Google |
| Aggregate selector | `official-sdk` expands to two extensions | Include Google when active |
| Dependencies | Two official SDK modules | Pin `google.golang.org/genai` |
| Makefile | Checks two SDKs/profiles | Add Google gates |
| Guidance packs | Generic/OpenAI-oriented | Add Google GenAI pack |
| Mock integration | OpenAI + Anthropic | Add deterministic Google facade |
| Documentation | Two targets | Document third target and surfaces |

### 8.3 Recommended refactor before adding a third protocol

The application currently contains a protocol switch. A third case is possible, but a fourth would turn orchestration into a ceremonial pile of `switch` branches.

Introduce a protocol runner registry:

```go
type ProtocolRunner interface {
    Protocol() config.Protocol
    Run(
        context.Context,
        RunContext,
        config.TargetConfig,
        profile.Profile,
    ) (result.TargetReport, error)
}
```

Registration:

```go
registry := protocol.NewRegistry(
    openai.NewRunner(),
    anthropic.NewRunner(),
    googlegenai.NewRunner(),
)
```

Profile-specific validation should follow the same pattern:

```go
type ProfileValidator interface {
    Protocol() string
    Validate(profile.Profile) error
}
```

This is not mandatory for a minimal patch, but it is required by this PRD because Google has two major surfaces and future Vertex support should not require another surgery on `internal/app`.

---

## 9. Target identity and activation

### 9.1 Protocol identifier

```text
google-genai
```

This identifier SHALL appear in:

- `config.Protocol`;
- `targets[].id`;
- `targets[].protocol`;
- profile `protocol`;
- extension manifests;
- subject references;
- report filters;
- guidance findings;
- target selectors.

Do not use only `google`, because that would falsely imply Vertex, Cloud APIs, Enterprise Agent Platform and perhaps the entire company cafeteria.

### 9.2 Canonical environment variables

```text
GOOGLE_GENAI_BASE_URL
GOOGLE_GENAI_API_KEY
GOOGLE_GENAI_API_VERSION
GOOGLE_GENAI_MODEL
GOOGLE_GENAI_PROFILE
```

Defaults:

```text
GOOGLE_GENAI_API_VERSION=v1beta
GOOGLE_GENAI_PROFILE=google-genai-compat-2026-09.json
```

There SHALL be no implicit default base URL in AIQA. Users testing Google directly must set:

```bash
export GOOGLE_GENAI_BASE_URL="https://generativelanguage.googleapis.com"
```

This prevents accidental target activation and unplanned billable calls merely because a generic SDK key happens to exist in the shell.

### 9.3 Accepted official aliases

For developer convenience, AIQA MAY accept:

```text
GOOGLE_GEMINI_BASE_URL
GOOGLE_API_KEY
GEMINI_API_KEY
```

Resolution order:

#### Base URL

1. `--google-genai-base-url`
2. `GOOGLE_GENAI_BASE_URL`
3. `GOOGLE_GEMINI_BASE_URL`

If canonical and alias base URLs are both set and normalize to different values, configuration SHALL fail with exit `2`.

#### API key

1. `--google-genai-api-key`
2. `GOOGLE_GENAI_API_KEY`
3. `GOOGLE_API_KEY`
4. `GEMINI_API_KEY`

Rules:

- an explicit/canonical key wins over aliases;
- if canonical is absent and both `GOOGLE_API_KEY` and `GEMINI_API_KEY` are set to different values, AIQA SHALL fail rather than silently select the wrong project;
- keys SHALL never be written to reports;
- aliases SHALL only be read after the Google target has been activated by a base URL or explicit `--target=google-genai`.

### 9.4 Target selection

Valid values:

```text
--target=auto
--target=openai
--target=anthropic
--target=google-genai
--target=openai,anthropic,google-genai
```

`auto` activates complete configured target pairs.

Activation matrix:

| Base URL | Key | Selected | Result |
|---|---|---|---|
| absent | absent | auto | Google target absent |
| present | present | auto | Google target active |
| present | absent | auto | configuration error |
| absent | present | auto | target remains inactive; generic key alone is not activation |
| present | present | explicit target | active |
| absent | any | explicit target | configuration error |

### 9.5 Model selection

Accepted sources, in order:

1. `--google-genai-model`;
2. `GOOGLE_GENAI_MODEL`;
3. discovered model routing.

Legacy `--model` SHALL remain valid only when exactly one target is active. With multiple targets it SHALL remain an ambiguity error.

Model IDs SHALL be normalized:

```text
models/gemini-3.8-flash  → gemini-3.8-flash
gemini-3.8-flash         → gemini-3.8-flash
```

Raw provider metadata and raw `name` SHALL still be preserved.

---

## 10. Base URL semantics

The Google base URL SHALL mean:

> Prefix immediately before the API version segment.

Example:

```text
GOOGLE_GENAI_BASE_URL=http://127.0.0.1:8080/google
GOOGLE_GENAI_API_VERSION=v1beta
```

Effective endpoints:

```text
http://127.0.0.1:8080/google/v1beta/models
http://127.0.0.1:8080/google/v1beta/models/gemini-3.8-flash
http://127.0.0.1:8080/google/v1beta/models/gemini-3.8-flash:generateContent
http://127.0.0.1:8080/google/v1beta/models/gemini-3.8-flash:streamGenerateContent?alt=sse
http://127.0.0.1:8080/google/v1beta/models/gemini-3.8-flash:countTokens
http://127.0.0.1:8080/google/v1beta/models/gemini-embedding-001:embedContent
http://127.0.0.1:8080/google/v1beta/interactions
http://127.0.0.1:8080/google/v1beta/interactions?alt=sse
```

Normalization SHALL:

- trim trailing `/`;
- accept but normalize an accidentally supplied trailing `/v1beta`;
- never append the API version twice;
- preserve an existing gateway path prefix;
- escape model path segments safely;
- preserve colon methods;
- merge query parameters without losing `alt=sse`;
- reject query strings or fragments in configured base URLs;
- use TLS outside loopback unless `--allow-http` is explicit.

Profile field:

```json
"base_url_semantics": "prefix_before_api_version"
```

---

## 11. Authentication contract

Native calls SHALL use:

```http
x-goog-api-key: <secret>
```

AIQA SHALL not use the `?key=` query parameter even though some Google examples show it, because:

- query strings are more likely to leak into reverse-proxy logs;
- URL snippets are frequently included in error reports;
- the official current API-key guide demonstrates the header;
- the official Go SDK also sends the header.

Required behavior:

- a valid key is accepted;
- invalid-key behavior is captured;
- missing-key behavior is captured;
- error body shape is validated without overfitting one exact HTTP status;
- `400`, `401` or `403` MAY be accepted for an authentication rejection if the response contains a coherent Google error object;
- `429`, `408`, timeout and `5xx` SHALL remain `ERROR`, not authentication incompatibility.

The following SHALL be redacted case-insensitively:

```text
x-goog-api-key
authorization
key query parameter
GOOGLE_GENAI_API_KEY
GOOGLE_API_KEY
GEMINI_API_KEY
```

---

## 12. Profile strategy

### 12.1 Why two profiles are required

Google’s current documentation establishes:

```text
Interactions = current recommended surface
generateContent = legacy but fully supported
```

A gateway can implement either surface to a high standard. The profile must state which claim it is making.

A combined profile with every surface marked core would make many legitimate implementations fail for policy reasons rather than protocol defects. A profile with everything optional would make “Google GenAI compatible” nearly meaningless.

Two profiles make the claim explicit.

### 12.2 Modern profile

Filename:

```text
profile/google-genai-compat-2026-09.json
```

Policy:

- Models discovery is core.
- Interactions text generation is core.
- Interactions streaming is core.
- `store=false` acceptance is core.
- Interactions error shape is core.
- Legacy `generateContent` is optional.
- CountTokens is optional.
- Embeddings is optional and applies only to embedding models.
- Tools, structured output, vision and thinking are optional.
- Stateful storage, agents and billable output are disabled by default.
- Official Go SDK requirements are optional because the SDK does not cover Interactions at the reviewed version.

### 12.3 Legacy strict profile

Filename:

```text
profile/google-genai-generate-content-compat-2026-09.json
```

Policy:

- Models discovery is core.
- `generateContent` text generation is core.
- legacy streaming is core.
- legacy usage and error shape are core.
- CountTokens is optional.
- Embeddings is optional.
- Interactions is optional/informative.
- Official Go SDK generation and stream requirements are optional interoperability requirements, not duplicate votes for native requirements.

### 12.4 Profile selection

```text
--google-genai-profile FILE
GOOGLE_GENAI_PROFILE
```

Default:

```text
google-genai-compat-2026-09.json
```

The Markdown header SHALL make the selected claim impossible to miss:

```text
Google GenAI profile: Current Interactions compatibility
```

or:

```text
Google GenAI profile: Legacy generateContent compatibility
```

### 12.5 Profile provenance

Proposed modern profile header:

```json
{
  "schema_version": "aiqa.profile.v3",
  "id": "google-genai-compat",
  "protocol": "google-genai",
  "revision": "2026-09-02",
  "api_version": "v1beta",
  "base_url_semantics": "prefix_before_api_version",
  "source": "https://github.com/googleapis/go-genai",
  "source_commit": "bfe8a871478bdec16932d7809e8f8bff122569d3",
  "source_ref_revision": "gemini-api:v1beta; interactions:GA-2026-06; sdk:v1.71.0",
  "scope": "GET /v1beta/models, GET /v1beta/models/{model}, POST /v1beta/interactions, POST /v1beta/models/{model}:generateContent, POST /v1beta/models/{model}:streamGenerateContent, POST /v1beta/models/{model}:countTokens, POST /v1beta/models/{model}:embedContent",
  "sources": [
    {
      "kind": "api_reference",
      "url": "https://ai.google.dev/api/models",
      "revision": "v1beta",
      "reviewed_at": "2026-09-02"
    },
    {
      "kind": "api_reference",
      "url": "https://ai.google.dev/api/generate-content",
      "revision": "v1beta",
      "reviewed_at": "2026-09-02"
    },
    {
      "kind": "api_reference",
      "url": "https://ai.google.dev/gemini-api/docs/interactions-overview",
      "revision": "GA",
      "reviewed_at": "2026-09-02"
    },
    {
      "kind": "official_sdk",
      "url": "https://github.com/googleapis/go-genai",
      "version": "v1.71.0",
      "commit": "bfe8a871478bdec16932d7809e8f8bff122569d3"
    },
    {
      "kind": "protobuf_contract",
      "url": "https://github.com/googleapis/googleapis/tree/master/google/ai/generativelanguage/v1beta",
      "reviewed_at": "2026-09-02"
    }
  ]
}
```

No source entry may imply that the Go SDK defines the Interactions contract.

---

## 13. Profile schema changes

`schema/profile-v3.schema.json` SHALL add:

```json
"protocol": {
  "enum": ["openai", "anthropic", "google-genai"]
}
```

Protocol-specific validation SHALL include:

```json
{
  "if": {
    "properties": {
      "protocol": {"const": "google-genai"}
    }
  },
  "then": {
    "required": ["api_version"],
    "properties": {
      "api_version": {"pattern": "^v[0-9]+(?:beta[0-9]*|alpha[0-9]*)?$"},
      "base_url_semantics": {"const": "prefix_before_api_version"}
    }
  }
}
```

Go validation SHALL additionally require:

- all Google module IDs begin with `google-genai.`;
- all Google probe IDs begin with `google.`;
- official binding IDs use `official-sdk.google-genai`;
- Interactions modules may not be marked `billable-artifact` merely because text tokens are billable;
- persistent-resource modules must use cost class `persistent-resource`;
- high-thinking modules use `high-token`;
- embedding modules use capability `google-genai.embed-content`;
- model modules declare at least one Google capability.

The current Anthropic-specific profile validation should be moved behind a validator registry instead of expanding a single conditional thicket.

---

## 14. Module catalog

### 14.1 Modern profile modules

```text
google-genai.discovery
google-genai.interactions
google-genai.interactions-stream
google-genai.interactions-tools
google-genai.interactions-structured-output
google-genai.interactions-vision
google-genai.interactions-thinking
google-genai.interactions-stateful
google-genai.generate-content
google-genai.generate-content-stream
google-genai.generate-content-tools
google-genai.generate-content-structured-output
google-genai.token-counting
google-genai.embeddings
google-genai.vision
google-genai.thinking
```

### 14.2 Module properties

| Module | Subject | Default | Cost class | Capability |
|---|---|---:|---|---|
| discovery | provider | yes | none | always |
| interactions | model | yes | token | `google-genai.interactions.text` |
| interactions-stream | model | yes | token | `google-genai.interactions.text` |
| interactions-tools | model | yes | token | `google-genai.interactions.tools` |
| interactions-structured-output | model | yes | token | `google-genai.interactions.structured-output` |
| interactions-vision | model | yes | token-media-input | `google-genai.interactions.image-input` |
| interactions-thinking | model | no | high-token | `google-genai.interactions.thinking` |
| interactions-stateful | model | no | persistent-resource | `google-genai.interactions.stateful` |
| generate-content | model | yes | token | `google-genai.generate-content.text` |
| generate-content-stream | model | yes | token | `google-genai.generate-content.text` |
| generate-content-tools | model | yes | token | `google-genai.generate-content.tools` |
| generate-content-structured-output | model | yes | token | `google-genai.generate-content.structured-output` |
| token-counting | model | yes | metadata | `google-genai.count-tokens` |
| embeddings | model | yes | token | `google-genai.embed-content` |
| vision | model | yes | token-media-input | `google-genai.generate-content.image-input` |
| thinking | model | no | high-token | `google-genai.generate-content.thinking` |

### 14.3 Cost gates

Add or reuse explicit gates:

```text
--allow-high-token-probes
--allow-persistent-resources
--allow-billable-artifacts
```

Rules:

- text generation does not require `--allow-billable-artifacts`;
- embedded image input does not create an artifact and does not require artifact permission;
- generated images/audio/video require artifact permission and are out of initial scope;
- Interactions stateful tests require persistent-resource permission;
- thinking probes require high-token permission;
- server tools require a future explicit server-tool permission and remain out of scope.

---

## 15. Capability model and routing

### 15.1 Capability identifiers

```text
google-genai.interactions.text
google-genai.interactions.tools
google-genai.interactions.structured-output
google-genai.interactions.image-input
google-genai.interactions.thinking
google-genai.interactions.stateful

google-genai.generate-content.text
google-genai.generate-content.tools
google-genai.generate-content.structured-output
google-genai.generate-content.image-input
google-genai.generate-content.thinking

google-genai.count-tokens
google-genai.embed-content
```

### 15.2 Evidence hierarchy

1. Explicit Google model override.
2. Model `supportedGenerationMethods`.
3. Model metadata such as `thinking`, input/output token limits.
4. Current profile-maintained Interactions-supported model set.
5. Conservative model-name rules.
6. Unknown applicability.

### 15.3 Method routing

From `supportedGenerationMethods`:

| Advertised method | Capability |
|---|---|
| `generateContent` | `google-genai.generate-content.text` |
| `countTokens` | `google-genai.count-tokens` |
| `embedContent` | `google-genai.embed-content` |
| other/unknown | preserve as metadata; do not infer |

Do not infer Interactions support from `generateContent`. Google’s Models metadata currently describes legacy generation methods, not a universal Interactions capability declaration.

### 15.4 Interactions routing

For the modern profile:

- an explicitly selected `GOOGLE_GENAI_MODEL` is an Interactions candidate;
- without explicit selection, AIQA may use a profile-maintained list of officially documented Interactions-compatible models;
- if no eligible model is present, Interactions applicability is `unknown`, not `not_applicable`;
- core Interactions requirements then become `INCONCLUSIVE`, with guidance to select a model explicitly;
- AIQA SHALL not probe every discovered embedding, image-only, TTS, music or agent model through Interactions.

Suggested profile metadata extension:

```json
"model_selection": {
  "interactions": {
    "strategy": "explicit_or_allowlist",
    "allowlist": [
      "gemini-3.8-flash",
      "gemini-3.7-flash",
      "gemini-3.6-flash",
      "gemini-3.5-flash",
      "gemini-3.1-pro-preview",
      "gemini-3-flash-preview",
      "gemini-2.5-pro",
      "gemini-2.5-flash",
      "gemini-2.5-flash-lite"
    ]
  }
}
```

This allowlist is versioned policy and must be updated only after reviewing official documentation.

### 15.5 Explicit overrides

Examples:

```bash
aiqa test=compat \
  --target=google-genai \
  --google-genai-model=custom-gemini \
  --model-capability='google-genai:custom-gemini=google-genai.interactions.text+google-genai.generate-content.text'
```

A future unified capability file should include target/protocol scope:

```json
{
  "targets": {
    "google-genai": {
      "models": {
        "custom-gemini": [
          "google-genai.interactions.text",
          "google-genai.generate-content.text"
        ],
        "custom-embedding": [
          "google-genai.embed-content"
        ]
      }
    }
  }
}
```

---

## 16. Native discovery probes

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.models.list.http` | core | `GET /v1beta/models` returns successful JSON |
| `google.models.list.schema` | core | Envelope contains `models[]`; each entry has valid `name` |
| `google.models.list.unique_names` | core | Model resource names are unique |
| `google.models.list.pagination` | optional | `pageToken` retrieves a distinct next page |
| `google.models.get` | optional | `GET /v1beta/models/{id}` returns matching model |
| `google.models.metadata.limits` | optional | Token limits are non-negative and coherent |
| `google.models.metadata.methods` | optional | `supportedGenerationMethods` is an array of strings |
| `google.models.metadata.thinking` | optional | `thinking`, when present, is boolean |
| `google.auth.x_goog_api_key` | core | Valid header authenticates |
| `google.error.invalid_auth` | optional | Invalid key returns a coherent Google error |
| `google.error.missing_auth` | optional | Missing key is rejected |
| `google.error.request_id` | optional | Request identifier is preserved when available |

Pagination details:

```text
GET /v1beta/models?pageSize=2
GET /v1beta/models?pageSize=2&pageToken=<nextPageToken>
```

Checks:

- no duplicate names across pages;
- second page is not identical to first;
- token is not logged unbounded;
- page size over the supported maximum is treated as a validation test, not a core requirement.

### 16.1 Model schema expectations

At minimum:

```json
{
  "models": [
    {
      "name": "models/gemini-3.8-flash"
    }
  ]
}
```

Optional typed fields:

```text
baseModelId
version
displayName
description
inputTokenLimit
outputTokenLimit
supportedGenerationMethods
thinking
temperature
maxTemperature
topP
topK
```

Unknown fields SHALL be preserved and tolerated.

---

## 17. Interactions API probes

### 17.1 Basic request

Request:

```json
{
  "model": "gemini-3.8-flash",
  "input": "Reply with exactly AIQA_OK.",
  "store": false
}
```

Endpoint:

```text
POST /v1beta/interactions
```

### 17.2 Core probes

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.interactions.basic` | core | Minimal stateless interaction succeeds |
| `google.interactions.store_false` | core | `store:false` is accepted |
| `google.interactions.schema` | core | Response is an Interaction object |
| `google.interactions.status` | core | Status is a recognized terminal/nonterminal value |
| `google.interactions.output_text` | core | Model output reconstructs `AIQA_OK` |
| `google.interactions.usage` | core | Usage fields are non-negative and coherent |
| `google.interactions.model` | optional | Returned model identifies the effective model |
| `google.interactions.created` | optional | Created timestamp is parseable |
| `google.interactions.system_instruction` | optional | System instruction affects marker output |
| `google.interactions.generation_config` | optional | Basic generation config is accepted |
| `google.interactions.stateless_multi_turn` | optional | Full history with `store:false` is interpreted |
| `google.interactions.error.invalid_model` | core | Unknown model yields client error |
| `google.interactions.error.invalid_json` | core | Malformed JSON variants yield client errors |
| `google.interactions.error.invalid_types` | optional | Wrong types yield client errors |
| `google.interactions.error.shape` | core | Error envelope is parseable |
| `google.interactions.error.storage_conflict` | optional | Illegal `store:false` stateful combination is rejected coherently |

### 17.3 Response schema

Required observations:

```text
id: non-empty string
object: "interaction"
status: known string
model or agent: present according to subject
steps: array
usage: object
```

Terminal status handling:

```text
completed
requires_action
failed
cancelled
incomplete
```

The exact accepted set SHALL come from the pinned profile. Unknown future statuses SHALL produce `WARN` rather than parser failure if the rest of the object is valid.

### 17.4 Output extraction

AIQA SHALL:

1. find `model_output` steps;
2. concatenate text content in order;
3. ignore thought signatures as answer text;
4. preserve step types as metrics;
5. never store full chain-of-thought-like content in a public report;
6. record only bounded summaries and hashes for sensitive/large step payloads.

### 17.5 Stateless multi-turn

The probe SHALL:

1. send turn one with `store:false`;
2. preserve all returned model-generated steps;
3. construct turn-two history including user input and exact prior steps;
4. send turn two with `store:false`;
5. verify a deterministic fact from turn one.

This tests the documented stateless path without creating retained interaction state.

### 17.6 Stateful module

Disabled by default.

When explicitly enabled:

```bash
--module=google-genai.interactions-stateful \
--allow-persistent-resources
```

The probe SHALL:

1. create interaction one with `store:true`;
2. use `previous_interaction_id` in interaction two;
3. verify context continuation;
4. delete every stored interaction created by AIQA;
5. report cleanup status separately;
6. mark cleanup failure as a P0 operational finding;
7. never leave IDs in Markdown beyond a short hash;
8. cap retained resource lifetime and count.

A stateful test SHALL never run merely because `test=compat` was selected.

---

## 18. Interactions streaming state machine

Endpoint:

```text
POST /v1beta/interactions?alt=sse
```

Request additionally includes:

```json
"stream": true,
"store": false
```

### 18.1 Core stream probes

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.interactions.stream.http` | core | HTTP 2xx and `text/event-stream` |
| `google.interactions.stream.syntax` | core | SSE framing is valid |
| `google.interactions.stream.event_type_match` | core | SSE event name matches JSON `event_type` for known events |
| `google.interactions.stream.sequence` | core | Step lifecycle is legal |
| `google.interactions.stream.text` | core | Text deltas reconstruct marker |
| `google.interactions.stream.completed` | core | Terminal `interaction.completed` is present |
| `google.interactions.stream.usage` | core | Final usage is available and coherent |
| `google.interactions.stream.done` | core | `event: done` and `data: [DONE]` terminate stream |
| `google.interactions.stream.error_event` | optional | In-band error events map to `ERROR` |
| `google.interactions.stream.unknown_event_tolerance` | implementation invariant | Parser safely skips unknown future events |

### 18.2 Legal sequence

Minimum valid sequence:

```text
interaction.created
[interaction.status_update]
(
  step.start
  step.delta*
  step.stop
)*
interaction.completed
done:[DONE]
```

Rules:

- each `step.stop.index` must correspond to a started step;
- deltas may only target an active step;
- indices need not be assumed contiguous unless the pinned contract says so;
- `interaction.completed` must occur after all completed steps;
- an `error` event terminates conformance evidence as `ERROR`;
- unknown events must be recorded and skipped;
- duplicate terminal events produce `FAIL`;
- bytes/events are bounded.

### 18.3 Delta types

Support at minimum:

```text
text
image
audio
thought_signature
thought_summary
arguments_delta
```

The parser SHALL use the `delta.type` discriminator.

### 18.4 Tool argument accumulation

For a `function_call` step:

- name and ID arrive in `step.start`;
- JSON argument fragments arrive as `arguments_delta`;
- fragments must be concatenated exactly in arrival order;
- final text must parse as JSON;
- parsed arguments must satisfy the local schema;
- no individual fragment should be parsed as complete JSON unless it actually is complete.

---

## 19. Legacy `generateContent` probes

Endpoint:

```text
POST /v1beta/models/{model}:generateContent
```

### 19.1 Basic request

```json
{
  "contents": [
    {
      "role": "user",
      "parts": [
        {"text": "Reply with exactly AIQA_OK."}
      ]
    }
  ],
  "generationConfig": {
    "maxOutputTokens": 32
  }
}
```

### 19.2 Probe catalog

| Probe ID | Modern profile | Legacy profile | Expected behavior |
|---|---:|---:|---|
| `google.generate.basic` | optional | core | Minimal request succeeds |
| `google.generate.schema` | optional | core | Response contains valid candidates |
| `google.generate.text` | optional | core | Candidate text reconstructs marker |
| `google.generate.usage` | optional | core | `usageMetadata` is coherent |
| `google.generate.finish_reason` | optional | core | Finish reason is recognized |
| `google.generate.system_instruction` | optional | optional | System instruction affects response |
| `google.generate.content_parts` | optional | optional | Array-form parts are accepted |
| `google.generate.multi_turn` | optional | optional | User/model history is interpreted |
| `google.generate.stop_sequences` | optional | optional | Stop sequence behavior is observable |
| `google.generate.max_output_tokens` | optional | optional | Small limit yields compatible stopping |
| `google.generate.candidate_count` | optional | optional | Requested candidate count is honored where supported |
| `google.generate.logprobs` | optional | optional | Logprob request produces compatible result |
| `google.generate.error.invalid_model` | optional | core | Unknown model yields client error |
| `google.generate.error.invalid_json` | optional | core | Malformed JSON yields client error |
| `google.generate.error.invalid_types` | optional | optional | Wrong types yield client error |
| `google.generate.error.shape` | optional | core | Error envelope is parseable |
| `google.generate.safety.feedback` | informative | informative | Safety outcome is distinguished from transport failure |

### 19.3 Candidate validation

A valid result must handle:

- zero candidates plus `promptFeedback`;
- one or more candidates;
- text and non-text parts;
- safety-blocked candidates;
- finish reasons;
- model version;
- response ID;
- usage metadata.

A harmless deterministic marker prompt returning no usable candidate may fail the text requirement, but attribution SHALL be model/provider scoped with appropriate confidence. It must not automatically become “gateway defect”.

### 19.4 Safety semantics

AIQA SHALL not send harmful prompts.

It SHALL distinguish:

```text
prompt blocked by safety
candidate blocked/truncated by safety
empty candidate
transport failure
schema failure
```

Safety feedback is protocol data, not a `500` equivalent.

---

## 20. Legacy streaming probes

Endpoint:

```text
POST /v1beta/models/{model}:streamGenerateContent?alt=sse
```

### 20.1 Probe catalog

| Probe ID | Modern profile | Legacy profile |
|---|---:|---:|
| `google.generate.stream.http` | optional | core |
| `google.generate.stream.syntax` | optional | core |
| `google.generate.stream.schema` | optional | core |
| `google.generate.stream.text` | optional | core |
| `google.generate.stream.finish_reason` | optional | core |
| `google.generate.stream.usage` | optional | optional |
| `google.generate.stream.model_version` | optional | optional |
| `google.generate.stream.error` | optional | optional |

### 20.2 Critical distinction

Legacy streaming SHALL NOT reuse the Interactions event state machine.

Do not require:

```text
interaction.created
step.start
step.stop
interaction.completed
event: done
```

Do not require an OpenAI-style `[DONE]` unless the pinned Google legacy contract explicitly requires one. The validator SHALL parse a stream of `GenerateContentResponse` objects and terminate correctly at EOF.

This distinction deserves an explicit regression test because copy-pasted SSE validators are a dependable factory for false failures.

---

## 21. Function calling probes

### 21.1 Legacy function calling

Capabilities:

```text
google-genai.generate-content.tools
```

Probes:

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.generate.tools.auto` | optional | Model may emit declared function call |
| `google.generate.tools.any` | optional | Required function mode emits a call |
| `google.generate.tools.none` | optional | Function calls can be suppressed where contract supports it |
| `google.generate.tools.allowed_names` | optional | Call restricted to allowed function |
| `google.generate.tools.arguments` | optional | Args parse and satisfy schema |
| `google.generate.tools.continuation` | optional | Function response continues to final answer |
| `google.generate.tools.parallel` | optional | Multiple calls have coherent IDs/names |
| `google.generate.tools.stream` | optional | Streamed calls reconstruct correctly |

### 21.2 Interactions function calling

Capabilities:

```text
google-genai.interactions.tools
```

Probes:

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.interactions.tools.call` | optional | Interaction returns `function_call` step |
| `google.interactions.tools.arguments` | optional | Accumulated args satisfy schema |
| `google.interactions.tools.requires_action` | optional | Status is `requires_action` when client action is needed |
| `google.interactions.tools.result_stateless` | optional | Stateless history with `function_result` yields final output |
| `google.interactions.tools.stream` | optional | Streaming args reconstruct |
| `google.interactions.tools.multiple` | optional | Multiple calls remain distinguishable |

The default suite SHALL use client-side function declarations only. It SHALL not execute server-side Google Search or Code Execution.

---

## 22. Structured output probes

### 22.1 Legacy surface

Request fields:

```json
{
  "generationConfig": {
    "responseMimeType": "application/json",
    "responseJsonSchema": {
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
```

Probes:

```text
google.generate.structured.mime_type
google.generate.structured.json_schema
google.generate.structured.local_validation
google.generate.structured.no_markdown
google.generate.structured.invalid_schema
```

A 2xx response alone is insufficient. AIQA SHALL parse the model text and validate it locally against the requested schema.

### 22.2 Interactions surface

The profile SHALL use the current Interactions structured-output shape from the pinned API revision.

Probes:

```text
google.interactions.structured.request
google.interactions.structured.local_validation
google.interactions.structured.stream
google.interactions.structured.invalid_schema
```

Because the Interactions contract is evolving, exact request fields SHALL live in the profile revision and native fixture tests rather than being inferred from legacy `generationConfig`.

---

## 23. Token counting

Endpoint:

```text
POST /v1beta/models/{model}:countTokens
```

Probes:

| Probe ID | Level |
|---|---:|
| `google.tokens.count.basic` | optional |
| `google.tokens.count.schema` | optional |
| `google.tokens.count.system` | optional |
| `google.tokens.count.tools` | optional |
| `google.tokens.count.media` | optional |
| `google.tokens.count.consistency` | optional |

Rules:

- `totalTokens` must be a non-negative integer;
- system content should produce a valid count;
- tools should produce a valid count;
- an embedded image may be counted without retaining media;
- comparison with generation prompt usage is diagnostic;
- exact equality SHALL NOT be required because hidden formatting, caching and modality accounting may differ;
- large deviations produce `WARN` with measured values.

---

## 24. Embeddings

Endpoint:

```text
POST /v1beta/models/{model}:embedContent
```

Applicability requires:

```text
supportedGenerationMethods contains embedContent
```

Probes:

| Probe ID | Level | Expected behavior |
|---|---:|---|
| `google.embed.basic` | optional | Basic embedding succeeds |
| `google.embed.schema` | optional | Embeddings array/object is valid |
| `google.embed.vector` | optional | Vector is non-empty and finite |
| `google.embed.dimensionality` | optional | Requested supported dimensionality is honored |
| `google.embed.task_type` | optional | Supported task type accepted |
| `google.embed.batch_input` | optional | Multiple inputs remain distinguishable |
| `google.embed.error.invalid_model` | optional | Non-embedding model rejected coherently |

AIQA SHALL NOT call `embedContent` on models that advertise only `generateContent`.

Core conformance SHALL not attempt semantic vector-quality scoring. A tiny optional diagnostic MAY verify that two identical inputs produce the same dimension, but it must not claim embedding quality.

---

## 25. Vision input

Use a tiny deterministic PNG embedded in the binary.

Legacy request:

```json
{
  "contents": [
    {
      "role": "user",
      "parts": [
        {"text": "Reply with the dominant color only."},
        {
          "inlineData": {
            "mimeType": "image/png",
            "data": "<bounded-base64>"
          }
        }
      ]
    }
  ]
}
```

Probes:

```text
google.generate.vision.inline_data
google.generate.vision.schema
google.generate.vision.semantic
google.generate.vision.usage

google.interactions.vision.inline_data
google.interactions.vision.schema
google.interactions.vision.semantic
google.interactions.vision.usage
```

Status policy:

- rejected valid media input: `UNSUP` for optional capability;
- accepted but ambiguous answer: `WARN`;
- malformed response: `FAIL`;
- timeout/429/5xx: `ERROR`;
- media payload never enters the report.

Remote media URIs SHALL not be used in the default suite.

---

## 26. Thinking probes

### 26.1 Applicability

Thinking probes run only when:

- model metadata says `thinking=true`;
- the selected model is in a profile-maintained thinking list;
- or an explicit override declares the capability.

### 26.2 Legacy probes

```text
google.generate.thinking.accepted
google.generate.thinking.budget_or_level
google.generate.thinking.include_thoughts
google.generate.thinking.usage
google.generate.thinking.signature
google.generate.thinking.stream
```

### 26.3 Interactions probes

```text
google.interactions.thinking.step
google.interactions.thinking.signature
google.interactions.thinking.summary
google.interactions.thinking.usage
google.interactions.thinking.stream
```

### 26.4 Privacy rule

AIQA SHALL NOT print raw thinking content.

Allowed report fields:

```text
thinking step present: yes/no
summary present: yes/no
signature present: yes/no
thinking tokens
bounded hash
```

The module is disabled by default and requires:

```bash
--allow-high-token-probes
```

---

## 27. Generation parameter tests

Google model parameter support belongs partly in compatibility and partly in `test=model-param`.

### 27.1 Compatibility-level validation

The compatibility profile MAY verify request acceptance/shape for:

```text
maxOutputTokens
temperature
topP
topK
candidateCount
stopSequences
seed
presencePenalty
frequencyPenalty
responseMimeType
responseJsonSchema
thinkingConfig
```

### 27.2 `test=model-param`

A later or same-release implementation SHOULD add Google-specific parameter analysis:

- accepted variants;
- invalid canaries;
- observable effect where defensible;
- confidence;
- token usage;
- declared model limits from discovery.

Rules:

- `temperature` effectiveness remains statistical;
- `topP` and `topK` effectiveness remains statistical;
- seed reproducibility is probabilistic, not guaranteed identity;
- model metadata bounds should constrain test variants;
- unsupported model-specific settings are not gateway defects;
- Interactions and `generateContent` parameter names/shapes remain separate.

MVP acceptance does not require full Google `model-param`, but the runner SHALL explicitly report it as not implemented rather than silently omit it when globally requested.

---

## 28. Usage normalization

AIQA’s provider-neutral usage fields SHALL map Google values as follows.

### 28.1 Legacy `GenerateContentResponse.usageMetadata`

| Google field | AIQA field |
|---|---|
| `promptTokenCount` | `input_tokens` |
| `candidatesTokenCount` | `output_tokens` |
| `cachedContentTokenCount` | `cached_input_tokens` |
| `thoughtsTokenCount` | `thinking_tokens` |
| `totalTokenCount` | `total_tokens` |
| `toolUsePromptTokenCount` | `provider_fields.tool_use_prompt_tokens` |
| modality token arrays | `provider_fields.*_tokens_by_modality` |

### 28.2 Interactions usage

| Google field | AIQA field |
|---|---|
| `total_input_tokens` | `input_tokens` |
| `total_output_tokens` | `output_tokens` |
| `total_cached_tokens` | `cached_input_tokens` |
| `total_thought_tokens` | `thinking_tokens` |
| `total_tokens` | `total_tokens` |
| `total_tool_use_tokens` | `provider_fields.tool_use_tokens` |
| modality arrays | `provider_fields.*_tokens_by_modality` |

### 28.3 Validation

AIQA SHALL:

- prefer provider-reported `total_tokens`;
- not invent an exact total if components do not sum due to undocumented categories;
- ensure all observed counts are non-negative;
- record arithmetic mismatch as `WARN`, not automatically `FAIL`;
- preserve Google-specific fields under `provider_fields`;
- aggregate target usage without double-counting one response reused by multiple requirements.

---

## 29. Error classification

Google error bodies commonly use a Google RPC-style envelope, but gateways may translate it. The validator must test compatibility without overfitting one backend build.

### 29.1 Outcome mapping

| Observation | Status |
|---|---|
| Expected valid request returns valid 2xx | PASS |
| Optional valid capability rejected with coherent 4xx | UNSUP |
| Required valid capability rejected | FAIL / `required_unsupported` |
| Invalid request accepted but behaves incompatibly | FAIL |
| Invalid request rejected with coherent 4xx | PASS for validation probe |
| 408, 429, timeout, connection reset, 5xx | ERROR |
| Safety block | Protocol result; evaluate dedicated safety semantics |
| Unknown future enum with otherwise valid response | WARN |
| SDK cannot parse native-valid response | SDK requirement FAIL or evidence conflict |
| Native parser fails but official SDK succeeds | Evidence conflict; investigate |

### 29.2 Error envelope

AIQA should recognize:

```json
{
  "error": {
    "code": 400,
    "message": "...",
    "status": "INVALID_ARGUMENT",
    "details": []
  }
}
```

but tolerate compatible gateway variants if required fields and semantics remain clear.

### 29.3 Retry policy

Native and extension suites SHALL:

- retry only transport errors and transient `408`, `429`, selected `5xx`;
- avoid retrying validation/authentication `4xx`;
- honor `Retry-After`;
- use bounded exponential backoff with jitter;
- report retries separately;
- keep one logical probe from being counted multiple times.

---

## 30. Native Google client

Create:

```text
internal/protocol/googlegenai/
├── client.go
├── client_test.go
├── models.go
├── interactions.go
├── interactions_stream.go
├── generate_content.go
├── generate_content_stream.go
├── tokens.go
├── embeddings.go
├── validate.go
├── validate_test.go
├── runner.go
└── runner_test.go
```

### 30.1 Client construction

Follow the Anthropic client pattern and reuse `internal/api`:

```go
headers := http.Header{}
headers.Set("x-goog-api-key", key)

inner, err := api.NewClientWithHeaders(
    normalizedBaseURL+"/"+apiVersion,
    userAgent,
    requestInterval,
    headers,
)
```

Expose:

```go
func (c *Client) TransportClient() *api.Client
func (c *Client) PacingStats() api.PacingStats
func (c *Client) PacingStatsForModel(string) api.PacingStats
```

### 30.2 Required methods

```go
ListModels(ctx, pageToken, pageSize)
GetModel(ctx, model)

CreateInteraction(ctx, body)
StreamInteraction(ctx, body)

GenerateContent(ctx, model, body)
StreamGenerateContent(ctx, model, body)

CountTokens(ctx, model, body)
EmbedContent(ctx, model, body)

WithCredentials(ctx, key)
```

### 30.3 URL builder

Implement one tested builder:

```go
func (c *Client) endpoint(resource string, method string, query url.Values) string
```

Examples:

```text
models
models/gemini-3.8-flash
models/gemini-3.8-flash:generateContent
models/gemini-3.8-flash:streamGenerateContent
interactions
```

Tests SHALL cover:

- prefix path;
- version exactly once;
- model with `models/` prefix;
- reserved characters;
- colon method;
- `alt=sse`;
- no secret in query;
- loopback;
- trailing slash;
- accidental `/v1beta` input.

### 30.4 Response limits

Use existing or stricter bounds:

```text
unary JSON body: 64 MiB maximum
SSE line: 1 MiB maximum
SSE event data: 8 MiB maximum
SSE event count: 10,000 maximum
decoded inline media retained: zero
report snippet: bounded and sanitized
```

Generated image/audio output remains out of default scope because streaming chunks can be large.

---

## 31. Protocol runner

Create a Google implementation of the protocol runner registry.

Pseudo-flow:

```go
func (r *Runner) Run(ctx context.Context, rc RunContext, cfg TargetConfig, p profile.Profile) (result.TargetReport, error) {
    client := googlegenai.NewClient(...)

    inventory := discoverAllModelPages(client)
    classifications := classifyGoogleModels(inventory, cfg, p)
    plans := assessModules(classifications, p.Modules, cfg)

    runProviderModules(...)
    runModelModules(...)
    runExtensionsIfSelected(...)

    finalizeRawSummary(...)
    extractArtifacts(...)
    assessment.ApplyTarget(...)
    guidance.ApplyTarget(...)

    return target, nil
}
```

### 31.1 Determinism

- preserve explicitly requested model order;
- sort discovered model IDs deterministically;
- restore report order after concurrent execution;
- use stable requirement IDs;
- use stable evidence IDs;
- exclude timestamps/request IDs from fingerprints.

### 31.2 Representative models

Provider-level error probes requiring a valid model SHALL choose a representative for the exact surface:

```text
Interactions representative
generateContent representative
embedding representative
```

Never use “first model in list” across unrelated capabilities.

---

## 32. Official Google Go SDK extension

### 32.1 Dependency

Add:

```go
require google.golang.org/genai v1.71.0
```

Pin and verify its module checksum.

### 32.2 Built-in path

```text
internal/extsuite/builtin/googlegenai/
├── googlegenai.go
└── googlegenai_test.go
```

### 32.3 Manifest

```text
extension id: official-sdk.google-genai
name: Official SDK – Google Gen AI Go
version: v1.71.0
protocol: google-genai
report contract: aiqa.report.v3
profile contract: aiqa.profile.v3
```

### 32.4 Suite catalog

| Suite ID | Subject | Capability |
|---|---|---|
| `models.list` | provider | none |
| `models.get` | model | discovered |
| `generate-content` | model | generate-content text |
| `generate-content.stream` | model | generate-content text |
| `generate-content.system` | model | generate-content text |
| `generate-content.multi-turn` | model | generate-content text |
| `function-calling` | model | tools |
| `function-calling.stream` | model | tools |
| `structured-output` | model | structured output |
| `count-tokens` | model | count tokens |
| `embed-content` | model | embeddings |
| `image-input` | model | image input |
| `error.responses` | model | generate-content text |

### 32.5 SDK client configuration

```go
client, err := genai.NewClient(ctx, &genai.ClientConfig{
    APIKey:  request.Credential.Value,
    Backend: genai.BackendGeminiAPI,
    HTTPClient: request.Runtime.HTTPClient,
    HTTPOptions: genai.HTTPOptions{
        BaseURL:    strings.TrimRight(request.Target.BaseURL, "/") + "/",
        APIVersion: request.Target.APIVersion,
        RetryOptions: &genai.HTTPRetryOptions{
            Attempts: genai.Ptr[int32](1),
        },
    },
})
```

The exact retry option SHALL be validated against the pinned SDK API. The behavioral acceptance test is more important than the constructor spelling:

> one planned SDK suite sends one request attempt unless AIQA’s host executor performs a visible retry.

The extension SHALL:

- set `BackendGeminiAPI` explicitly;
- set base URL explicitly;
- set API version explicitly;
- set key explicitly;
- inject the target-owned HTTP client;
- disable SDK-managed retries;
- never read ambient `GOOGLE_GENAI_USE_VERTEXAI`;
- never use ADC;
- never turn on automatic function execution;
- return one terminal result per planned suite.

### 32.6 Interactions limitation

The manifest SHALL NOT advertise an Interactions suite until the pinned Go SDK exposes and supports it.

The modern profile MAY include an informative requirement:

```text
google.official-sdk.interactions.available
```

but its status must be:

```text
not_applicable
```

or represented as profile/extension coverage metadata, not `SKIP`, `UNSUP` or `FAIL`.

Suggested Markdown:

```text
Official Google Go SDK coverage: legacy GenerateContent surface.
Interactions SDK parity: not evaluated; v1.71.0 exposes no public Go Interactions client.
```

### 32.7 Extension bindings

Examples:

```json
{
  "extension_id": "official-sdk.google-genai",
  "suite_id": "generate-content",
  "requirement_id": "google.official-sdk.generate-content",
  "related_requirement_ids": ["google.generate.basic", "google.generate.schema"],
  "subject": "model",
  "module": "google-genai.generate-content"
}
```

```json
{
  "extension_id": "official-sdk.google-genai",
  "suite_id": "generate-content.stream",
  "requirement_id": "google.official-sdk.generate-content.stream",
  "related_requirement_ids": ["google.generate.stream.http", "google.generate.stream.schema"],
  "subject": "model",
  "module": "google-genai.generate-content-stream"
}
```

SDK evidence SHALL remain an explicit interoperability requirement, not a second score vote for the native wire requirement.

---

## 33. Extension selection

The aggregate selector:

```text
--ext-suite=official-sdk
```

SHALL expand according to active targets:

| Active target | Extension |
|---|---|
| OpenAI | `official-sdk.openai` |
| Anthropic | `official-sdk.anthropic` |
| Google GenAI | `official-sdk.google-genai` |

Examples:

```bash
aiqa test=compat,ext-suite \
  --target=google-genai \
  --ext-suite=official-sdk
```

```bash
aiqa test=ext-suite \
  --target=google-genai \
  --ext-suite=official-sdk.google-genai
```

`test=compat` SHALL not scan, plan or execute extensions.

---

## 34. Reporting

### 34.1 Report schema

The implementation SHOULD remain on:

```text
aiqa.report.v3
```

because the root already supports independent targets.

Schema updates are additive:

- allow protocol `google-genai`;
- allow Google API version/base URL metadata;
- add Google profile provenance;
- add Google provider fields;
- preserve all existing OpenAI/Anthropic JSON shapes.

A `v4` report is not justified merely by adding a target.

### 34.2 Target summary

Example:

```markdown
## Targets

| Target | Profile | Overall | Core | Optional | Evidence | Official SDK |
|---|---|---:|---:|---:|---:|---|
| OpenAI | `openai-compat@2026-09-01` | PASS | 12/12 | 31/31 | 100% | PASS |
| Anthropic | `anthropic-compat@2026-09-01` | PASS-WITH-GAPS | 15/15 | 28/31 | 98% | PASS |
| Google GenAI | `google-genai-compat@2026-09-02` | PARTIAL | 16/16 | 22/29 | 97% | Legacy surface only |
```

### 34.3 Google surface summary

```markdown
### Google GenAI surfaces

| Surface | Role in profile | Applicability | Overall | Core | Optional | Evidence |
|---|---|---|---:|---:|---:|---:|
| Interactions | current/core | applicable | PASS | 16/16 | 8/9 | 100% |
| GenerateContent | legacy/optional | applicable | PARTIAL | n/a | 12/16 | 94% |
| Embeddings | optional | applicable to `gemini-embedding-001` | PASS-WITH-GAPS | n/a | 4/5 | 100% |
```

### 34.4 No cross-target averaging

The report SHALL NOT calculate:

```text
(OpenAI score + Anthropic score + Google score) / 3
```

The run verdict follows the existing worst blocking target policy, while each target retains independent measures.

### 34.5 Inventory

Google inventory columns:

```text
Model
Resource name
Supported methods
Input limit
Output limit
Thinking
Interactions routing
GenerateContent routing
Embeddings routing
Evaluation
```

### 34.6 SDK presentation

```markdown
### Official SDK – Google Gen AI Go `v1.71.0`

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `models.list` | provider | PASS | 102 ms | SDK listed models |
| `generate-content` | `gemini-3.8-flash` | PASS | 854 ms | SDK parsed response |
| `generate-content.stream` | `gemini-3.8-flash` | FAIL | 901 ms | SDK rejected malformed stream chunk |
| `interactions` | — | N/A | — | Not exposed by pinned Go SDK |
```

---

## 35. Guidance and remediation

Add:

```text
guidance/packs/generic-google-genai-gateway.json
```

The pack SHALL be non-normative and may not modify verdicts.

### 35.1 Finding codes

```text
google.auth.x_goog_api_key_missing
google.auth.query_key_only
google.api_version.mismatch
google.models.envelope_invalid
google.models.resource_name_invalid
google.models.pagination_invalid
google.models.duplicate_name
google.models.method_metadata_invalid
google.model.method_mismatch

google.interactions.endpoint_missing
google.interactions.store_false_rejected
google.interactions.response_invalid
google.interactions.output_missing
google.interactions.usage_invalid
google.interactions.stream.syntax_invalid
google.interactions.stream.sequence_invalid
google.interactions.stream.event_type_mismatch
google.interactions.stream.done_missing
google.interactions.stream.error_event
google.interactions.state_cleanup_failed

google.generate.response_invalid
google.generate.candidate_missing
google.generate.usage_invalid
google.generate.stream.invalid
google.generate.stream.wrong_contract
google.generate.safety_blocked

google.tools.arguments_invalid
google.tools.continuation_failed
google.structured_output.invalid
google.tokens.count_inconsistent
google.embedding.vector_invalid
google.vision.semantic_uncertain
google.thinking.usage_invalid

google.sdk.parse_failure
google.sdk.request_failure
google.sdk.surface_unavailable
google.sdk.native_conflict
```

### 35.2 Priority examples

| Finding | Priority | Action |
|---|---:|---|
| Required Interactions endpoint absent in modern profile | P0 | Implement endpoint or select legacy profile |
| `store:false` ignored | P0 | Fix storage semantics and data-retention behavior |
| Core stream sequence malformed | P0 | Fix SSE encoder/state machine |
| SDK cannot parse native-valid legacy stream | P1 | Align stream response shape |
| Optional embeddings unsupported | P2 | Implement or document capability |
| No explicit Interactions model selected | P0/P1 | Configure target model; evidence currently incomplete |
| Go SDK lacks Interactions surface | Informative | No provider remediation |
| Thinking test disabled | P3 | Re-run with high-token opt-in if needed |

### 35.3 Example recommendation

```json
{
  "title": "Honor store=false for Interactions",
  "priority": "P0",
  "action_kind": "fix",
  "target": {
    "component": "google-genai gateway",
    "endpoint": "POST /v1beta/interactions"
  },
  "why": "The current profile requires a stateless compatibility path and the request was accepted without demonstrating the requested storage behavior.",
  "steps": [
    {"id": "1", "text": "Forward the store field without rewriting false to the provider default."},
    {"id": "2", "text": "Ensure no previous_interaction_id resource is required for a stateless request."},
    {"id": "3", "text": "Return a compatible Interaction object while preserving store=false semantics."}
  ],
  "verification": [
    {
      "kind": "requirement",
      "ids": ["google.interactions.store_false"]
    }
  ]
}
```

---

## 36. Security and privacy requirements

### 36.1 Secrets

Never store:

```text
API keys
Authorization headers
x-goog-api-key
signed media URLs
query key values
cookies
ADC credentials
service account JSON
```

### 36.2 Interactions storage

Default:

```json
"store": false
```

All modern core probes SHALL be stateless.

Persistent probes require explicit opt-in and cleanup.

### 36.3 Media

- use embedded tiny media;
- do not fetch arbitrary public URLs by default;
- do not store base64;
- report MIME, byte count, dimensions and SHA-256 only;
- cap decoded size;
- reject local/private remote targets if remote media is later added.

### 36.4 Tools

Default allowed:

```text
client-side function declaration with deterministic fake result
```

Default forbidden:

```text
Google Search
Code Execution
Computer Use
Remote MCP
URL Context
File Search
Deep Research
```

### 36.5 Reports

Snippets SHALL:

- be bounded;
- redact secrets;
- remove large thought summaries;
- remove media data;
- remove signed query strings;
- avoid full tool arguments if they can contain user data;
- store repeated logs as content-addressed guidance artifacts once.

---

## 37. Request and cost budgets

Suggested default per-call output limits:

| Probe | Max output |
|---|---:|
| Marker generation | 32 tokens |
| Multi-turn | 48 tokens |
| Structured output | 64 tokens |
| Function calling | 64 tokens |
| Vision marker | 32 tokens |
| Thinking | profile/model specific; explicit high-token gate |

Suggested default request counts per evaluated text model:

| Module | Approximate requests |
|---|---:|
| Models/discovery | shared, 2–5 |
| Interactions core | 5–8 |
| Interactions stream | 1–2 |
| Legacy basic | 4–7 |
| Legacy stream | 1–2 |
| Tools | 3–6 |
| Structured output | 2–3 |
| Count tokens | 2–4 |
| SDK extension | 6–10 |

Actual usage SHALL be reported, not guessed.

Add target-scoped safety limits if not already general:

```text
--max-requests-per-model
--max-output-tokens-per-probe
--allow-high-token-probes
--allow-persistent-resources
```

A model catalog with dozens of models can be expensive. The report must show:

```text
discovered models
selected models
evaluated models
inventory-only models
estimated/observed request count
observed token usage
```

---

## 38. Configuration data model

Extend `TargetConfig`:

```go
type TargetConfig struct {
    Protocol   Protocol
    BaseURL    string
    APIKey     string
    APIVersion string
    Models     []string
    Profile    string
    Modules    []string

    // existing fields...

    AllowPersistentResources bool
    AllowHighTokenProbes     bool
}
```

Public report config:

```json
{
  "id": "google-genai",
  "protocol": "google-genai",
  "base_url": "http://127.0.0.1:8080/google",
  "api_version": "v1beta",
  "profile": "google-genai-compat-2026-09.json",
  "requested_models": ["gemini-3.8-flash"],
  "credential_source": "GOOGLE_GENAI_API_KEY",
  "allow_persistent_resources": false,
  "allow_high_token_probes": false
}
```

`credential_source` MAY name the source variable but SHALL never include a key value.

---

## 39. CLI changes

Add:

```text
--google-genai-base-url URL
--google-genai-api-key KEY
--google-genai-api-version VERSION
--google-genai-model ID[,ID]
--google-genai-profile FILE
--allow-persistent-resources
--allow-high-token-probes
```

Help text:

```text
Google GenAI:
  GOOGLE_GENAI_BASE_URL      base URL before /v1beta
  GOOGLE_GENAI_API_KEY       x-goog-api-key credential
  GOOGLE_GENAI_API_VERSION   API version (default v1beta)
  GOOGLE_GENAI_MODEL         selected Gemini model(s)
  GOOGLE_GENAI_PROFILE       embedded profile filename
```

Examples:

### Native current profile

```bash
GOOGLE_GENAI_BASE_URL="https://generativelanguage.googleapis.com" \
GOOGLE_GENAI_API_KEY="$GEMINI_API_KEY" \
GOOGLE_GENAI_MODEL="gemini-3.8-flash" \
aiqa test=compat \
  --target=google-genai \
  --format=markdown,json \
  --name=google-current
```

### Native + official SDK

```bash
GOOGLE_GENAI_BASE_URL="https://generativelanguage.googleapis.com" \
GOOGLE_GENAI_API_KEY="$GEMINI_API_KEY" \
GOOGLE_GENAI_MODEL="gemini-3.8-flash" \
aiqa test=compat,ext-suite \
  --target=google-genai \
  --ext-suite=official-sdk.google-genai \
  --format=markdown,json \
  --name=google-with-sdk
```

### Legacy strict profile

```bash
GOOGLE_GENAI_BASE_URL="http://127.0.0.1:8080/google" \
GOOGLE_GENAI_API_KEY="$GOOGLE_GENAI_API_KEY_QA" \
GOOGLE_GENAI_MODEL="gemini-3.8-flash" \
GOOGLE_GENAI_PROFILE="google-genai-generate-content-compat-2026-09.json" \
aiqa test=compat,ext-suite \
  --ext-suite=official-sdk.google-genai \
  --name=google-legacy
```

---

## 40. File-level implementation map

| Path | Change |
|---|---|
| `internal/config/config.go` | Add protocol, env resolution, flags, validation and aliases |
| `cmd/aiqa/main.go` | Add help text and examples |
| `internal/profile/profile.go` | Add Google protocol validator/selection |
| `schema/profile-v3.schema.json` | Permit and constrain `google-genai` |
| `profile/google-genai-compat-2026-09.json` | Add modern profile |
| `profile/google-genai-generate-content-compat-2026-09.json` | Add legacy strict profile |
| `internal/protocol/registry.go` | Add protocol runner registry |
| `internal/protocol/googlegenai/*` | Native client, runner, validators and streams |
| `internal/app/app.go` | Register Google target runner |
| `internal/capability/*` | Add protocol-scoped Google classifier/routing |
| `internal/extsuite/builtin/googlegenai/*` | Official Go SDK extension |
| `internal/extsuite/registry.go` | Register third built-in |
| `internal/extsuite/planner.go` | Expand aggregate `official-sdk` selector |
| `internal/result/types.go` | Add no breaking fields unless needed |
| `schema/report-v3.schema.json` | Permit Google protocol and provider fields |
| `internal/assessment/*` | Ensure protocol-neutral module aggregation |
| `internal/guidance/*` | Add Google finding normalization |
| `guidance/packs/generic-google-genai-gateway.json` | Google recommendations |
| `internal/report/markdown.go` | Render Google surfaces and SDK coverage |
| `internal/app/app_test.go` | Add tri-target integration |
| `test/fixtures/*` | Add deterministic Google facade |
| `go.mod`, `go.sum` | Pin `google.golang.org/genai v1.71.0` |
| `Makefile` | Profiles, dependencies, SBOM, upstream checks |
| `script/check-profile-upstream.go` | Support Google SDK source |
| `docs/architecture.md` | Third target and two surfaces |
| `docs/extension-suites.md` | Third official SDK |
| `docs/reports.md` | Google target rendering |
| `README.md` | User-facing configuration |
| `CHANGELOG.md` | Feature and compatibility note |

---

## 41. Deterministic mock provider

Add a test server exposing:

```text
/google/v1beta/models
/google/v1beta/models/{id}
/google/v1beta/models/{id}:generateContent
/google/v1beta/models/{id}:streamGenerateContent
/google/v1beta/models/{id}:countTokens
/google/v1beta/models/{id}:embedContent
/google/v1beta/interactions
```

Catalog:

```text
gemini-mock-text
gemini-mock-thinking
gemini-mock-vision
gemini-mock-embedding
gemini-mock-unknown
```

Metadata SHALL make routing testable:

```json
{
  "name": "models/gemini-mock-text",
  "inputTokenLimit": 4096,
  "outputTokenLimit": 1024,
  "supportedGenerationMethods": [
    "generateContent",
    "countTokens"
  ],
  "thinking": false
}
```

The mock SHALL support:

- pagination;
- malformed request variants;
- transient 429 fixture;
- native valid stream;
- deliberately broken stream;
- safety-block fixture;
- Interactions stateless history;
- function call and result;
- structured output;
- image input;
- embedding vectors;
- extension SDK requests through the actual SDK.

---

## 42. Test strategy

### 42.1 Unit tests

Configuration:

- activation matrix;
- alias precedence;
- conflicting aliases;
- target ordering;
- base URL normalization;
- API version validation;
- model normalization;
- secret omission.

Profile:

- Google protocol accepted;
- invalid module prefixes rejected;
- extension binding validation;
- modern and legacy profile schema validation;
- every probe assigned correctly;
- no duplicate IDs.

Client:

- URL construction;
- header authentication;
- no query key;
- model escaping;
- SSE query;
- bounds;
- cancellation;
- retry classification.

Validation:

- models envelope;
- usage mapping;
- Interaction response;
- both SSE state machines;
- unknown enums/events;
- error envelope;
- embedding vectors;
- JSON Schema output.

### 42.2 Integration tests

- Google-only native run;
- Google-only native + SDK run;
- tri-target native run;
- tri-target native + all official SDKs;
- modern profile against Interactions-capable mock;
- legacy profile against `generateContent`-only mock;
- modern profile against legacy-only mock produces expected FAIL;
- legacy profile against legacy-only mock passes;
- embed-only model never receives generation calls;
- text model never receives embedding calls;
- SDK extension makes one attempt;
- report v3 validates;
- Markdown is deterministic;
- guidance is deterministic;
- API key absent from all artifacts.

### 42.3 Race tests

- concurrent models preserve report order;
- immutable extension manifest;
- target pacers isolated;
- SSE parser has no shared mutable state;
- guidance fingerprints stable.

### 42.4 Live tests

Live tests SHALL be:

- secret-gated;
- manually dispatched or scheduled;
- not required for ordinary pull requests;
- constrained to explicit models;
- constrained to low token caps;
- native and SDK results retained as CI artifacts;
- never print keys.

Suggested secrets:

```text
GOOGLE_GENAI_API_KEY_QA
GOOGLE_GENAI_MODEL_QA
```

Suggested direct endpoint:

```text
https://generativelanguage.googleapis.com
```

Live tests validate the reference service, not third-party gateways.

---

## 43. Quality and supply-chain gates

Update `Makefile`.

### 43.1 Profiles

```make
profiles-check:
    test -f profile/google-genai-compat-2026-09.json
    test -f profile/google-genai-generate-content-compat-2026-09.json
```

### 43.2 Dependency pin

```make
dependency-check:
    go mod verify
    test "$(go list -m -f '{{.Version}}' google.golang.org/genai)" = "v1.71.0"
```

### 43.3 SBOM

Assert:

```text
google.golang.org/genai@v1.71.0
```

with its detected license.

### 43.4 Vulnerability scan

Existing `govulncheck` SHALL cover the new dependency.

### 43.5 Upstream profile check

The Google checker SHALL compare:

- pinned SDK tag/commit;
- latest SDK release;
- profile reviewed date;
- optionally the pinned protobuf contract commit.

Documentation pages do not provide immutable commits. Their semantic review remains manual and SHALL not be auto-updated.

Output example:

```text
Google Gen AI SDK upstream moved.
profile SDK: v1.71.0 / bfe8a871...
upstream SDK: v1.72.0 / ...
Review API and generated type diffs before changing profile.
```

---

## 44. Implementation milestones

### PR 0 — protocol registry refactor

Deliver:

- protocol runner registry;
- protocol profile-validator registry;
- no behavior changes;
- OpenAI/Anthropic golden reports unchanged;
- app switch reduced to registration/lookup.

Acceptance:

- `make check`;
- `make race`;
- existing mock reports byte-stable except intentionally updated metadata.

### PR 1 — Google target foundation and profiles

Deliver:

- protocol enum;
- env/CLI resolution;
- base URL/API version semantics;
- profile schema changes;
- modern and legacy profile assets;
- target/report plumbing;
- inventory-only Google runner stub.

Acceptance:

- tri-target configuration parses;
- Google target appears in v3 report;
- no Google API call yet except optionally model listing in next PR;
- existing targets unaffected.

### PR 2 — discovery and legacy core

Deliver:

- Google client;
- Models list/get/pagination;
- capability routing;
- `generateContent`;
- legacy stream;
- errors;
- usage;
- legacy strict profile fully executable.

Acceptance:

- legacy profile can PASS against mock;
- embed-only model is N/A for generation;
- stream validator does not require Interactions events.

### PR 3 — token counting, embeddings, tools, structured output and vision

Deliver:

- CountTokens;
- EmbedContent;
- legacy function calling;
- structured output;
- inline image input;
- related guidance.

Acceptance:

- all optional modules route correctly;
- no media payload in reports;
- local schema validation works.

### PR 4 — Interactions current core

Deliver:

- stateless Interactions;
- response validation;
- modern SSE state machine;
- stateless multi-turn;
- function-call steps;
- usage mapping;
- modern profile core executable.

Acceptance:

- every core modern requirement passes against mock;
- `store:false` is present on every default interaction;
- no stored resource created.

### PR 5 — official Google Go SDK extension

Deliver:

- dependency pin;
- built-in extension;
- manifest and bindings;
- aggregate `official-sdk` expansion;
- retry/HTTP injection tests;
- SBOM/security updates.

Acceptance:

- SDK sends requests through mock;
- one attempt per suite;
- no Interactions suite advertised;
- native/SDK conflict handled by assessment.

### PR 6 — guidance, reports, docs and live CI

Deliver:

- Google guidance pack;
- surface summary;
- examples;
- README/docs/changelog;
- secret-gated workflow;
- regenerated mock report.

Acceptance:

- Markdown leads with current vs legacy surface distinction;
- recommendations include targeted requirements;
- JSON validates;
- no secrets;
- all quality gates pass.

---

## 45. Acceptance criteria

### Configuration

1. `google-genai` is a valid target.
2. `--target=auto` activates Google only when a Google base URL and usable key are resolved.
3. A configured base URL without a key exits `2`.
4. A generic Google key alone does not activate Google.
5. Canonical key overrides aliases.
6. Conflicting aliases without canonical key exit `2`.
7. Loopback HTTP works without global `--allow-http`.
8. Non-loopback plaintext remains rejected.
9. Base path prefixes are preserved.
10. API version is appended exactly once.
11. Multi-target legacy `--model` remains an ambiguity error.
12. `--google-genai-model` affects only Google.

### Profile and routing

13. Both Google profiles validate against profile v3 schema.
14. Modern profile makes Interactions core.
15. Legacy profile makes `generateContent` core.
16. N/A requirements do not enter denominators.
17. Unsupported optional capability does not produce core FAIL.
18. Required unsupported capability does produce FAIL.
19. `supportedGenerationMethods` routes generation and embedding modules.
20. Embed-only models never receive generation calls.
21. Unknown models do not automatically receive every suite.
22. Interactions routing uses explicit selection or versioned allowlist.
23. A missing eligible Interactions model makes modern core INCONCLUSIVE, not PASS.

### Native REST

24. Authentication uses `x-goog-api-key`.
25. No request URL contains the real key.
26. Model list pagination is tested.
27. Raw metadata is preserved.
28. `models/` prefix is normalized without corrupting raw name.
29. Basic Interactions sends `store:false`.
30. Default Interactions tests create no retained resource.
31. Stateful tests require explicit permission.
32. Stateful cleanup is verified.
33. Interaction response schema is validated.
34. Interactions SSE sequence is validated.
35. Interactions requires terminal `done/[DONE]`.
36. In-band SSE error becomes `ERROR`.
37. Unknown future event types are tolerated.
38. Function arguments are accumulated across deltas.
39. Legacy generation schema is validated independently.
40. Legacy stream uses its own parser.
41. Legacy stream is not failed for lacking Interactions event names.
42. Safety feedback is not mapped to transport error.
43. Usage mapping preserves thought/cache/tool fields.
44. Token counting mismatch is diagnostic, not exact-equality failure.
45. Embedding vector elements must be finite.
46. Structured output is locally JSON Schema validated.
47. Vision media is bounded and absent from reports.
48. Raw thinking content is not rendered.

### Official SDK

49. `official-sdk.google-genai` is built in.
50. It pins `google.golang.org/genai v1.71.0`.
51. It uses `BackendGeminiAPI`.
52. It uses the target base URL and API version.
53. It uses the target-owned HTTP client.
54. SDK retries are disabled or behaviorally proven to one attempt.
55. It exposes only suites present in the pinned SDK.
56. It does not fabricate an Interactions suite.
57. Aggregate `official-sdk` selects Google only when Google is active.
58. `test=compat` alone never runs the SDK.
59. Bound SDK requirements count once.
60. Native/SDK disagreement becomes evidence conflict, not duplicate failure votes.

### Reports and guidance

61. The report remains `aiqa.report.v3`.
62. OpenAI, Anthropic and Google appear as independent targets.
63. Target order is deterministic.
64. No cross-protocol average is emitted.
65. Current and legacy Google surfaces are shown separately.
66. Official Go SDK’s Interactions limitation is informational.
67. API keys are absent from JSON, Markdown, stderr and artifacts.
68. Base64 media is absent.
69. Repeated SDK logs are deduplicated.
70. Findings have stable fingerprints.
71. Recommendations cite requirement IDs.
72. Guidance does not alter verdict or exit code.
73. Run verdict follows existing target policy.
74. Existing OpenAI and Anthropic JSON consumers remain compatible.

### Quality

75. `make check` passes.
76. `make race` passes.
77. `make security-check` passes.
78. `go mod verify` passes.
79. SBOM contains the exact Google SDK pin.
80. Direct live tests are opt-in and secret-gated.
81. Default CI performs no real billable Google calls.
82. Golden reports are deterministic.

---

## 46. Risks and mitigations

### Risk: Google API evolves quickly

Mitigation:

- immutable profile revision;
- pinned SDK;
- reviewed-at dates;
- upstream checks;
- unknown enum/event tolerance;
- no automatic profile rewrite.

### Risk: Interactions and legacy are conflated

Mitigation:

- two profiles;
- separate modules;
- separate stream parsers;
- separate report rows;
- explicit profile title.

### Risk: Go SDK lags Interactions

Mitigation:

- native REST remains authoritative;
- SDK extension advertises only real surfaces;
- missing SDK surface is informational;
- add Interactions suite only after a pinned Go SDK exposes it.

### Risk: accidental data retention

Mitigation:

- `store:false` core;
- persistent module disabled;
- explicit permission;
- cleanup verification;
- retention warning in guidance.

### Risk: model catalog causes excessive spend

Mitigation:

- explicit model recommended;
- capability routing;
- output caps;
- request budgets;
- inventory-only reporting;
- billable/high-token gates.

### Risk: model-specific absence blamed on gateway

Mitigation:

- subject-scoped findings;
- capability evidence;
- attribution confidence;
- N/A and UNSUP semantics;
- provider-level and model-level requirements remain distinct.

### Risk: custom gateway does not expose `/models`

Mitigation:

- explicit model selection can still create a synthetic inventory entry;
- modern profile remains INCONCLUSIVE for discovery if Models is core;
- a future reduced profile may relax discovery, but the default claim must not silently do so.

### Risk: API key leaks through SDK/query examples

Mitigation:

- header-only native auth;
- custom redaction;
- SDK transport inspection tests;
- no request URL in raw report without sanitization.

### Risk: SSE parser rejects future events

Mitigation:

- skip/log unknown events;
- enforce invariants only for known event types;
- preserve bounded unknown-event metrics.

---

## 47. Definition of Done

The feature is complete when:

1. A single AIQA binary can run OpenAI, Anthropic and Google GenAI targets together.
2. Google has two embedded, valid, documented profiles.
3. The modern profile executes Models + Interactions core natively.
4. The legacy profile executes Models + `generateContent` core natively.
5. Both Google stream contracts have distinct state machines.
6. Model applicability prevents category errors.
7. The official Google Go SDK extension is pinned and integrated.
8. SDK coverage does not falsely claim Interactions support.
9. Reports show separate surface and target assessments.
10. Deterministic guidance is generated for known Google findings.
11. Default Interactions probes use `store:false`.
12. Persistent and high-cost operations remain explicit opt-ins.
13. Existing OpenAI and Anthropic behavior remains regression-tested.
14. All schemas, race tests, security checks, SBOM checks and live-gated tests pass.
15. README, architecture, extension, report and profile-maintenance documentation is complete.

---

## 48. Decisions made

| Question | Decision |
|---|---|
| Is Google an OpenAI module? | No, independent target |
| Target name | `google-genai` |
| Backend | Gemini Developer API |
| Vertex | Future separate target |
| Current core surface | Interactions |
| Legacy support | Separate strict profile |
| Default API version | `v1beta` |
| Base URL meaning | Prefix before API version |
| Native auth | `x-goog-api-key` |
| Default interaction storage | `store:false` |
| Official SDK | `google.golang.org/genai v1.71.0` |
| SDK Interactions claim | Not available/evaluated at pinned version |
| SDK execution | Explicit `test=ext-suite` |
| Report version | Keep `aiqa.report.v3` |
| Aggregate score | None across targets or surfaces |
| Generated media | Out of default scope |
| Server tools | Out of default scope |
| Stateful tests | Explicit persistent-resource opt-in |

---

## 49. Deferred decisions

These should not block the first Google target:

- a separate `google-vertex` target;
- Interactions get/list/delete conformance beyond cleanup;
- agent profiles for Deep Research and Antigravity;
- Live API/WebSocket compatibility;
- file upload and Files API;
- caches;
- batches;
- tuning;
- image/audio/video output generation;
- automatic function calling;
- server-side tools;
- Google SDKs in Python/JavaScript/Java as additional extensions;
- full Google `test=model-param`;
- cross-version profile comparison;
- targeted `aiqa verify` commands;
- HTML report.

Each deferred item should become a separate profile/module or extension, not a quiet expansion of an existing score denominator.

---

## 50. Source maintenance checklist

Before merging or updating the profile:

1. Review current Interactions overview.
2. Review Interactions streaming event types.
3. Review Models schema.
4. Review GenerateContent request/response and streaming.
5. Review CountTokens.
6. Review EmbedContent.
7. Review API key guidance.
8. Compare pinned Go SDK release with latest.
9. Compare Go SDK exported client services.
10. Confirm custom base URL and API version behavior.
11. Confirm retry configuration.
12. Review protobuf diffs for legacy Generative Language methods.
13. Update `reviewed_at`.
14. Bump profile revision on semantic change.
15. Regenerate fixtures.
16. Run:
    ```bash
    make check
    make race
    make security-check
    ```
17. Run secret-gated direct smoke tests.
18. Verify no key or media data in artifacts.

---

## 51. Reference implementation command

The final tri-target acceptance command is:

```bash
OPENAI_BASE_URL="https://api.openai.com/v1" \
OPENAI_API_KEY="$OPENAI_API_KEY_QA" \
ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic" \
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY_QA" \
GOOGLE_GENAI_BASE_URL="http://127.0.0.1:8080/google" \
GOOGLE_GENAI_API_KEY="$GOOGLE_GENAI_API_KEY_QA" \
GOOGLE_GENAI_API_VERSION="v1beta" \
GOOGLE_GENAI_MODEL="gemini-3.8-flash" \
aiqa --verbose \
  test=compat,ext-suite \
  --ext-suite=official-sdk \
  --format=markdown,json \
  --name=compat-report
```

The generated Markdown SHALL answer, without requiring the reader to excavate raw JSON:

1. Does the OpenAI target meet its profile?
2. Does the Anthropic target meet its profile?
3. Does the Google GenAI target meet the selected current or legacy profile?
4. Which Google models and methods were applicable?
5. Did Interactions run statelessly?
6. Did each streaming contract conform?
7. Which optional capabilities are absent?
8. Could the official Go SDK send and parse its supported calls?
9. Which SDK surfaces were not available at the pinned version?
10. What should the gateway maintainer fix first?
11. How can each finding be verified by stable requirement ID?
12. How many requests and tokens were consumed?

That is the actual product outcome: not merely another green percentage, but a protocol-specific map of what works, what does not, what was not tested, and what to repair.
