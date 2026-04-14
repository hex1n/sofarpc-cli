#!/usr/bin/env python3
"""Replay saved call-rpc cases for the effective project state.

CLI-first entrypoint:
  sofarpc rpc-test run-cases
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import _lib  # noqa: F401,E402  — side effect: ensures sofarpc_cli on sys.path
from sofarpc_cli import effective_cases_dir, load_config, resolve_project_root  # noqa: E402

REPO_ROOT = resolve_project_root()
CASES_DIR = effective_cases_dir(REPO_ROOT)

if sys.stdout.encoding and sys.stdout.encoding.lower() != "utf-8":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

RUNS_DIR = CASES_DIR / "_runs"


def display_path(path: Path) -> str:
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def iter_case_files() -> List[Path]:
    if not CASES_DIR.exists():
        return []
    return sorted(
        p for p in CASES_DIR.glob("*.json")
        if not p.name.startswith("_")
    )


def build_command(sofarpc: str, service: str, method: str, case: dict,
                  params: list, context_override: Optional[str],
                  default_context: Optional[str]) -> Tuple[List[str], Path]:
    tmp = tempfile.NamedTemporaryFile(
        mode="w", suffix=".json", prefix=".rpc-run-",
        dir=str(REPO_ROOT), delete=False, encoding="utf-8",
    )
    tmp.write(json.dumps(params, ensure_ascii=False))
    tmp.close()
    tmp_path = Path(tmp.name)
    argv: List[str] = [sofarpc, "call"]
    ctx = context_override or case.get("context") or default_context
    if ctx:
        argv += ["-context", ctx]
    if case.get("payloadMode"):
        argv += ["-payload-mode", case["payloadMode"]]
    if case.get("timeoutMs"):
        argv += ["-timeout-ms", str(case["timeoutMs"])]
    argv += ["-data", f"@{tmp_path}"]
    argv += ["-full-response"]
    argv.append(f"{service}.{method}")
    return argv, tmp_path


def _unwrap_generic_envelope(body: Any) -> Any:
    """Generic payload mode returns {type, fields, fieldNames}; peel up to
    a couple of layers so the classifier sees the real business payload."""
    for _ in range(3):
        if isinstance(body, dict) and isinstance(body.get("fields"), dict) and "type" in body:
            body = body["fields"]
            continue
        break
    return body


def parse_result(stdout: str) -> Dict[str, Any]:
    """Extract useful summary from a -full-response stdout."""
    try:
        data = json.loads(stdout)
    except Exception:
        return {"parsed": False, "stdout_head": stdout[:200]}
    out: Dict[str, Any] = {"parsed": True}
    if isinstance(data, dict):
        body = data.get("result") if isinstance(data.get("result"), dict) else data
        if isinstance(body, dict) and isinstance(body.get("fields"), dict) and "type" in body:
            out["envelope"] = body.get("type")
            body = _unwrap_generic_envelope(body)
        if isinstance(body, dict):
            out["success"] = body.get("success")
            out["errorCode"] = body.get("errorCode") or body.get("code")
            out["errorMsg"] = body.get("errorMsg") or body.get("message")
        diag = data.get("diagnostics")
        if isinstance(diag, dict):
            out["target"] = diag.get("targetUrl") or diag.get("target")
    return out


def classify(rc: int, summary: Dict[str, Any]) -> str:
    if rc != 0:
        return "RPC_FAIL"
    if summary.get("success") is False:
        return "BIZ_FAIL"
    if summary.get("success") is True:
        return "OK"
    return "UNKNOWN"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--filter", default="", help="substring match against service or method")
    ap.add_argument("--only-names", default="", help="comma-separated case names to include")
    ap.add_argument("--context", default="", help="override sofarpc context for every case")
    ap.add_argument("--dry-run", action="store_true", help="print commands, do not execute")
    ap.add_argument("--save", action="store_true", help="save per-case result to cases/_runs/")
    ap.add_argument("--sofarpc", default="", help="override sofarpc binary (else from config.json)")
    args = ap.parse_args()

    cfg = load_config(project_root=REPO_ROOT)
    sofarpc = args.sofarpc or cfg.get("sofarpcBin") or "sofarpc"
    case_files = iter_case_files()
    if not case_files:
        print(f"[run_cases] no cases under {display_path(CASES_DIR)}", file=sys.stderr)
        return 2

    if args.save:
        RUNS_DIR.mkdir(parents=True, exist_ok=True)

    only_names = {n.strip() for n in args.only_names.split(",") if n.strip()}
    rows: List[Tuple[str, str, str, str, str]] = []  # service_method, case, status, code, msg
    any_rpc_fail = False

    for cf in case_files:
        try:
            spec = json.loads(cf.read_text(encoding="utf-8"))
        except Exception as exc:
            print(f"[run_cases] skip {cf.name}: {exc}", file=sys.stderr)
            continue
        service = spec.get("service")
        method = spec.get("method")
        if not service or not method:
            continue
        slug = f"{service}.{method}"
        if args.filter and args.filter.lower() not in slug.lower():
            continue
        for case in spec.get("cases", []):
            name = case.get("name") or "<unnamed>"
            if only_names and name not in only_names:
                continue
            params = case.get("params") or []
            argv, tmp = build_command(
                sofarpc, service, method, case, params,
                args.context or None,
                cfg.get("defaultContext") or None,
            )
            shown = " ".join(_shquote(a) for a in argv)
            print(f"▶ {service.rsplit('.',1)[-1]}.{method} [{name}]")
            print(f"    {shown}")
            if args.dry_run:
                tmp.unlink(missing_ok=True)
                rows.append((f"{service.rsplit('.',1)[-1]}.{method}", name, "DRY", "", ""))
                continue
            try:
                proc = subprocess.run(
                    argv,
                    cwd=str(REPO_ROOT),
                    capture_output=True,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                )
            finally:
                try:
                    tmp.unlink(missing_ok=True)
                except Exception:
                    pass
            summary = parse_result(proc.stdout)
            status = classify(proc.returncode, summary)
            if status == "RPC_FAIL":
                any_rpc_fail = True
            rows.append((
                f"{service.rsplit('.',1)[-1]}.{method}", name, status,
                str(summary.get("errorCode") or ""),
                (summary.get("errorMsg") or "")[:60],
            ))
            if args.save:
                out = {
                    "at": dt.datetime.now().astimezone().isoformat(timespec="seconds"),
                    "status": status,
                    "returnCode": proc.returncode,
                    "summary": summary,
                    "stdout": proc.stdout,
                    "stderr": proc.stderr,
                }
                safe = cf.stem + "__" + name.replace("/", "_")
                (RUNS_DIR / f"{safe}.json").write_text(
                    json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8",
                )

    print("\n── summary " + "─" * 60)
    print(f"{'case':<50}{'name':<14}{'status':<10}{'code':<10}{'msg'}")
    for r in rows:
        print(f"{r[0][:48]:<50}{r[1][:12]:<14}{r[2]:<10}{r[3]:<10}{r[4]}")
    if not rows:
        print("(no cases matched filter)")
        return 2
    return 1 if any_rpc_fail else 0


def _shquote(s: str) -> str:
    if not s or any(c in s for c in " \"'<>|&"):
        return '"' + s.replace('"', '\\"') + '"'
    return s


if __name__ == "__main__":
    raise SystemExit(main())
