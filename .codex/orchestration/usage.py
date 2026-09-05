"""Version-sensitive rollout adapter; documented exec JSON adapter.

Counters are observations, never a billing statement. Re-import replaces a
snapshot. Sources are kept separate and the report never adds overlapping ones.
"""
from collections import defaultdict
import json
from pathlib import Path
import time

KEYS = ("input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens")


def counters(value):
    if not isinstance(value, dict):
        raise ValueError("Missing counters")
    numbers = []
    for k in KEYS:
        n = value.get(k)
        if not isinstance(n, int) or isinstance(n, bool) or n < 0:
            raise ValueError("Unsupported counter schema")
        numbers.append(n)
    if numbers[1] > numbers[0] or numbers[3] > numbers[2]:
        raise ValueError("Counters violate subset constraints")
    return numbers


def save_snapshot(store, thread, source, rows, status, detail=""):
    with store.db:
        store.db.execute("DELETE FROM usage WHERE thread=? AND source=?", (thread, source))
        for model, nums in rows.items():
            store.db.execute("INSERT INTO usage VALUES(?,?,?,?,?,?,?,?,?)",
                             (thread, source, model, *nums, status, time.time()))
        store.db.execute("INSERT OR REPLACE INTO usage_status VALUES(?,?,?,?,?)",
                         (thread, source, status, detail, time.time()))
    return status


def import_rollout(store, path, thread):
    source = "rollout-v1-experimental"
    if not path or not Path(path).is_file():
        return save_snapshot(store, thread, source, {}, "missing_transcript")
    p = Path(path)
    if p.stat().st_size > store.policy.get("rollout_max_bytes", 33554432):
        return save_snapshot(store, thread, source, {}, "size_limit")
    # Only counters and model context are extracted; no transcript copy is made.
    model = "unknown"
    previous = [0, 0, 0, 0]
    totals = defaultdict(lambda: [0, 0, 0, 0])
    observed = 0
    header = None
    malformed = 0
    try:
        with p.open(encoding="utf-8") as f:
            for line in f:
                try:
                    e = json.loads(line)
                except ValueError:
                    malformed += 1
                    continue
                if not isinstance(e, dict):
                    malformed += 1
                    continue
                payload = e.get("payload", {})
                if not isinstance(payload, dict):
                    continue
                if e.get("type") == "session_meta":
                    found = payload.get("id")
                    if header is not None and found != header:
                        return save_snapshot(store, thread, source, {}, "mixed_threads")
                    header = found
                    if payload.get("forked_from_id") or payload.get("forked_from"):
                        return save_snapshot(store, thread, source, {}, "fork_counters_unverified")
                if e.get("type") == "turn_context":
                    model = payload.get("model") or "unknown"
                if e.get("type") != "event_msg" or payload.get("type") != "token_count":
                    continue
                info = payload.get("info")
                if info is None:
                    continue  # Rate-limit-only or empty update.
                current = counters(info.get("total_token_usage"))
                delta = [a - b for a, b in zip(current, previous)]
                if min(delta) < 0 or delta[1] > delta[0] or delta[3] > delta[2]:
                    return save_snapshot(store, thread, source, {}, "counter_reset_or_inconsistent_delta")
                # Cumulative snapshots repeat: only deltas are accumulated.
                if any(delta):
                    totals[model] = [a + b for a, b in zip(totals[model], delta)]
                previous = current
                observed += 1
    except (ValueError, UnicodeError, TypeError, AttributeError):
        return save_snapshot(store, thread, source, {}, "unsupported_schema")
    if header != thread:
        return save_snapshot(store, thread, source, {}, "thread_identity_unverified")
    if not observed:
        return save_snapshot(store, thread, source, {}, "no_usage_events")
    if malformed:
        return save_snapshot(store, thread, source, totals, "partial", f"malformed_lines={malformed}")
    return save_snapshot(store, thread, source, totals, "observed",
                         "Unique thread snapshot; final sample may precede the final response flush")


def import_exec(store, thread, usage_events, model, complete=True):
    totals = [0, 0, 0, 0]
    try:
        for value in usage_events:
            nums = counters(value)
            totals = [a + b for a, b in zip(totals, nums)]
    except ValueError:
        return save_snapshot(store, thread, "exec-json-root", {}, "unsupported_schema")
    if not usage_events:
        return save_snapshot(store, thread, "exec-json-root", {}, "no_usage_events")
    return save_snapshot(store, thread, "exec-json-root", {model: totals},
                         "observed" if complete else "partial",
                         "Root stream only; model is requested unless independently observed")
