#!/usr/bin/env python3
"""Fail when the embedded OpenAI contract profile is behind upstream HEAD."""

from __future__ import annotations

import json
import os
import pathlib
import sys
import urllib.error
import urllib.request

PROFILE = pathlib.Path(__file__).resolve().parents[1] / "profiles" / "openai-chat-2026-08.json"
COMMITS_API = os.environ.get(
    "AIQA_OPENAPI_COMMITS_URL",
    "https://api.github.com/repos/openai/openai-openapi/commits?per_page=1",
)


def main() -> int:
    profile = json.loads(PROFILE.read_text(encoding="utf-8"))
    expected = profile["source_commit"]
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "aiqa-profile-check/0.1",
    }
    if token := os.environ.get("GITHUB_TOKEN"):
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(COMMITS_API, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            commits = json.load(response)
    except (OSError, urllib.error.HTTPError, json.JSONDecodeError) as exc:
        print(f"unable to query OpenAI OpenAPI upstream: {exc}", file=sys.stderr)
        return 2
    if not commits or not isinstance(commits[0].get("sha"), str):
        print("unexpected GitHub commits response", file=sys.stderr)
        return 2
    actual = commits[0]["sha"]
    if actual == expected:
        print(f"profile source is current: {actual}")
        return 0
    print("OpenAI OpenAPI upstream moved; review the schema diff before updating the profile.")
    print(f"profile:  {expected}")
    print(f"upstream: {actual}")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
