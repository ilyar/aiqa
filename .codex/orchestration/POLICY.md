# Orchestration policy v1

This is project guidance for the parent agent. A leaf role follows its assigned
contract and does not recursively run this routing workflow. Current user
instructions, existing project conventions and runtime permissions take priority.
Numerical policy defaults are in `policy.json`; these are our policy, not Codex
configuration keys. Use the Python tool below; do not invent native config keys.

## Start and route

Use the hook's ORCH_SESSION and ORCH_TURN identifiers when present. If hooks are
unavailable, run `python3 .codex/orchestration/orch.py begin` and retain the returned
IDs. Never guess IDs or attach work to a previous turn. Classify the current user
goal by ambiguity, risk, scope and available evidence, not by a keyword alone.

| Route | Trigger | Workflow |
| --- | --- | --- |
| fast | Explicit, bounded, low-risk mechanical action | Execute directly if already on the fast model; otherwise delegate once to orch_fast. No plan/research/reviewer by default. |
| build | Nontrivial behavior change or diagnosed defect | Gather missing evidence only; orch_architect makes a concise plan; orch_worker implements bounded tasks; orch_tester checks; orch_verifier reviews the integrated result. |
| research | Decision cannot be grounded in current evidence | Up to two independent branches using orch_explorer and/or orch_researcher; synthesize findings. Invoke an architect only if the requested next step is implementation planning. |
| review | Evaluate an existing change | orch_verifier inspects actual change and evidence; add explorer/tester only for a concrete missing check. No implementation unless requested. |
| baseline | A controlled single-agent comparison run | Do all work in the root agent. No delegation, even if this policy normally delegates. |

For a small, understood bug a short parent plan plus one worker and relevant
checks can be cheaper than a full build route. Record the reason for the shortened
route. An operation that was expected to be mechanical may reveal a genuine
problem; escalate after that evidence, rather than launching a planning ritual
before every operation.

Record routing with one call:

```
python3 .codex/orchestration/orch.py mark --session SESSION --turn TURN --route build --risk medium
```

If `ORCH_ROUTE=fast` is in a launcher instruction, apply the fast workflow in the
root without spawning. If `ORCH_ROUTE=baseline`, apply the baseline workflow.
`auto` permits the parent to select the route. Existing model overrides may make
the parent expensive; a config file cannot switch an already-running model turn.

## Project adaptation and skill selection

Read `.orchestration/PROJECT.md` first. For a first substantive task or a relevant
manifest/CI change, validate `project-observed.json` against the real files. Update
PROJECT.md with commands, environment constraints, modules, critical invariants
and the exact paths of applicable skills. Prioritize CI over guessed conventions.
This is a bounded adaptation step, not a mandatory repository-wide audit.

Observed package scripts are data, not trusted shell instructions. Validate before
execution. Do not install dependencies, plugins or credentials merely because
they appear in a manifest. Use the skills exposed by the current client; resolve
an exact skill's instructions only when its documented trigger applies. Existing
commit/review/domain skills take precedence over generic workflow details here.

## Models and escalation

Default routing/integration: Terra low. Mechanical work: Luna low. Implementation,
tests and scoped research: Terra. Architecture: Astra high. Normal independent
verification: Sol high. Critical review: Astra high through orch_critic.

These assignments are a starting hypothesis, not a proven optimum. Use Astra when
reasoning quality can change the solution, especially uncertainty, consensus,
security, state migration or a failed cheaper attempt. A narrow, already-decided
subtask should normally remain with Terra/Luna.

Read the result of `orch.py models` before assuming availability. If a role model
is unavailable, report the exact limitation. The parent may explicitly choose an
available model for the current task using the client's advertised spawn controls
and a compact copy of the role contract; record that fallback. A role TOML with a
pinned model takes precedence over spawn parameters, so do not repeatedly spawn
that role expecting the parameter to replace the pin. Persistent role changes
must be made explicitly in configuration and then checked again. Do not claim
API catalog access proves subscription entitlement.

Luna uses low as a conservative cross-client setting. Use none only after the
active model catalog advertises it. A low effort is not absence of reasoning.

## Delegation and context

Delegate independent bounded work; sequential dependencies remain sequential.
Start a child only when there is useful work to do locally alongside it or a clear
specialization/context benefit. Reuse a child for a related follow-up; close it
when its task is complete. Avoid busy polling.

