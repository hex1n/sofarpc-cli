"""Python tooling shared by the sofarpc CLI ecosystem.

Scope: shared primitives consumed by the bundled Claude Code skills (and
any user-written Python scripts that want to read the same per-project
config / index / case layout).

Not in scope: replacing the Go CLI under ``cmd/sofarpc/``. Go keeps the
control plane (fast cold start, clean Windows subprocess semantics,
single-binary distribution); this package keeps the data-layout
primitives that Python callers need.
"""
from __future__ import annotations

__version__ = "0.1.0"

from .project import (
    resolve_project_root,
    effective_config_path,
    effective_index_dir,
    effective_cases_dir,
)
from .config import (
    DEFAULT_CONFIG,
    load_config,
    iter_source_roots,
    resolve_repo_path,
)
from .util import save_json

__all__ = [
    "__version__",
    "resolve_project_root",
    "effective_config_path",
    "effective_index_dir",
    "effective_cases_dir",
    "DEFAULT_CONFIG",
    "load_config",
    "iter_source_roots",
    "resolve_repo_path",
    "save_json",
]
