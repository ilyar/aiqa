#!/usr/bin/env python3
"""Project orchestration utilities. Run --help for the supported commands."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import tomllib
import uuid

from core import VERSION, Store, atomic_json, compact, digest, git, handle_hook
from report import collect, markdown
from usage import import_exec, import_rollout


def emit(value):
    print(json.dumps(value, ensure_ascii=False, indent=2))


def positive(value):
    number = int(value)
    if number < 0:
        raise argparse.ArgumentTypeError("Expected a nonnegative integer")
    return number


def catalog(binary="codex", timeout=30):
    """Read-only app-server handshake + paginated model/list. No inference."""
    proc = subprocess.Popen([binary, "app-server"], stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            text=True, bufsize=1)
    replies = queue.Queue()

    def reader():
        for line in proc.stdout:
            try:
                replies.put(json.loads(line))
            except ValueError:
                pass
        replies.put(None)

    threading.Thread(target=reader, daemon=True).start()
    deadline = time.monotonic() + timeout

    def send(message):
        proc.stdin.write(compact(message) + "\n")
        proc.stdin.flush()

    def request(number, method, params):
        send({"id": number, "method": method, "params": params})
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("model/list timeout")
            try:
                msg = replies.get(timeout=remaining)
            except queue.Empty:
                raise TimeoutError("model/list timeout") from None
            if msg is None:
                raise RuntimeError("app-server ended before model/list completed")
            if msg.get("id") == number:
                if "error" in msg:
                    raise RuntimeError("app-server returned an error; check Codex version and sign-in")
                return msg.get("result", {})

    try:
        request(0, "initialize", {"clientInfo": {"name": "codex_orchestra", "title": "Project orchestra", "version": VERSION}})
        send({"method": "initialized", "params": {}})
        result, cursor, number = [], None, 1
        cursors = set()
        while True:
            params = {"limit": 100, "includeHidden": True}
            if cursor is not None:
                params["cursor"] = cursor
            page = request(number, "model/list", params)
            result.extend(page.get("data", []))
            cursor = page.get("nextCursor")
            if not cursor:
                break
            if cursor in cursors:
                raise RuntimeError("Repeated model catalog cursor")
            cursors.add(cursor)
            number += 1
        return result
    finally:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
        proc.stdin.close()
        proc.stdout.close()


def doctor(store, models=None):
    findings = []
    def finding(name, okay, detail):
        findings.append({"check": name, "ok": okay, "detail": detail})
    config_file = store.root / ".codex/config.toml"
    configs = []
    try:
        config = tomllib.loads(config_file.read_text())
        configs.append(("root", config))
        finding("config TOML", True, "Parsed; this is not native Codex schema validation")
    except (OSError, ValueError) as e:
        finding("config TOML", False, type(e).__name__)
        config = {}
    ignored = sorted(set(config) & {"otel", "profiles", "profile", "notify", "model_provider", "model_providers", "openai_base_url", "chatgpt_base_url"})
    finding("project scope", not ignored, "Ignored project keys: " + ", ".join(ignored) if ignored else "No known machine-only keys in project config")
    for p in sorted((store.root / ".codex/agents").glob("orch_*.toml")):
        try:
            c = tomllib.loads(p.read_text())
            valid = all(c.get(k) for k in ("name", "description", "developer_instructions", "model", "model_reasoning_effort"))
            valid &= c.get("agents", {}).get("enabled") is False
            valid &= c.get("features", {}).get("multi_agent_v2") is False
            finding(p.name, bool(valid), "Role fields + leaf delegation switch")
            configs.append((c.get("name", p.stem), c))
        except (ValueError, OSError) as e:
            finding(p.name, False, type(e).__name__)
    try:
        hooks = json.loads((store.root / ".codex/hooks.json").read_text())
        wanted = {"SessionStart", "UserPromptSubmit", "SubagentStart", "SubagentStop", "Stop", "PreToolUse", "PostToolUse", "PreCompact", "Interrupt"}
        finding("hook declarations", wanted <= set(hooks.get("hooks", {})), "Presence only; /hooks trust and actual execution must be checked in Codex")
    except (ValueError, OSError) as e:
        finding("hook declarations", False, type(e).__name__)
    if models is not None:
        by_model = {m.get("model"): m for m in models}
        for name, c in configs:
            m = by_model.get(c.get("model"))
            efforts = {e.get("reasoningEffort") for e in m.get("supportedReasoningEfforts", [])} if m else set()
            okay = bool(m) and c.get("model_reasoning_effort") in efforts
            finding("model:" + name, okay, "Advertised by the current client catalog" if okay else "Model/effort absent; no automatic fallback performed")
    else:
        finding("model entitlement", None, "Not checked. Run models to query this installation's catalog")
    try:
        version = subprocess.run(["codex", "--version"], capture_output=True, text=True, timeout=5)
        finding("Codex CLI", version.returncode == 0, version.stdout.strip())
    except (OSError, subprocess.TimeoutExpired):
        finding("Codex CLI", None, "Unavailable here; live startup and inference have not been tested")
    finding("hook activity", None, f"{store.db.execute('SELECT COUNT(*) FROM events').fetchone()[0]} stored events; inspect report after a live task")
    return findings


def run_task(store, args):
    prompt = Path(args.task_file).read_text() if args.task_file else sys.stdin.read()
    if not prompt.strip():
        raise ValueError("Provide a nonempty task file or pipe a task to stdin")
    mode = store.policy.get("fast" if args.route == "fast" else "baseline" if args.route == "baseline" else "root", {})
    model, effort = mode.get("model", "gpt-5.6-terra"), mode.get("effort", "low")
    request = f"ORCH_ROUTE={args.route}. Follow the applicable project instructions.\n\n{prompt}"
    cmd = [args.codex, "exec", "--json", "--sandbox", args.sandbox,
           "-m", model, "-c", "model_reasoning_effort=" + json.dumps(effort)]
    if args.route in {"fast", "baseline"}:
        cmd += ["-c", "agents.enabled=false", "-c", "features.multi_agent_v2=false"]
    cmd.append("-")
    started = time.time()
    proc = subprocess.Popen(cmd, cwd=store.root, stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, text=True, bufsize=1)
    # A separate writer avoids a pipe deadlock for large task files.
    def write_prompt():
        try:
            proc.stdin.write(request)
            proc.stdin.close()
        except (BrokenPipeError, OSError):
            pass
    threading.Thread(target=write_prompt, daemon=True).start()
    thread, usage_events, failed, malformed = None, [], False, 0
    try:
        for line in proc.stdout:
            try:
                event = json.loads(line)
            except ValueError:
                malformed += 1
                continue
            kind = event.get("type")
            if kind == "thread.started":
                thread = event.get("thread_id")
            elif kind == "turn.completed" and isinstance(event.get("usage"), dict):
                usage_events.append(event["usage"])
            elif kind in {"turn.failed", "error"}:
                failed = True
            item = event.get("item", {})
            if kind == "item.completed" and item.get("type") == "agent_message":
                print(item.get("text", ""), flush=True)
        code = proc.wait()
    except KeyboardInterrupt:
        proc.terminate()
        try:
            proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        failed, code = True, 130
    finally:
        proc.stdout.close()
    ended = time.time()
    if thread:
        # Prefer the real hook IDs; make an explicit fallback only without hooks.
        rows = list(store.db.execute("SELECT turn FROM tasks WHERE session=?", (thread,)))
        if len(rows) == 1:
            turn = rows[0]["turn"]
        else:
            turn = "exec-" + uuid.uuid4().hex
            store.task(thread, turn)
        store.event("exec_started", thread, turn, model=model, data={"requested_effort": effort}, ts=started)
        store.event("exec_finished", thread, turn, model=model, data={"exit_code": code, "malformed_lines": malformed}, ts=ended)
        if args.route != "auto":
            store.mark(thread, turn, {"route": args.route})
        import_exec(store, thread, usage_events, "requested:" + model, complete=(code == 0 and not failed and not malformed))
    else:
        print("Orchestra: no thread.started was observed; usage is unknown.", file=sys.stderr)
    return code if code else (1 if failed else 0)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=str(Path(__file__).resolve().parents[2]), help="Project root (normally inferred from installation)")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("hook", help="Read native hook JSON from stdin")
    sub.add_parser("begin", help="Begin a manual task if native hooks are absent")
    sub.add_parser("adapt", help="Discover project manifests/CI/skills without executing them")
    sub.add_parser("doctor", help="Offline configuration and installation checks")
    p = sub.add_parser("models", help="Query the installed Codex model catalog without inference")
    p.add_argument("--codex", default="codex")
    p = sub.add_parser("mark", help="Annotate a route, outcome or explicit human acceptance")
    p.add_argument("--session", required=True); p.add_argument("--turn", required=True)
    p.add_argument("--route", choices=["auto", "fast", "build", "research", "review", "baseline"])
    p.add_argument("--risk", choices=["low", "medium", "high"])
    p.add_argument("--outcome", choices=["unknown", "verified", "blocked", "failed"])
    p.add_argument("--rework", type=positive); p.add_argument("--escalations", type=positive)
    p.add_argument("--human", choices=["yes", "no", "unknown"])
    p.add_argument("--evidence"); p.add_argument("--end", action="store_true", help="End a manual task")
    p = sub.add_parser("check", help="Run an explicit verification command as argv")
    p.add_argument("--session", required=True); p.add_argument("--turn", required=True)
    p.add_argument("--label", required=True)
    p.add_argument("argv", nargs=argparse.REMAINDER)
    p = sub.add_parser("remember", help="Save a concise conclusion with local file evidence")
    p.add_argument("--key", required=True); p.add_argument("--text", required=True)
    p.add_argument("--evidence", action="append", required=True)
    p.add_argument("--ttl-days", type=float, default=30)
    p = sub.add_parser("recall", help="Retrieve relevant memory and mark stale entries")
    p.add_argument("query", nargs="?", default="")
    p = sub.add_parser("checkpoint", help="Save a compact per-session continuation note")
    p.add_argument("--session", required=True); p.add_argument("--turn", required=True)
    p.add_argument("--text", required=True)
    p = sub.add_parser("import-rollout", help="Refresh an experimental token snapshot after a session flush")
    p.add_argument("--thread", required=True); p.add_argument("path")
    p = sub.add_parser("report", help="Print measured observations and data coverage")
    p.add_argument("--json", action="store_true")
    p = sub.add_parser("run", help="Run Codex exec with root-stream usage capture")
    p.add_argument("--route", choices=["auto", "fast", "baseline"], default="auto")
    p.add_argument("--task-file"); p.add_argument("--codex", default="codex")
    p.add_argument("--sandbox", choices=["read-only", "workspace-write"], default="read-only")
    args = parser.parse_args()
    store = None
    try:
        store = Store(args.root)
        if args.command == "hook":
            emit(handle_hook(store, json.load(sys.stdin)))
        elif args.command == "begin":
            session, turn = "manual-" + uuid.uuid4().hex, uuid.uuid4().hex
            store.task(session, turn); store.event("manual_begin", session, turn)
            emit({"session": session, "turn": turn})
        elif args.command == "adapt":
            emit(store.adapt())
        elif args.command == "doctor":
            result = doctor(store); emit(result)
            return int(any(r["ok"] is False for r in result))
        elif args.command == "models":
            models = catalog(args.codex)
            result = {"observed_at": time.time(), "models": models, "checks": doctor(store, models)}
            atomic_json(store.state / "models.json", result); emit(result)
            return int(any(r["ok"] is False for r in result["checks"]))
        elif args.command == "mark":
            if args.outcome == "verified" and not args.evidence:
                raise ValueError("verified requires --evidence; still an agent annotation, not human acceptance")
            store.mark(args.session, args.turn, vars(args))
            if args.end:
                store.event("manual_end", args.session, args.turn)
            emit({"recorded": True})
        elif args.command == "check":
            argv = args.argv[1:] if args.argv[:1] == ["--"] else args.argv
            if not argv:
                raise ValueError("Provide a command after --")
            store.task(args.session, args.turn)
            start = time.time()
            try:
                p = subprocess.run(argv, cwd=store.root, check=False)
                code = p.returncode
            except OSError:
                code = 127
            except KeyboardInterrupt:
                code = 130
            check_id = uuid.uuid4().hex
            with store.db:
                store.db.execute("INSERT INTO checks VALUES(?,?,?,?,?,?,?,?,?)",
                                 (check_id, start, args.session, args.turn, args.label[:200], time.time()-start, code,
                                  digest(compact(argv)), git(store.root, "rev-parse", "HEAD")))
            emit({"check_id": check_id, "exit_code": code})
            return code if code >= 0 else 128 - code
        elif args.command == "remember":
            store.remember(args.key, args.text, args.evidence, args.ttl_days); emit({"saved": args.key})
        elif args.command == "recall":
            emit(store.recall(args.query))
        elif args.command == "checkpoint":
            if len(args.text) > 4000:
                raise ValueError("Checkpoint is limited to 4000 characters")
            with store.db:
                store.db.execute("INSERT OR REPLACE INTO checkpoints VALUES(?,?,?,?)", (args.session, args.turn, time.time(), args.text))
            emit({"saved": True})
        elif args.command == "import-rollout":
            emit({"status": import_rollout(store, args.path, args.thread)})
        elif args.command == "report":
            result = collect(store)
            emit(result) if args.json else print(markdown(result))
        elif args.command == "run":
            return run_task(store, args)
        return 0
    except Exception as e:
        if args.command == "hook":
            # Observability never blocks the user's task or invents an approval.
            emit({"systemMessage": "Orchestra hook failed: " + type(e).__name__ + "; measurements may be incomplete."})
            return 0
        print(f"Orchestra: {e}", file=sys.stderr)
        return 1
    finally:
        if store:
            store.close()


if __name__ == "__main__":
    raise SystemExit(main())
