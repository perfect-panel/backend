#!/usr/bin/env python3
"""Static contract checks for the Swagger sync GitHub Actions workflow."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from typing import cast

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "swagger.yaml"
TEXT = WORKFLOW.read_text(encoding="utf-8")


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    sys.exit(1)


def require(pattern: str, message: str, *, flags: int = 0) -> re.Match[str]:
    match = re.search(pattern, TEXT, flags)
    if not match:
        fail(message)
    return cast(re.Match[str], match)


# PRs and all long-lived branches should generate/verify, but only main syncs cross-repo.
require(r"pull_request:[\s\S]*branches:\s*\[main, dev\]", "pull_request must run verification for main and dev")
require(r"push:[\s\S]*branches:\s*\[main, dev\]", "push must run verification for main and dev")

# The backend workflow must sync only from non-PR main runs.
require(r"if:\s*github\.event_name\s*!=\s*'pull_request'\s*&&\s*github\.ref\s*==\s*'refs/heads/main'", "sync steps must be gated to non-PR main runs")

# The target repository is frontend main checked out under ./frontend.
require(r"repository:\s*perfect-panel/frontend\b", "workflow must checkout perfect-panel/frontend")
require(r"ref:\s*main\b", "frontend checkout must pin ref: main")
require(r"path:\s*frontend\b", "frontend checkout must use path: frontend")
require(r"frontend/docs/public/swagger", "workflow must sync into frontend/docs/public/swagger")

# Sync every generated top-level JSON dynamically instead of maintaining a stale manual list.
require(r"manifest=['\"]frontend/docs/public/swagger/\.backend-generated['\"]", "workflow must track backend-owned files in a manifest")
require(r"done\s*<\s*['\"]?\$\{manifest\}['\"]?", "workflow must remove files listed by the previous backend manifest")
require(r"find\s+build/swagger\b[^\n]*-maxdepth\s+1[^\n]*-name\s+['\"]\*\.json['\"][^\n]*-printf\s+['\"]%f", "workflow must discover all top-level build/swagger/*.json files dynamically")
require(r"cp\s+['\"]build/swagger/\$\{file\}['\"]\s+frontend/docs/public/swagger/", "workflow must copy each discovered JSON into frontend/docs/public/swagger")

# Only generated Swagger JSON should be committed in the frontend repository, and no-op syncs should not commit.
require(r"working-directory:\s*frontend\b", "commit step must run in frontend checkout")
require(r"git add\s+--all\s+--\s+docs/public/swagger", "commit step must stage additions, updates, and removals under docs/public/swagger")
require(r"git diff --cached --quiet", "workflow must skip commit when there are no changes")
require(r"git commit -m ['\"]docs\(api\): sync Swagger from backend['\"]", "workflow must use the required Swagger sync commit message")

# Old docs repo and frontend client generation must not be touched by this workflow.
if "perfect-panel/ppanel-docs" in TEXT or "ppanel-docs" in TEXT:
    fail("workflow must not checkout or write perfect-panel/ppanel-docs")
if re.search(r"openapi2ts", TEXT, re.IGNORECASE):
    fail("workflow must not run openapi2ts")
print("Swagger workflow contract OK")
