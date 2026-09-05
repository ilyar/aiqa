"""Report observed events without conflating completion and correctness."""
from collections import defaultdict
import json
import math
import statistics


def percentile(values, q):
    if not values:
        return None
    v = sorted(values)
    pos = (len(v) - 1) * q
    low, high = math.floor(pos), math.ceil(pos)
    return v[low] + (v[high] - v[low]) * (pos - low)


def collect(store):
    events = [dict(r) for r in store.db.execute("SELECT * FROM events ORDER BY ts,id")]
    tasks = [dict(r) for r in store.db.execute("SELECT * FROM tasks")]
    checks = [dict(r) for r in store.db.execute("SELECT * FROM checks")]
    groups = defaultdict(list)
    children = defaultdict(list)
    overhead = []
    for e in events:
        e["data"] = json.loads(e["data"])
        groups[e["session"], e["turn"]].append(e)
        if e["agent"] and e["kind"] in {"SubagentStart", "SubagentStop"}:
            children[e["agent"]].append(e)
        if e["kind"] == "hook_overhead":
            overhead.append(e["data"].get("ms", 0))
    for t in tasks:
        es = groups[t["session"], t["turn"]]
        starts = [e["ts"] for e in es if e["kind"] in {"UserPromptSubmit", "manual_begin", "exec_started"}]
        stops = [e["ts"] for e in es if e["kind"] in {"Stop", "exec_finished", "Interrupt", "manual_end"}]
        t["elapsed_s"] = max(stops) - min(starts) if starts and stops and max(stops) >= min(starts) else None
        t["interrupted"] = any(e["kind"] == "Interrupt" for e in es)
        t["subagents"] = len({e["agent"] for e in es if e["kind"] == "SubagentStart" and e["agent"]})
        t["compactions"] = sum(e["kind"] == "PreCompact" for e in es)
        t["tool_calls_observed"] = sum(e["kind"] == "PreToolUse" for e in es)
        cs = [c for c in checks if (c["session"], c["turn"]) == (t["session"], t["turn"])]
        t["checks_passed"] = sum(c["exit_code"] == 0 for c in cs)
        t["checks_failed"] = sum(c["exit_code"] != 0 for c in cs)
    child_rows = []
    for agent, es in children.items():
        starts = [e for e in es if e["kind"] == "SubagentStart"]
        stops = [e for e in es if e["kind"] == "SubagentStop"]
        child_rows.append({"agent": agent, "role": es[0]["role"], "model": es[0]["model"],
                           "session": es[0]["session"], "turn": es[0]["turn"],
                           "elapsed_s": max(e["ts"] for e in stops) - min(e["ts"] for e in starts) if starts and stops else None,
                           "stop_observed": bool(stops)})
    usage = [dict(r) for r in store.db.execute("SELECT * FROM usage")]
    statuses = [dict(r) for r in store.db.execute("SELECT * FROM usage_status")]
    # Per-thread precedence: never add documented exec counters to rollout counters.
    usable_exec = {r["thread"] for r in usage if r["source"] == "exec-json-root" and r["status"] == "observed"}
    selected = [r for r in usage if (r["thread"] in usable_exec and r["source"] == "exec-json-root") or
                (r["thread"] not in usable_exec and r["source"] != "exec-json-root")]
    # An interrupted exec run can still offer a partial observation if no rollout exists.
    selected_threads = {r["thread"] for r in selected}
    selected += [r for r in usage if r["source"] == "exec-json-root" and r["thread"] not in selected_threads]
    expected = {t["session"] for t in tasks} | set(children)
    seen = {r["thread"] for r in selected if r["status"] == "observed"}
    return {"schema_version": 1, "tasks": tasks, "children": child_rows, "checks": checks,
            "usage": selected, "usage_status": statuses,
            "coverage": {"threads_expected": len(expected), "threads_with_observed_snapshot": len(expected & seen),
                         "missing_threads": sorted(expected - seen),
                         "claim": "snapshot coverage, not proof of complete billing or final-response coverage"},
            "hook_handler_ms": {"count": len(overhead), "sum": sum(overhead),
                                "p50": percentile(overhead, .5), "p95": percentile(overhead, .95)}}


def fmt(value):
    return "—" if value is None else f"{value:.2f}"


