#!/usr/bin/env python3
"""Safely plan or run one minimal AIQA provider compatibility check."""
import argparse
import ipaddress
import json
import os
import pathlib
import re
import stat
import subprocess
import sys
import tempfile
import urllib.parse
from extract_report import load_and_summarize

KEYS = {"openai": ("OPENAI_API_KEY", "OPENAI_BASE_URL"), "anthropic": ("ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL"), "google-genai": ("GOOGLE_GENAI_API_KEY", "GOOGLE_GENAI_BASE_URL")}
GOOGLE_PROFILES = {"google-genai-generate-content-compat-2026-09.json", "google-genai-compat-2026-09.json"}
GOOGLE_PROFILE_IDS = {"google-genai-generate-content-compat-2026-09.json": "google-genai-generate-content-compat", "google-genai-compat-2026-09.json": "google-genai-compat"}
CONTROL = re.compile(r"[\x00-\x1f\x7f]")

def fail(message): raise ValueError(message)

def posix_safe(path, label):
    if os.name != "posix": fail("run_check.py enforces permission guarantees only on POSIX; use the documented Windows manual workflow")
    resolved = pathlib.Path(path).resolve(strict=True)
    if not resolved.is_file() or not os.access(resolved, os.X_OK): fail(f"{label} must be an executable regular file")
    # The binary itself must be private or root-owned and never group/world writable.
    info = resolved.stat()
    if info.st_mode & (stat.S_IWGRP | stat.S_IWOTH): fail(f"unsafe {label} permissions")
    return resolved

def validate_url(raw):
    if len(raw) > 2048 or CONTROL.search(raw): fail("base URL is invalid")
    value = urllib.parse.urlsplit(raw)
    if value.scheme not in {"https", "http"} or not value.netloc or value.username or value.password or value.query or value.fragment: fail("base URL must be an origin/path without credentials, query, or fragment")
    if value.scheme == "http":
        try: local = ipaddress.ip_address(value.hostname).is_loopback
        except ValueError: local = value.hostname.lower() == "localhost" if value.hostname else False
        if not local: fail("HTTP is allowed only for localhost or numeric loopback tests")
    return raw

def validate_model(value):
    if not value or len(value) > 256 or "," in value or CONTROL.search(value) or any(ord(c) in range(0x80, 0xA0) or ord(c) in {0x061C, 0x200E, 0x200F, 0x2028, 0x2029} or ord(c) in range(0x202A, 0x202F) or ord(c) in range(0x2066, 0x206A) for c in value): fail("model must be one bounded non-control ID")
    return value

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--aiqa", required=True); p.add_argument("--target", required=True, choices=KEYS)
    p.add_argument("--base-url", required=True); p.add_argument("--model", required=True); p.add_argument("--profile")
    p.add_argument("--sdk", action="store_true"); p.add_argument("--name", default="report")
    p.add_argument("--execute", action="store_true", help="authorized: verify and run the fixed local command")
    args = p.parse_args()
    if not pathlib.PurePath(args.aiqa).is_absolute(): fail("--aiqa must be an absolute trusted binary path")
    if pathlib.PurePath(args.name).name != args.name or "/" in args.name or "\\" in args.name or not args.name or len(args.name) > 80: fail("--name must be a bounded output basename")
    validate_url(args.base_url); validate_model(args.model)
    if args.profile and (args.target != "google-genai" or args.profile not in GOOGLE_PROFILES): fail("--profile is restricted to reviewed Google profiles")
    if args.target == "google-genai" and not args.profile: args.profile = "google-genai-generate-content-compat-2026-09.json"
    key, base_env = KEYS[args.target]
    tests = "compat,sdk" if args.sdk else "compat"
    argv = [args.aiqa, f"test={tests}", f"--target={args.target}", f"--model={args.model}", "--format=json", f"--name={args.name}", "--timeout=30s", "--run-timeout=300s", "--queue-timeout=30s", "--concurrency=1", "--target-concurrency=1", "--request-interval=1s", "--max-catalog-models=256", "--max-selected-models=1", "--inventory-byte-budget=1MiB", "--fail-on=core", "--progress=off", "--color=never", "--unicode=never"]
    if args.target == "google-genai": argv += [f"--google-genai-profile={args.profile}"]
    plan = {"mode": "execute" if args.execute else "plan", "target": args.target, "model": args.model, "profile": args.profile, "credential_env": key, "base_url_env": base_env, "argv": argv, "limits": {"request_seconds": 30, "run_seconds": 300, "queue_seconds": 30, "concurrency": 1}}
    if not args.execute: print(json.dumps(plan, sort_keys=True)); return
    if not os.environ.get(key): fail(f"selected canonical credential {key} is not set")
    binary = posix_safe(args.aiqa, "AIQA binary")
    version = subprocess.run([str(binary), "--version"], capture_output=True, text=True, timeout=10, env={"PATH": os.defpath})
    if version.returncode != 0 or not re.search(r"(?<![0-9.])1\.0\.0-beta\.1(?![0-9.])", version.stdout + version.stderr): fail("trusted AIQA binary is not version 1.0.0-beta.1")
    previous_umask = os.umask(0o077)
    work = pathlib.Path(tempfile.mkdtemp(prefix="aiqa-operator-")); os.chmod(work, 0o700)
    artifacts = work / "artifacts"; artifacts.mkdir(mode=0o700)
    env = {"PATH": os.defpath, "LANG": "C", "LC_ALL": "C", "TZ": "UTC", key: os.environ[key], base_env: args.base_url}
    stdout_path, stderr_path = artifacts / "stdout.log", artifacts / "stderr.log"
    try:
        with stdout_path.open("x", encoding="utf-8") as out, stderr_path.open("x", encoding="utf-8") as err:
            os.chmod(stdout_path, 0o600); os.chmod(stderr_path, 0o600)
            run = subprocess.Popen([str(binary), *argv[1:]], cwd=artifacts, env=env, stdout=out, stderr=err)
            try: code = run.wait(timeout=310)
            except subprocess.TimeoutExpired: run.kill(); run.wait(); fail("outer process guard expired")
    except OSError: fail("AIQA process could not be started")
    else:
        report = artifacts / f"{args.name}.json"
        if not report.is_file(): fail("AIQA did not write the expected report")
        os.chmod(report, 0o600)
        try:
            extracted = load_and_summarize(report, expected_protocol=args.target, expected_model=args.model, expected_profile_id=GOOGLE_PROFILE_IDS.get(args.profile))
        except ValueError: fail("AIQA report did not satisfy the structural contract")
        if code not in {0, 1, 2}: fail("AIQA returned an unsupported exit status")
        plan.update({"exit_code": code, "artifact_dir": str(artifacts), "report": f"{args.name}.json", "report_summary": extracted, "completion": "report-inspected"})
        print(json.dumps(plan, sort_keys=True))
        return code
    finally:
        os.umask(previous_umask)

if __name__ == "__main__":
    try: sys.exit(main() or 0)
    except (ValueError, OSError, subprocess.SubprocessError, json.JSONDecodeError):
        print(json.dumps({"error": "operator check did not complete"}), file=sys.stderr); sys.exit(2)
