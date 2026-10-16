#!/usr/bin/env python3
"""Emit a bounded, structural-only AIQA report v3 summary.

Provider-controlled text, identifiers, URLs, findings, recommendations, and
errors are intentionally never emitted.
"""
import argparse
import json
import pathlib
import sys

MAX_REPORT_BYTES = 16 * 1024 * 1024
VERDICTS = {"pass", "pass-with-gaps", "partial", "fail", "inconclusive"}
STATUSES = {"pass", "fail", "warn", "unsupported", "skip", "error", "not_applicable"}
PROTOCOLS = {"openai", "anthropic", "google-genai"}
VERDICT_RANK = {"pass": 0, "pass-with-gaps": 1, "partial": 2, "inconclusive": 3, "fail": 4}

def invalid(message="invalid report structure"):
    raise ValueError(message)

def status_counts(items):
    if not isinstance(items, list): invalid()
    counts = {}
    for item in items:
        if not isinstance(item, dict): invalid()
        status = item.get("status")
        if not isinstance(status, str) or status not in STATUSES: invalid()
        counts[status] = counts.get(status, 0) + 1
    return counts

def summarize(data, expected_protocol=None, expected_model=None, expected_profile_id=None):
    if not isinstance(data, dict) or data.get("schema_version") != "aiqa.report.v3": invalid()
    summary, config, targets = data.get("summary"), data.get("config"), data.get("targets")
    if not isinstance(summary, dict) or not isinstance(summary.get("verdict"), str) or summary.get("verdict") not in VERDICTS or not isinstance(config, dict) or not isinstance(targets, list) or not targets: invalid()
    if expected_model is not None and config.get("requested_models") != [expected_model]: invalid("report model scope mismatch")
    if expected_protocol is not None and len(targets) != 1: invalid("report target scope mismatch")
    safe_targets = []
    for target in targets:
        if not isinstance(target, dict) or not isinstance(target.get("protocol"), str) or target.get("protocol") not in PROTOCOLS: invalid()
        target_summary = target.get("summary")
        assessment = target_summary.get("assessment") if isinstance(target_summary, dict) else None
        if not isinstance(assessment, dict) or not isinstance(assessment.get("verdict"), str) or assessment.get("verdict") not in VERDICTS: invalid()
        if expected_protocol is not None and target["protocol"] != expected_protocol: invalid("report protocol mismatch")
        if expected_profile_id is not None:
            profile = target.get("profile")
            if not isinstance(profile, dict) or profile.get("id") != expected_profile_id: invalid("report profile mismatch")
        requirements = status_counts(target.get("requirements", []))
        discovery = target.get("discovery", {})
        if not isinstance(discovery, dict): invalid()
        discovery_items = discovery.get("tests", discovery.get("compatibility", []))
        safe_targets.append({"protocol": target["protocol"], "verdict": assessment["verdict"], "requirement_statuses": requirements, "discovery_statuses": status_counts(discovery_items)})
    counts = {verdict: 0 for verdict in VERDICTS}
    for target in safe_targets: counts[target["verdict"]] = counts.get(target["verdict"], 0) + 1
    reported_counts = summary.get("target_counts")
    if not isinstance(reported_counts, dict) or any(not isinstance(key, str) or key not in VERDICTS or not isinstance(value, int) or isinstance(value, bool) or value < 0 for key, value in reported_counts.items()):
        invalid("report target counts are invalid")
    normalized_counts = {verdict: reported_counts.get(verdict, 0) for verdict in VERDICTS}
    if normalized_counts != counts or summary["verdict"] != max((target["verdict"] for target in safe_targets), key=VERDICT_RANK.get):
        invalid("report canonical verdicts are inconsistent")
    return {"schema_version": "aiqa.report.v3", "verdict": summary["verdict"], "target_count": len(safe_targets), "targets": safe_targets}

def load_and_summarize(path, **expected):
    source = pathlib.Path(path)
    if not source.is_file(): invalid("report unavailable")
    try:
        with source.open("rb") as stream: raw = stream.read(MAX_REPORT_BYTES + 1)
        if len(raw) > MAX_REPORT_BYTES: invalid("report exceeds local limit")
        data = json.loads(raw.decode("utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError, TypeError): invalid("report is not valid JSON")
    return summarize(data, **expected)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report")
    args = parser.parse_args()
    print(json.dumps(load_and_summarize(args.report), sort_keys=True))

if __name__ == "__main__":
    try: main()
    except (ValueError, TypeError, RecursionError):
        print(json.dumps({"error": "report summary unavailable"}), file=sys.stderr); sys.exit(2)