def esc(value):
    return str(value if value is not None else "unknown").replace("|", "\\|").replace("\n", " ")


def markdown(report):
    tasks = report["tasks"]
    lines = ["# Codex Orchestra: local measurements", "",
             "Empty values are unknown. `verified` is an agent annotation; acceptance is recorded separately.", "",
             "| Route / risk / config | Tasks | Measured duration | p50 s | p95 s | Verified / known outcomes | Unknown outcomes | Repair count known |",
             "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"]
    groups = defaultdict(list)
    for t in tasks:
        groups[t["route"], t["risk"], t["policy_hash"]].append(t)
    for key, group in sorted(groups.items()):
        durations = [t["elapsed_s"] for t in group if t["elapsed_s"] is not None]
        known = sum(t["outcome"] != "unknown" for t in group)
        verified = sum(t["outcome"] == "verified" for t in group)
        repair_known = sum(t["rework"] is not None for t in group)
        lines.append(f"| {' / '.join(esc(k) for k in key)} | {len(group)} | {len(durations)} | {fmt(percentile(durations,.5))} | {fmt(percentile(durations,.95))} | {verified}/{known} | {len(group)-known} | {repair_known}/{len(group)} |")
    if not tasks:
        lines.append("| No recorded tasks | 0 | 0 | — | — | 0/0 | 0 | 0/0 |")
    verified = [t for t in tasks if t["outcome"] == "verified" and t["rework"] is not None]
    first = sum(t["rework"] == 0 for t in verified)
    lines += ["", f"First-pass completion among verified tasks with a known repair count: {first}/{len(verified)} (self-reported).",
              f"Human acceptance: yes={sum(t['human']=='yes' for t in tasks)}, no={sum(t['human']=='no' for t in tasks)}, unknown={sum(t['human']=='unknown' for t in tasks)}.",
              f"Interrupted tasks: {sum(t['interrupted'] for t in tasks)}. Unknown or interrupted runs remain in the report.", "",
              f"Recorded checks: passed={sum(c['exit_code']==0 for c in report['checks'])}, failed={sum(c['exit_code']!=0 for c in report['checks'])}.",
              "A command's exit status does not establish coverage of the user's acceptance criteria.", "",
              "| Child | Role | Model observed at lifecycle boundary | Lifetime s | Stop observed |",
              "| --- | --- | --- | ---: | --- |"]
    for c in report["children"]:
        lines.append(f"| {esc(c['agent'])} | {esc(c['role'])} | {esc(c['model'])} | {fmt(c['elapsed_s'])} | {c['stop_observed']} |")
    cov = report["coverage"]
    lines += ["", "Child lifetimes can overlap and include waiting. Their sum is not wall time or measured speedup.", "",
              f"Token snapshot coverage: **{cov['threads_with_observed_snapshot']}/{cov['threads_expected']} observed threads**. Final-response flush and unobserved children can still leave gaps.", "",
              "| Thread | Source | Model attribution | Input | Cached subset | Output | Reasoning subset | Status |",
              "| --- | --- | --- | ---: | ---: | ---: | ---: | --- |"]
    for u in report["usage"]:
        lines.append(f"| {esc(u['thread'])} | {esc(u['source'])} | {esc(u['model'])} | {u['input']} | {u['cached']} | {u['output']} | {u['reasoning']} | {u['status']} |")
    lines += ["", "Input already contains cached tokens; output already contains reasoning tokens. Do not add the subset columns again.",
              "Usage is by thread, not allocated to individual tasks or roles after model rerouting. No dollar total is inferred from subscription quota or incomplete counters."]
    bad = [r for r in report["usage_status"] if r["status"] != "observed"]
    if bad:
        lines += ["", "Adapter gaps: " + "; ".join(esc(r["thread"]) + "=" + esc(r["status"]) for r in bad)]
    h = report["hook_handler_ms"]
    lines += ["", f"Hook handler time: n={h['count']}, total={fmt(h['sum'])} ms, p50={fmt(h['p50'])} ms, p95={fmt(h['p95'])} ms.",
              "Handler timing excludes Python startup, database opening and host scheduling; it is not full instrumentation overhead.", "",
              "Compare matched tasks at the same revision and include failures. Change one policy parameter at a time. Small samples and unknown outcomes are not evidence of an improvement.", ""]
    return "\n".join(lines)
