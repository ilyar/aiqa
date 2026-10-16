# Unified model routing

AIQA uses one global model request and resolves it independently for every active protocol target. The same ID can therefore be tested by OpenAI, Anthropic, and Google GenAI when each target exposes it in its own inventory.

## Selection contract

`--model ID[,ID]` is parsed as an ordered CSV list. AIQA trims each component, rejects an empty component, and removes later duplicates while preserving the first occurrence. The removed `--openai-model`, `--anthropic-model`, and `--google-genai-model` selectors, together with `GOOGLE_GENAI_MODEL`, return an explicit migration error.

Each target first retrieves and validates its own model inventory. The raw entries remain in `discovery.models`; normalized IDs are used only for matching:

- OpenAI-compatible and Anthropic targets trim the inventory ID and compare it exactly.
- Google removes the protocol-defined `models/` resource prefix for matching and retains the original resource name as raw provenance.

With an explicit global list, a target schedules only IDs present in its successfully retrieved inventory. It never creates synthetic metadata or probes an absent ID. An ID missing from one target is silently omitted there. If the global list is absent, only trusted inventory entries are scheduled; ambiguous entries remain visible as inventory-only records and receive no automatic preflight.

Silent omission does not establish conformance: a model-scoped target with no
evaluated models is `INCONCLUSIVE` and non-rankable, even if another target
matched the requested ID. The target-level incomplete-scope requirement does
not create a synthetic model or claim provider nonconformance.

Successful discovery with zero matches is valid. A failed or malformed discovery remains visible as an error or inconclusive discovery result and cannot be converted into a successful empty selection.

## Ambiguous routes

An explicitly requested inventory entry with insufficient capability metadata receives one minimal, non-billable text preflight for that target's native text surface. A successful response proves that the requested model route is available, and the response is reused by the ordinary basic/schema/usage checks. AIQA does not send a second basic request for the same model.

An explicit model-route rejection is a model-scoped failure and stops dependent probes. A transport failure, timeout, 408, 429, or 5xx is an availability error and produces an inconclusive root. Both outcomes retain the selected model and the preflight evidence, but they do not run the full suite after the route is blocked. Image modules never use a billable image request as a preflight.

## Capability and module routing

Classification is target-local. Explicit capability overrides refine a model only after inventory membership is proven; an override cannot make a missing ID available. Module applicability and execution are recorded separately:

- `not_applicable` means positive evidence points to another capability.
- `unknown` means the route or capability evidence is insufficient.
- `applicable` plus `executed` means every declared profile requirement is expected to have evidence.
- An applicable executed module that omits a declared probe becomes an execution error; a skipped applicable module remains a visible `SKIP`.

The default unknown policy is inventory-only for OpenAI, Anthropic, and Google. The low-confidence `chat`, `messages`, or `text` fallbacks are explicit opt-ins. A complete Anthropic `ModelInfo` identifies a Messages route; incomplete Anthropic entries remain ambiguous. Google `supportedGenerationMethods` describes the legacy GenerateContent surface and does not imply Interactions support.

Billable image modules require explicit authorization. The artifact budget is shared by native and SDK image probes for each model, so an extension cannot create an additional artifact after the native budget is exhausted.

## Evidence and guidance

Native, SDK, and extension evidence are reconciled against the same target-qualified subject and requirement ID. Derived probes require their prerequisites. A blocked dependent is `SKIP` with `blocked_by` evidence; it does not copy the prerequisite's `FAIL`, `ERROR`, or `UNSUPPORTED` status. Route and transport failures retain a root-cause signal, and guidance groups findings by target, subject, and root cause. Blocked dependent requirements are excluded from actionable recommendation counts.

The Anthropic SDK tool suites use the same bounded request:

```text
Call get_status for resource-alpha.
```

Their `get_status` schema requires the string property `resource_id`, sets `additionalProperties: false`, and validates the exact `resource-alpha` argument. Anthropic Models validation is pinned to `anthropic-sdk-go v1.68.0`: model identity and RFC3339 creation time are required, token limits are positive, and the required capability groups and nested fields are checked while unknown additive fields remain valid.

## Transport telemetry

Every target and model rate-limit record distinguishes logical requests from physical HTTP attempts. It reports successful requests, rate-limited responses, retry attempts, recovered and exhausted retries, fixed or server-directed backoff, and the pacer state. Latency samples count only unique successful HTTP attempts; duplicated `TestResult` records and derived probes do not add samples. Safe rate and concurrency are omitted from JSON and rendered as `N/S` when no request succeeded. Model-scoped records include the model ID, and Google URL-addressed requests preserve model attribution.

## Report v3

`config.requested_models` is the single global request provenance field. Target rows contain only models actually selected from that target's inventory; they do not emit a target-local requested assignment. Raw inventory and discovery validation remain target-local. A successful target with no selected models contributes no model rows or model findings, while a discovery failure remains an explicit target error/inconclusive result. Markdown and JSON use the same selected-model, applicability, dependency, and telemetry semantics.

Google keeps two independent profile contracts:

- `google-genai-compat-2026-09.json` tests Models and current Interactions as the core surface.
- `google-genai-generate-content-compat-2026-09.json` tests Models and legacy GenerateContent as the core surface.

## Verification boundary

Local implementation evidence comes from the repository tests and gates:

```bash
GOCACHE=/tmp/aiqa-gocache go test ./...
make check
make race
make security-check
go mod verify
```

Live provider acceptance, billable artifact authorization, and production credentials are separate operational actions. A passing local gate does not claim that a live provider endpoint has been tested.

## Local audit evidence

The 2026-09-04 implementation audit covers the global selector and exact target-local inventory intersection, silent absence, ambiguous-route preflight reuse, transient and route-rejection blocking, independent dual-target execution, mixed capability routing, billable image gating, Anthropic Models validation and SDK tools, dependency blocking, guidance grouping, and transport-owned telemetry. Deterministic fixtures cover these paths in `internal/config`, `internal/protocol`, `internal/app`, `internal/suites`, `internal/assessment`, `internal/guidance`, and `internal/api`.

The repository gates are the release evidence for this local implementation:

```text
GOCACHE=/tmp/aiqa-gocache go test ./...
GOCACHE=/tmp/aiqa-gocache make check
GOCACHE=/tmp/aiqa-gocache make race
GOCACHE=/tmp/aiqa-gocache make security-check
```

These gates do not replace authorized live acceptance. No live provider, billable artifact, production credential, merge, or publication is implied by the local audit.