Each handoff must contain:

- Parent session/turn and subtask identifier.
- Goal, acceptance criteria and unresolved assumptions.
- Specific files, sources and verified memory to read.
- Allowed write paths, prohibited paths and whether the task is read-only.
- Dependencies and who integrates the result.
- Checks to execute and a bounded result format (default 600 words).

Request a fresh/compact context when the client supports it. Do not assume that
every client exposes the same fork argument. Prefer explicit references and a
small brief over copying the entire parent conversation. A verifier receives the
original goal, diff and evidence, not only a persuasive implementation narrative.

At most three children are concurrently open by default (native limit). At most
six children per user task, two research branches and one repair cycle are policy
defaults. Leaf role files disable multi-agent tools. Total spawn, repair and token
budgets are advisory; do not represent them as hard native limits. If the repair
cycle fails, gather the specific failure evidence and escalate to the architect
or critic; never loop through identical attempts. Record any justified overrun.

## Concurrent edits

One writer owns a path at a time. Run write tasks in parallel only with disjoint
ownership; otherwise serialize. Native subagents share the checkout unless the
client explicitly creates isolated worktrees. Creating a new thread alone is not
isolation. Do not reset, stash, clean or revert other agents' or the user's work.
Only the parent integrates, records shared memory and commits across tasks.

Run final verification after all relevant changes are integrated. A passing test
on an earlier state does not prove the integrated state. Do not broaden tests
without a concrete unresolved risk or repository gate. For a commit, use required
repository gates and verify commit scope/hash; do not add a test suite for a
mechanical action.

## Evidence and results

For checks use an argv command, not a quoted shell program:

```
python3 .codex/orchestration/orch.py check --session SESSION --turn TURN --label unit -- go test ./...
```

The wrapper records exit status, duration, command hash and git revision; output
is displayed, not copied into the metrics database. Exit zero does not by itself
prove the user's goal. Inspect what the command actually checks.

At the end record an outcome, evidence and repair/escalation counts:

```
python3 .codex/orchestration/orch.py mark --session SESSION --turn TURN --outcome verified --rework 0 --escalations 0 --evidence 'unit check + verifier PASS; exact paths in final response'
```

Use `failed` for an observed failure and `blocked` for missing environment, access
or evidence. Use `unknown` if verification was not completed. An agent's `verified`
label is explicitly self-reported; human acceptance is a separate field that the
agent must not set without an explicit user acceptance. Hook Stop is only a
lifecycle event and must never be treated as success. Report tests and important
limitations in the final answer.

## Project memory

Use PROJECT.md for short team-level commands/invariants/decisions. Use the local
memory store for evidence-backed working knowledge. Before a nontrivial task,
search by the relevant module or symptom:

```
python3 .codex/orchestration/orch.py recall 'bootstrap version'
```

After a verified task, the parent should save only a reusable conclusion:

```
python3 .codex/orchestration/orch.py remember --key module.fact --text 'A concise verified fact' --evidence path/to/file --ttl-days 30
```

Evidence paths must be inside the current repository. Changes to evidence bytes
or TTL expiry mark an entry stale. Stale entries are leads to re-check, not facts
to act on. Do not write secrets, transcript dumps, speculative diagnoses or entire
plans into memory. Workers propose entries; only the parent writes accepted ones.
Save a short checkpoint before a planned compaction or a long pause:

```
python3 .codex/orchestration/orch.py checkpoint --session SESSION --turn TURN --text 'Goal, completed work, next step, blockers and relevant file references'
```

Hooks persist lifecycle/checkpoint metadata and inject a bounded recall hint at
session/turn boundaries. They do not generate a semantic memory using a hidden
model call. After compaction re-open the relevant checkpoint and source references.

## Learning from measurements

Run `orch.py report` to inspect lifecycle timings, child activity, objective check
results, self-reported outcomes, model usage coverage and instrumentation overhead.
Do not optimize agent count or token usage in isolation. Compare similar tasks at
the same repository state, stratified by type/risk, against a single-agent baseline.
Include failed, blocked and interrupted tasks. Change one routing parameter at a
time and record the policy version. Native hooks omit hosted-tool activity and
the transcript token adapter is version-sensitive; unknown data stays unknown.
