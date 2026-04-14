"""Project root and state directory resolution.

Root resolution order:
  1. Env var ``SOFARPC_PROJECT_ROOT`` (absolute path) — highest priority.
  2. Walk up from CWD until a marker is found: ``.sofarpc/``, ``.claude/``,
     ``.agents/``, ``pom.xml``, ``.git/``.
  3. Fall back to CWD.

Per-project state layouts, in priority order:
  - ``<root>/.sofarpc/``                 (primary, agent-neutral)
  - ``<root>/.claude/rpc-test/``         (legacy-1, Claude-specific)
  - ``<root>/.claude/skills/rpc-test/``  (legacy-2, very old skill-nested)

Each holds ``config.json``, ``index/*.json``, ``cases/*.json`` (+ ``cases/_runs/``).

Reads walk the list and return the first layout whose ``config.json`` exists.
Writers choose their target explicitly: ``detect_config --write`` writes the
primary layout, while tools such as ``build_index`` and ``run_cases`` operate
against the effective layout so legacy projects keep using their current state
dir until migrated.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import Optional, Tuple

_ROOT_MARKERS = (".sofarpc", ".claude", ".agents", "pom.xml", ".git")

LAYOUT_PRIMARY = "primary"
LAYOUT_CLAUDE = "claude"
LAYOUT_LEGACY = "legacy"


def _walk_up_for_marker(start: Path) -> Optional[Path]:
    cur = start.resolve()
    while True:
        for marker in _ROOT_MARKERS:
            if (cur / marker).exists():
                return cur
        if cur.parent == cur:
            return None
        cur = cur.parent


def resolve_project_root() -> Path:
    env = os.environ.get("SOFARPC_PROJECT_ROOT", "").strip()
    if env:
        p = Path(env).expanduser().resolve()
        if p.exists():
            return p
        sys.stderr.write(
            f"[sofarpc_cli] SOFARPC_PROJECT_ROOT={env} does not exist; falling back\n"
        )
    cwd = Path.cwd()
    found = _walk_up_for_marker(cwd)
    return found or cwd


def state_dir(project_root: Optional[Path] = None) -> Path:
    """Primary, agent-neutral per-project state dir."""
    root = project_root or resolve_project_root()
    return root / ".sofarpc"


def claude_state_dir(project_root: Optional[Path] = None) -> Path:
    """Legacy-1 state dir: `<root>/.claude/rpc-test/` (Claude-specific layout)."""
    root = project_root or resolve_project_root()
    return root / ".claude" / "rpc-test"


def legacy_state_dir(project_root: Optional[Path] = None) -> Path:
    """Legacy-2 state dir: `<root>/.claude/skills/rpc-test/` (oldest layout)."""
    root = project_root or resolve_project_root()
    return root / ".claude" / "skills" / "rpc-test"


def config_path(project_root: Optional[Path] = None) -> Path:
    return state_dir(project_root) / "config.json"


def claude_config_path(project_root: Optional[Path] = None) -> Path:
    return claude_state_dir(project_root) / "config.json"


def legacy_config_path(project_root: Optional[Path] = None) -> Path:
    return legacy_state_dir(project_root) / "config.json"


def _layout_state_dir(layout: str, project_root: Optional[Path]) -> Path:
    if layout == LAYOUT_PRIMARY:
        return state_dir(project_root)
    if layout == LAYOUT_CLAUDE:
        return claude_state_dir(project_root)
    return legacy_state_dir(project_root)


def effective_config_path(project_root: Optional[Path] = None) -> Tuple[Path, str]:
    """Return ``(path, layout)`` where *layout* is ``primary``/``claude``/``legacy``.

    Prefers primary; falls back through the legacy layouts only when their
    ``config.json`` actually exists. When nothing exists, returns the primary
    path (useful for "where would I write this?").
    """
    for layout in (LAYOUT_PRIMARY, LAYOUT_CLAUDE, LAYOUT_LEGACY):
        cand = _layout_state_dir(layout, project_root) / "config.json"
        if cand.exists():
            return cand, layout
    return config_path(project_root), LAYOUT_PRIMARY


def effective_state_dir(project_root: Optional[Path] = None) -> Path:
    _, layout = effective_config_path(project_root)
    return _layout_state_dir(layout, project_root)


def effective_index_dir(project_root: Optional[Path] = None) -> Path:
    return effective_state_dir(project_root) / "index"


def effective_cases_dir(project_root: Optional[Path] = None) -> Path:
    return effective_state_dir(project_root) / "cases"
