# Backlog

## Targeted verification commands

Status: planned, not implemented.

### Problem

Earlier actionable reports emitted `aiqa test=compat` as a verification command for every finding. The command reran the complete compatibility suite, used `$MODEL` instead of the model already recorded in the finding, and looked narrower than its actual execution scope. Those commands have been removed. Reports now contain only structured requirement and Official SDK suite targets.

### Intended behavior

Add a targeted verification entry point that executes only the selected canonical requirement and its declared prerequisites. A model-scoped target must use the exact model ID from the finding. A provider-scoped target must not add a model argument.

The proposed interface is:

```text
aiqa verify --requirement <requirement-id> [--model <exact-model-id>]
aiqa verify --external-suite <suite-id> [--model <exact-model-id>]
```

Report generation must remain command-free by default. A separate `--report-verify-commands` flag may add commands to Markdown and JSON only when the installed AIQA version can execute every emitted target selectively.

### Acceptance criteria

- The planner resolves one requirement or Official SDK suite by stable ID and rejects unknown or ambiguous IDs.
- The runner executes only that target and the minimum declared prerequisites; it does not silently expand to the complete `compat` or `model-param` suite.
- Model-scoped commands contain the exact shell-quoted model ID from the finding and never use `$MODEL` or another placeholder.
- Provider-scoped commands omit `--model`.
- Parameter checks can be selected by their canonical requirement ID.
- Native and Official SDK targets retain their distinct execution paths.
- The targeted result states which prerequisites ran and which target produced the final status.
- `--report-verify-commands` defaults to off and changes neither probes nor conformance results.
- Commands are omitted when a target is not executable by the current binary, profile, or external-checker setup.
- Generated commands exclude secrets and rely on the normal environment for endpoint credentials.
- Unit tests prove target resolution, prerequisite planning, literal model rendering, provider scope, disabled-by-default output, and rejection of broad fallback execution.
- Integration tests prove that each emitted command runs only the advertised target against the deterministic mock provider.

### Non-goals

This item does not add automatic remediation, mutate provider configuration, infer credentials, or replace the full compatibility report.
