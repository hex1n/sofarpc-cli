"""Smoke tests for the sofarpc_cli package.

Keep fast and hermetic — everything happens under a tmp_path fixture; no
dependence on the real project root or env vars.
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import pytest

from sofarpc_cli import (
    DEFAULT_CONFIG,
    effective_cases_dir,
    effective_config_path,
    effective_index_dir,
    iter_source_roots,
    load_config,
    resolve_project_root,
    resolve_repo_path,
    save_json,
)


def _mkproject(
    tmp: Path,
    with_config: bool = True,
    layout: str = "primary",
) -> Path:
    """Layout is one of: "primary" (.sofarpc/), "claude" (.claude/rpc-test/),
    "legacy" (.claude/skills/rpc-test/).
    """
    root = tmp / "proj"
    if layout == "primary":
        state = root / ".sofarpc"
    elif layout == "claude":
        state = root / ".claude" / "rpc-test"
    else:
        state = root / ".claude" / "skills" / "rpc-test"
    state.mkdir(parents=True)
    if with_config:
        (state / "config.json").write_text(
            json.dumps({
                "facadeModules": [
                    {"name": "x-facade", "sourceRoot": "mod/src/main/java"},
                ],
                "defaultContext": "test-direct",
            }),
            encoding="utf-8",
        )
    return root


def test_resolve_project_root_env_override(tmp_path, monkeypatch):
    root = _mkproject(tmp_path)
    monkeypatch.setenv("SOFARPC_PROJECT_ROOT", str(root))
    assert resolve_project_root() == root


def test_resolve_project_root_walks_up(tmp_path, monkeypatch):
    root = _mkproject(tmp_path)
    nested = root / "a" / "b" / "c"
    nested.mkdir(parents=True)
    monkeypatch.delenv("SOFARPC_PROJECT_ROOT", raising=False)
    monkeypatch.chdir(nested)
    assert resolve_project_root() == root


def test_effective_paths_primary_layout(tmp_path):
    root = _mkproject(tmp_path, layout="primary")
    cfg_path, layout = effective_config_path(root)
    assert cfg_path == root / ".sofarpc" / "config.json"
    assert layout == "primary"
    assert effective_index_dir(root) == root / ".sofarpc" / "index"
    assert effective_cases_dir(root) == root / ".sofarpc" / "cases"


def test_effective_paths_claude_fallback(tmp_path):
    root = _mkproject(tmp_path, layout="claude")
    cfg_path, layout = effective_config_path(root)
    assert cfg_path == root / ".claude" / "rpc-test" / "config.json"
    assert layout == "claude"
    assert effective_index_dir(root) == root / ".claude" / "rpc-test" / "index"
    assert effective_cases_dir(root) == root / ".claude" / "rpc-test" / "cases"


def test_effective_paths_legacy_fallback(tmp_path):
    root = _mkproject(tmp_path, layout="legacy")
    cfg_path, layout = effective_config_path(root)
    assert cfg_path == root / ".claude" / "skills" / "rpc-test" / "config.json"
    assert layout == "legacy"
    assert effective_index_dir(root) == root / ".claude" / "skills" / "rpc-test" / "index"
    assert effective_cases_dir(root) == root / ".claude" / "skills" / "rpc-test" / "cases"


def test_effective_paths_prefers_primary_when_all_present(tmp_path):
    root = _mkproject(tmp_path, layout="primary")
    # add a claude-layout config that should be shadowed by primary
    (root / ".claude" / "rpc-test").mkdir(parents=True)
    (root / ".claude" / "rpc-test" / "config.json").write_text(
        json.dumps({"facadeModules": [{"name": "shadowed", "sourceRoot": "x"}]}),
        encoding="utf-8",
    )
    cfg_path, layout = effective_config_path(root)
    assert layout == "primary"
    assert cfg_path == root / ".sofarpc" / "config.json"


def test_effective_paths_no_config_returns_primary_target(tmp_path):
    root = tmp_path / "empty"
    root.mkdir()
    cfg_path, layout = effective_config_path(root)
    assert layout == "primary"
    assert cfg_path == root / ".sofarpc" / "config.json"


def test_load_config_merges_defaults(tmp_path):
    root = _mkproject(tmp_path)
    cfg = load_config(project_root=root)
    assert cfg["defaultContext"] == "test-direct"
    assert cfg["requiredMarkers"] == DEFAULT_CONFIG["requiredMarkers"]
    assert cfg["facadeModules"][0]["name"] == "x-facade"


def test_load_config_strips_comment_keys(tmp_path):
    root = tmp_path / "proj"
    (root / ".claude" / "rpc-test").mkdir(parents=True)
    (root / ".claude" / "rpc-test" / "config.json").write_text(
        json.dumps({
            "_comment": "ignore me",
            "$schema": "ignore me too",
            "facadeModules": [{"name": "y", "sourceRoot": "mod/java", "_note": "dropped"}],
        }),
        encoding="utf-8",
    )
    cfg = load_config(project_root=root)
    assert "_comment" not in cfg and "$schema" not in cfg
    assert "_note" not in cfg["facadeModules"][0]


def test_load_config_optional_returns_defaults(tmp_path):
    root = tmp_path / "empty"
    root.mkdir()
    cfg = load_config(project_root=root, optional=True)
    assert cfg == dict(DEFAULT_CONFIG)


def test_load_config_missing_exits(tmp_path):
    root = tmp_path / "empty"
    root.mkdir()
    with pytest.raises(SystemExit) as exc:
        load_config(project_root=root)
    assert exc.value.code == 2


def test_iter_source_roots_resolves_relative(tmp_path):
    root = _mkproject(tmp_path)
    cfg = load_config(project_root=root)
    roots = iter_source_roots(cfg, project_root=root)
    assert roots == [root / "mod" / "src" / "main" / "java"]


def test_resolve_repo_path_absolute_passthrough(tmp_path):
    abs_path = tmp_path / "somewhere"
    assert resolve_repo_path(str(abs_path), project_root=tmp_path) == abs_path


def test_save_json_creates_parent_dirs(tmp_path):
    target = tmp_path / "deep" / "nested" / "out.json"
    save_json(target, {"hello": "世界"})
    assert json.loads(target.read_text(encoding="utf-8")) == {"hello": "世界"}
