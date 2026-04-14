#!/usr/bin/env python3
"""Detect facade modules and print or write call-rpc config.

CLI-first entrypoint:
  sofarpc rpc-test detect-config --write

Direct script fallback:
  python tools/detect_config.py --write
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Dict, List, Optional

if sys.stdout.encoding and sys.stdout.encoding.lower() != "utf-8":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import _lib  # noqa: F401,E402  — side effect: ensures sofarpc_cli on sys.path
from sofarpc_cli import DEFAULT_CONFIG, resolve_project_root  # noqa: E402
from sofarpc_cli.project import (  # noqa: E402
    claude_config_path,
    config_path,
    legacy_config_path,
)

REPO_ROOT = resolve_project_root()
CONFIG_PATH = config_path(REPO_ROOT)
_CLAUDE_CONFIG_PATH = claude_config_path(REPO_ROOT)
_LEGACY_CONFIG_PATH = legacy_config_path(REPO_ROOT)

FACADE_SUFFIX_PAT = re.compile(
    r"(?:-|_)?(facade|api|client)(?:s)?$", re.IGNORECASE
)
SKIP_DIRS = {"target", "build", "node_modules", ".git", ".idea", "dist", "out"}
ARTIFACT_RE = re.compile(r"<artifactId>\s*([^<\s]+?)\s*</artifactId>", re.IGNORECASE)


def iter_pom_files(root: Path):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS and not d.startswith(".")]
        if "pom.xml" in filenames:
            yield Path(dirpath) / "pom.xml"


def first_artifact_id(pom_text: str) -> Optional[str]:
    # first <artifactId> inside <project> (skip <parent>)
    # simplest heuristic: first match whose position > last </parent> close
    parent_close = pom_text.find("</parent>")
    search_from = parent_close + len("</parent>") if parent_close != -1 else 0
    m = ARTIFACT_RE.search(pom_text, search_from)
    if m:
        return m.group(1)
    m = ARTIFACT_RE.search(pom_text)
    return m.group(1) if m else None


def looks_like_facade(artifact: str) -> bool:
    return bool(FACADE_SUFFIX_PAT.search(artifact))


def detect_mvn_command() -> str:
    if os.name == "nt" and (REPO_ROOT / "mvnw.cmd").exists():
        return "./mvnw.cmd"
    if (REPO_ROOT / "mvnw").exists():
        return "./mvnw"
    return "mvn"


def detect_sofarpc_bin() -> str:
    from shutil import which
    if which("sofarpc") or which("sofarpc.exe"):
        return "sofarpc"
    exe_name = "sofarpc.exe" if os.name == "nt" else "sofarpc"
    common: List[Path] = []
    shim_dir = os.environ.get("SOFARPC_SHIM_DIR", "").strip()
    if shim_dir:
        common.append(Path(shim_dir) / exe_name)
    if os.name == "nt":
        common.append(Path.home() / "bin" / exe_name)
    else:
        common.append(Path.home() / ".local" / "bin" / exe_name)
    common.append(Path.home() / ".sofarpc" / "bin" / exe_name)
    for c in common:
        if c.exists():
            return str(c).replace("\\", "/")
    return "sofarpc"


def rel(path: Path) -> str:
    try:
        return str(path.relative_to(REPO_ROOT)).replace("\\", "/")
    except ValueError:
        return str(path).replace("\\", "/")


def detect_facade_modules() -> List[dict]:
    found: List[dict] = []
    for pom in iter_pom_files(REPO_ROOT):
        try:
            text = pom.read_text(encoding="utf-8", errors="replace")
        except Exception:
            continue
        artifact = first_artifact_id(text)
        if not artifact or not looks_like_facade(artifact):
            continue
        mod_dir = pom.parent
        src = mod_dir / "src" / "main" / "java"
        if not src.exists():
            continue
        found.append({
            "name": artifact,
            "sourceRoot": rel(src),
            "mavenModulePath": rel(mod_dir),
            "jarGlob": f"{rel(mod_dir)}/target/{artifact}-*.jar",
            "depsDir": f"{rel(mod_dir)}/target/facade-deps",
        })
    # de-dup by (name, mavenModulePath)
    seen = set()
    unique = []
    for m in found:
        key = (m["name"], m["mavenModulePath"])
        if key in seen:
            continue
        seen.add(key)
        unique.append(m)
    return unique


def merge(existing: Optional[dict], detected: dict) -> dict:
    """User-edited fields win; only fill blanks from detected."""
    if not existing:
        return detected
    merged = dict(existing)
    for k, v in detected.items():
        if k not in merged or merged[k] in ("", None, []):
            merged[k] = v
    return merged


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true", help="write to config.json (default: print only)")
    args = ap.parse_args()

    detected = dict(DEFAULT_CONFIG)
    detected["mvnCommand"] = detect_mvn_command()
    detected["sofarpcBin"] = detect_sofarpc_bin()
    detected["facadeModules"] = detect_facade_modules()

    existing = None
    read_from = None
    for candidate in (CONFIG_PATH, _CLAUDE_CONFIG_PATH, _LEGACY_CONFIG_PATH):
        if candidate.exists():
            try:
                raw = json.loads(candidate.read_text(encoding="utf-8"))
                existing = {k: v for k, v in raw.items() if not (k.startswith("_") or k.startswith("$"))}
                read_from = candidate
                break
            except Exception as exc:
                print(f"[detect] {candidate} is not valid JSON ({exc}); ignoring", file=sys.stderr)

    final = merge(existing, detected)

    print(json.dumps(final, ensure_ascii=False, indent=2))

    if not final["facadeModules"]:
        print(
            "\n[detect] no facade modules found. Expected a maven module whose\n"
            "  artifactId ends in 'facade' / 'api' / 'client' with src/main/java.\n"
            "  Edit the generated config manually if your project uses different naming.",
            file=sys.stderr,
        )

    if args.write:
        CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
        CONFIG_PATH.write_text(json.dumps(final, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(f"\n[detect] wrote {CONFIG_PATH}")
        if read_from is not None and read_from != CONFIG_PATH:
            print(
                f"[detect] migrated from legacy location {read_from}\n"
                f"  you can delete the legacy file when comfortable.",
                file=sys.stderr,
            )
    else:
        print(f"\n[detect] dry-run only; pass --write to save {CONFIG_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
