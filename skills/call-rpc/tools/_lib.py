"""Bootstrap `sofarpc_cli` importability for bundled call-rpc scripts."""
from __future__ import annotations

import os
import sys
from pathlib import Path
from shutil import which
from typing import Iterable


def _candidate_install_roots() -> Iterable[Path]:
    home = os.environ.get("SOFARPC_HOME", "").strip()
    if home:
        yield Path(home).expanduser()
    marker = Path(__file__).resolve().parent / ".sofarpc_install_root"
    if marker.exists():
        try:
            pointer = marker.read_text(encoding="utf-8").strip()
        except Exception:
            pointer = ""
        if pointer:
            yield Path(pointer)
    cur = Path(__file__).resolve().parent
    for _ in range(8):
        yield cur
        if cur.parent == cur:
            break
        cur = cur.parent
    exe = which("sofarpc") or which("sofarpc.exe")
    if exe:
        p = Path(exe).resolve()
        yield p.parent.parent


def _ensure_sofarpc_cli() -> None:
    try:
        import sofarpc_cli  # noqa: F401
        return
    except ImportError:
        pass
    seen = set()
    for root in _candidate_install_roots():
        root = root.resolve()
        if root in seen:
            continue
        seen.add(root)
        if (root / "sofarpc_cli" / "__init__.py").exists():
            sys.path.insert(0, str(root))
            try:
                import sofarpc_cli  # noqa: F401
                return
            except ImportError:
                continue
    raise ImportError(
        "Cannot locate the `sofarpc_cli` Python package.\n"
        "  Either `pip install sofarpc-cli`, or set SOFARPC_HOME to the CLI install root."
    )


_ensure_sofarpc_cli()
