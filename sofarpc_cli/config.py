"""Per-project configuration loading for the call-rpc skill."""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional

from .project import (
    claude_config_path,
    config_path,
    effective_config_path,
    legacy_config_path,
    resolve_project_root,
)

DEFAULT_CONFIG: Dict[str, Any] = {
    "facadeModules": [],
    "mvnCommand": "./mvnw.cmd" if os.name == "nt" else "./mvnw",
    "sofarpcBin": "sofarpc",
    "interfaceSuffixes": ["Facade", "Api"],
    "requiredMarkers": ["必传", "必填", "required"],
    "defaultContext": "",
    "manifestPath": "sofarpc.manifest.json",
}


def load_config(
    *,
    project_root: Optional[Path] = None,
    optional: bool = False,
) -> Dict[str, Any]:
    """Load project config merged onto :data:`DEFAULT_CONFIG`.

    When ``optional`` is ``True`` a missing config returns the defaults instead
    of aborting — used by ``detect_config`` when it is about to write one.
    """
    root = project_root or resolve_project_root()
    path, _ = effective_config_path(root)
    if not path.exists():
        if optional:
            return dict(DEFAULT_CONFIG)
        sys.stderr.write(
            f"[sofarpc_cli] no config found, tried:\n"
            f"  - {config_path(root)}           (primary)\n"
            f"  - {claude_config_path(root)}    (legacy-claude)\n"
            f"  - {legacy_config_path(root)}    (legacy-skill)\n"
            f"  Run `sofarpc rpc-test detect-config --write` (or the bundled\n"
            f"  detect_config.py --write) to generate one at {config_path(root)}.\n"
            f"  Project root used: {root}\n"
        )
        sys.exit(2)

    raw = json.loads(path.read_text(encoding="utf-8"))
    clean = {k: v for k, v in raw.items() if not (k.startswith("_") or k.startswith("$"))}
    merged = dict(DEFAULT_CONFIG)
    merged.update(clean)

    modules = merged.get("facadeModules") or []
    if not modules:
        sys.stderr.write(f"[sofarpc_cli] config has no facadeModules entry (at {path})\n")
        sys.exit(2)

    cleaned_modules: List[Dict[str, Any]] = []
    for m in modules:
        if not isinstance(m, dict):
            continue
        cleaned_modules.append({k: v for k, v in m.items() if not k.startswith("_")})
    merged["facadeModules"] = cleaned_modules
    return merged


def resolve_repo_path(
    path_from_repo: str,
    *,
    project_root: Optional[Path] = None,
) -> Path:
    """Resolve a repo-relative path to an absolute :class:`Path`."""
    p = Path(path_from_repo)
    if p.is_absolute():
        return p
    root = project_root or resolve_project_root()
    return (root / p).resolve()


def iter_source_roots(
    cfg: Dict[str, Any],
    *,
    project_root: Optional[Path] = None,
) -> List[Path]:
    roots: List[Path] = []
    for m in cfg.get("facadeModules", []):
        src = m.get("sourceRoot")
        if src:
            roots.append(resolve_repo_path(src, project_root=project_root))
    return roots
