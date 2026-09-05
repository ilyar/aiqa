"""Local metadata and memory. Python 3.11+, no external dependencies."""
from __future__ import annotations

import hashlib
import json
import os
import re
import sqlite3
import subprocess
import time
import uuid
from pathlib import Path

VERSION = "1.0.0"


def compact(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def digest(value):
    if isinstance(value, str):
        value = value.encode()
    return hashlib.sha256(value).hexdigest()


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
        os.replace(temp, path)
    finally:
        temp.unlink(missing_ok=True)


def git(root, *args):
    try:
        p = subprocess.run(["git", "-C", str(root), *args], capture_output=True,
                           timeout=5, check=False)
        return p.stdout.decode("utf-8", "replace").strip() if p.returncode == 0 else None
    except (OSError, subprocess.TimeoutExpired):
        return None


class Store:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.config_dir = self.root / ".codex/orchestration"
        self.state = self.root / ".orchestration/state"
        self.state.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(self.state / "metrics.sqlite3", timeout=1.5)
        self.db.row_factory = sqlite3.Row
        self.db.execute("PRAGMA busy_timeout=1500")
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.executescript("""
        CREATE TABLE IF NOT EXISTS events (
          id INTEGER PRIMARY KEY, ts REAL NOT NULL, kind TEXT NOT NULL,
          session TEXT, turn TEXT, agent TEXT, role TEXT, model TEXT,
          policy_hash TEXT, data TEXT NOT NULL);
        CREATE INDEX IF NOT EXISTS events_task ON events(session,turn,ts);
        CREATE TABLE IF NOT EXISTS tasks (
          session TEXT, turn TEXT, route TEXT DEFAULT 'unknown',
          risk TEXT DEFAULT 'unknown', outcome TEXT DEFAULT 'unknown',
          rework INTEGER, escalations INTEGER, human TEXT DEFAULT 'unknown',
          evidence TEXT, policy_hash TEXT, revision TEXT,
          PRIMARY KEY(session,turn));
        CREATE TABLE IF NOT EXISTS checks (
          id TEXT PRIMARY KEY, ts REAL, session TEXT, turn TEXT, label TEXT,
          duration REAL, exit_code INTEGER, command_hash TEXT, revision TEXT);
        CREATE TABLE IF NOT EXISTS memory (
          key TEXT PRIMARY KEY, text TEXT NOT NULL, created REAL,
          expires REAL, evidence TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS checkpoints (
          session TEXT, turn TEXT, ts REAL, text TEXT,
          PRIMARY KEY(session,turn));
        CREATE TABLE IF NOT EXISTS usage (
          thread TEXT, source TEXT, model TEXT, input INTEGER, cached INTEGER,
          output INTEGER, reasoning INTEGER, status TEXT, updated REAL,
          PRIMARY KEY(thread,source,model));
        CREATE TABLE IF NOT EXISTS usage_status (
          thread TEXT, source TEXT, status TEXT, detail TEXT, updated REAL,
          PRIMARY KEY(thread,source));
        """)
        p = self.config_dir / "policy.json"
        self.policy = json.loads(p.read_text()) if p.exists() else {}
        # Include actual role and root configuration in the version of a run.
        pieces = []
        paths = [self.root / "AGENTS.md", self.root / ".codex/config.toml", p,
                 self.config_dir / "POLICY.md", self.root / ".codex/hooks.json",
                 self.root / ".orchestration/PROJECT.md"]
        paths += sorted((self.root / ".codex/agents").glob("*.toml"))
        for path in paths:
            if path.is_file():
                pieces.append([str(path.relative_to(self.root)), digest(path.read_bytes())])
        self.policy_hash = digest(compact(pieces))[:16]

    def close(self):
        self.db.close()

    def event(self, kind, session=None, turn=None, agent=None, role=None,
              model=None, data=None, ts=None):
        with self.db:
            self.db.execute("INSERT INTO events(ts,kind,session,turn,agent,role,model,policy_hash,data) VALUES(?,?,?,?,?,?,?,?,?)",
                            (time.time() if ts is None else ts, kind, session, turn,
                             agent, role, model, self.policy_hash, compact(data or {})))

    def task(self, session, turn):
        if not session or not turn:
            raise ValueError("Both session and turn are required")
        with self.db:
            self.db.execute("INSERT OR IGNORE INTO tasks(session,turn,policy_hash,revision) VALUES(?,?,?,?)",
                            (session, turn, self.policy_hash, git(self.root, "rev-parse", "HEAD")))

    def mark(self, session, turn, values):
        self.task(session, turn)
        allowed = {"route", "risk", "outcome", "rework", "escalations", "human", "evidence"}
        values = {k: v for k, v in values.items() if k in allowed and v is not None}
        if not values:
            return
        with self.db:
            self.db.execute("UPDATE tasks SET " + ",".join(k + "=?" for k in values) +
                            " WHERE session=? AND turn=?", [*values.values(), session, turn])
        self.event("annotation", session, turn, data=values)

    def remember(self, key, text, paths, ttl_days):
        if not key or not text or len(text) > 1500 or not paths or ttl_days <= 0:
            raise ValueError("Memory needs a key, <=1500 characters, evidence and positive TTL")
        evidence = []
        for name in paths:
            p = (self.root / name).resolve()
            if not p.is_relative_to(self.root) or not p.is_file():
                raise ValueError("Evidence must be an existing file inside this project")
            if p.stat().st_size > 8 * 1024 * 1024:
                raise ValueError("Use a small source file as memory evidence")
            evidence.append({"path": str(p.relative_to(self.root)), "sha256": digest(p.read_bytes())})
        now = time.time()
        with self.db:
            self.db.execute("INSERT OR REPLACE INTO memory VALUES(?,?,?,?,?)",
                            (key, text, now, now + ttl_days * 86400, compact(evidence)))

    def recall(self, query="", limit=5):
        words = set(re.findall(r"\w{3,}", query.lower()))
        result = []
        for row in self.db.execute("SELECT * FROM memory ORDER BY created DESC"):
            r = dict(row)
            evidence = json.loads(r["evidence"])
            stale = r["expires"] < time.time()
            for item in evidence:
                p = (self.root / item["path"]).resolve()
                stale |= (not p.is_relative_to(self.root) or not p.is_file() or
                          p.stat().st_size > 8 * 1024 * 1024)
                if not stale:
                    stale |= digest(p.read_bytes()) != item["sha256"]
            text = (r["key"] + " " + r["text"] + " " + compact(evidence)).lower()
            score = sum(w in text for w in words)
            if words and not score:
                continue
            result.append({"key": r["key"], "text": r["text"], "stale": stale,
                           "evidence": evidence, "score": score})
        return sorted(result, key=lambda x: (x["stale"], -x["score"]))[:limit]

    def adapt(self):
        files = git(self.root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
        names = set(files.split("\0")) if files is not None else set()
        names.update(n for n in ["package.json", "go.mod", "Cargo.toml", "pyproject.toml",
                                  "Makefile", "justfile", "pom.xml", "build.gradle.kts"]
                     if (self.root / n).is_file())
        manifests = {"package.json", "go.mod", "Cargo.toml", "pyproject.toml",
                     "Makefile", "justfile", "pom.xml", "build.gradle.kts"}
        selected = []
        for n in sorted(names):
            p = Path(n)
            if any(part in {"node_modules", "vendor", "target", ".git", ".venv"} for part in p.parts):
                continue
            if p.name in manifests or n.startswith(".github/workflows/") or p.name == "SKILL.md" or p.name == "AGENTS.md":
                selected.append(n)
        observations = []
        for n in selected[:300]:
            p = (self.root / n).resolve()
            if not p.is_relative_to(self.root) or not p.is_file() or p.stat().st_size > 1024 * 1024:
                continue
            content = p.read_bytes()
            item = {"path": n, "sha256": digest(content)}
            if p.name == "package.json":
                try:
                    data = json.loads(content)
                    item["script_names"] = sorted(data.get("scripts", {}))
                    item["package_manager"] = data.get("packageManager")
                except (ValueError, TypeError):
                    item["parse_error"] = True
            observations.append(item)
        fingerprint = digest(compact(observations))
        dest = self.root / ".orchestration/project-observed.json"
        previous = json.loads(dest.read_text()) if dest.exists() else {}
        changed = previous.get("fingerprint") != fingerprint
        result = {"schema_version": 1, "fingerprint": fingerprint, "observed_at": time.time(),
                  "status": "candidates_require_review", "truncated": len(selected) > 300,
                  "observations": observations}
        if changed:
            atomic_json(dest, result)
        return {"changed": changed, "fingerprint": fingerprint, "files": len(observations),
                "truncated": result["truncated"]}


def numeric_exit(response):
    # Unknown response formats are not treated as successful.
    if isinstance(response, dict):
        for key in ("exit_code", "exitCode"):
            value = response.get(key)
            if isinstance(value, int) and not isinstance(value, bool):
                return value
    return None


def handle_hook(store, payload):
    started = time.perf_counter()
    event = payload.get("hook_event_name", "unknown")
    session, turn = payload.get("session_id"), payload.get("turn_id")
    agent, role = payload.get("agent_id"), payload.get("agent_type")
    data = {k: payload[k] for k in ("source", "trigger", "tool_name", "tool_use_id",
                                   "stop_hook_active", "permission_mode") if k in payload}
    if event in {"PreToolUse", "PostToolUse"}:
        data["input_sha256"] = digest(compact(payload.get("tool_input")))
    if event == "PostToolUse":
        data["exit_code"] = numeric_exit(payload.get("tool_response"))
    # Prompts, command text, responses and assistant messages are not retained.
    store.event(event, session, turn, agent, role, payload.get("model"), data)
    messages = []
    context = []
    if event == "UserPromptSubmit" and session and turn:
        store.task(session, turn)
        context.append(f"ORCH_SESSION={session}; ORCH_TURN={turn}. Record the route/outcome using orch.py mark. Read .codex/orchestration/POLICY.md as needed.")
        rows = store.recall(str(payload.get("prompt", "")), store.policy.get("memory_limit", 5))
        valid = [r for r in rows if not r["stale"]]
        if valid:
            context.append("Relevant project memory (recheck evidence before consequential changes): " + compact(valid)[:4500])
    if event == "SessionStart":
        adaptation = store.adapt()
        context.append("Read .orchestration/PROJECT.md. Manifest/CI observations are " +
                       ("new or changed; review on the next substantive task." if adaptation["changed"] else "unchanged."))
        # Restrict checkpoints to this conversation, including compact/resume.
        row = store.db.execute("SELECT turn,text FROM checkpoints WHERE session=? ORDER BY ts DESC LIMIT 1", (session,)).fetchone()
        if row:
            context.append("Latest checkpoint for this session (validate current files): " + row["text"][:3000])
    if event == "SubagentStart":
        context.append(f"Parent ORCH_SESSION={session}; ORCH_TURN={turn}; agent={agent}. Follow your leaf contract; return memory candidates to the parent.")
    if event in {"Stop", "SubagentStop"} and store.policy.get("observe_rollouts", True):
        from usage import import_rollout
        path = payload.get("agent_transcript_path") if event == "SubagentStop" else payload.get("transcript_path")
        thread = agent if event == "SubagentStop" else session
        if thread:
            try:
                status = import_rollout(store, path, thread)
                if status != "observed":
                    messages.append("Orchestra: token usage is incomplete (" + status + "); see report.")
            except (OSError, ValueError, sqlite3.Error) as e:
                messages.append("Orchestra: token adapter failed (" + type(e).__name__ + "); usage is unknown.")
    store.event("hook_overhead", session, turn, agent, role, data={"hook": event, "ms": (time.perf_counter() - started) * 1000})
    output = {}
    if context:
        output["hookSpecificOutput"] = {"hookEventName": event, "additionalContext": "\n".join(context)[:8000]}
    if messages:
        output["systemMessage"] = " ".join(messages)
    return output
