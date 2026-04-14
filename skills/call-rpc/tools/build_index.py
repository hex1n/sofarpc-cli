#!/usr/bin/env python3
"""Build facade skeleton indexes for the effective project state.

CLI-first entrypoint:
  sofarpc rpc-test build-index
"""
from __future__ import annotations

import json
import os
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Set, Tuple

if sys.stdout.encoding and sys.stdout.encoding.lower() != "utf-8":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

try:
    import javalang  # type: ignore
except ImportError:
    sys.stderr.write("[build_index] missing dependency: pip install javalang\n")
    sys.exit(2)

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import _lib  # noqa: F401,E402  — side effect: ensures sofarpc_cli on sys.path
from sofarpc_cli import (  # noqa: E402
    effective_index_dir,
    iter_source_roots,
    load_config,
    resolve_project_root,
    save_json,
)

REPO_ROOT = resolve_project_root()
INDEX_DIR = effective_index_dir(REPO_ROOT)


def display_path(path: Path) -> str:
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)

PRIMITIVE_ZERO = {
    "byte", "short", "int", "long", "float", "double",
    "Byte", "Short", "Integer", "Long", "Float", "Double",
    "Number", "AtomicInteger", "AtomicLong",
}
STRING_LIKE = {"String", "CharSequence", "UUID"}
BOOL_LIKE = {"boolean", "Boolean"}
DECIMAL_LIKE = {"BigDecimal", "BigInteger"}
DATE_LIKE = {"Date", "LocalDate", "LocalDateTime", "Instant", "Timestamp", "LocalTime", "OffsetDateTime", "ZonedDateTime"}
COLLECTION_LIKE = {"List", "ArrayList", "LinkedList", "Collection", "Iterable", "Set", "HashSet", "LinkedHashSet", "TreeSet"}
MAP_LIKE = {"Map", "HashMap", "LinkedHashMap", "TreeMap", "ConcurrentHashMap"}

REQUIRED_ANNOTATIONS = {"NotNull", "NonNull", "NotEmpty", "NotBlank"}
SKIP_FIELD_NAMES = {"serialVersionUID"}

# SOFA-style: <li>fieldName|必传|description</li>
SOFA_LI_PAT = re.compile(r"<li>\s*([A-Za-z_]\w*)\s*\|\s*([^|<]+?)\s*\|", re.IGNORECASE)
# `@param name ... 必传 ...`
PARAM_DOC_PAT = re.compile(r"@param\s+([A-Za-z_]\w*)\s+([^@]+)", re.IGNORECASE)


@dataclass
class FieldInfo:
    name: str
    java_type: str
    comment: str = ""
    required: bool = False


@dataclass
class ClassInfo:
    fqn: str
    simple_name: str
    file: str
    kind: str                       # "class" | "interface" | "enum"
    superclass: Optional[str] = None
    imports: Dict[str, str] = field(default_factory=dict)
    same_pkg_prefix: str = ""
    fields: List[FieldInfo] = field(default_factory=list)
    enum_constants: List[str] = field(default_factory=list)
    methods: List[dict] = field(default_factory=list)
    # Return types of instance methods on a class; used to surface response
    # wrapper helper getters such as dataOptional()/dataOrThrow().
    method_returns: List[str] = field(default_factory=list)


def iter_java_files(roots: List[Path]) -> Iterable[Path]:
    for r in roots:
        if not r.exists():
            sys.stderr.write(f"[build_index] skip missing root: {r}\n")
            continue
        for p in r.rglob("*.java"):
            yield p


def extract_javadoc_before(source: str, decl_pos: int) -> str:
    chunk = source[:decl_pos]
    end = chunk.rfind("*/")
    if end == -1:
        return ""
    start = chunk.rfind("/**", 0, end)
    if start == -1:
        return ""
    tail = chunk[end + 2:]
    if tail.strip():
        return ""
    raw = chunk[start + 3:end]
    lines = []
    for line in raw.splitlines():
        line = line.strip().lstrip("*").strip()
        if line:
            lines.append(line)
    return " ".join(lines)


def annotation_names(node) -> Set[str]:
    names: Set[str] = set()
    ann = getattr(node, "annotations", None) or []
    for a in ann:
        if a.name:
            names.add(a.name.split(".")[-1])
    return names


def contains_required_marker(text: str, markers: List[str]) -> bool:
    low = text.lower()
    for m in markers:
        if m.lower() in low:
            return True
    return False


def is_required(field_node, comment: str, markers: List[str]) -> bool:
    if contains_required_marker(comment, markers):
        return True
    return bool(annotation_names(field_node) & REQUIRED_ANNOTATIONS)


def render_type(ref) -> str:
    if ref is None:
        return "void"
    if isinstance(ref, javalang.tree.BasicType):
        base = ref.name
        if getattr(ref, "dimensions", None):
            base += "[]" * len(ref.dimensions)
        return base
    if isinstance(ref, javalang.tree.ReferenceType):
        base = ref.name
        args = getattr(ref, "arguments", None)
        if args:
            inner = []
            for a in args:
                if a is None or a.type is None:
                    inner.append("?")
                else:
                    inner.append(render_type(a.type))
            base += "<" + ", ".join(inner) + ">"
        sub = getattr(ref, "sub_type", None)
        if sub is not None:
            base += "." + render_type(sub)
        if getattr(ref, "dimensions", None):
            base += "[]" * len(ref.dimensions)
        return base
    return str(ref)


def parse_file(path: Path, markers: List[str]) -> List[ClassInfo]:
    source = path.read_text(encoding="utf-8", errors="replace")
    try:
        tree = javalang.parse.parse(source)
    except Exception as exc:
        sys.stderr.write(f"[build_index] parse failed {path}: {exc}\n")
        return []

    pkg = tree.package.name if tree.package else ""
    imports: Dict[str, str] = {}
    for imp in tree.imports:
        if imp.wildcard or imp.static:
            continue
        simple = imp.path.rsplit(".", 1)[-1]
        imports[simple] = imp.path

    infos: List[ClassInfo] = []
    for td in tree.types:
        info = build_class_info(td, pkg, imports, source, path, markers)
        if info:
            infos.append(info)
    return infos


def build_class_info(td, pkg: str, imports: Dict[str, str], source: str,
                     path: Path, markers: List[str]) -> Optional[ClassInfo]:
    simple = td.name
    fqn = f"{pkg}.{simple}" if pkg else simple

    if isinstance(td, javalang.tree.InterfaceDeclaration):
        kind = "interface"
    elif isinstance(td, javalang.tree.EnumDeclaration):
        kind = "enum"
    elif isinstance(td, javalang.tree.ClassDeclaration):
        kind = "class"
    else:
        return None

    superclass = None
    if kind == "class":
        ext = getattr(td, "extends", None)
        if ext is not None:
            superclass = ext.name

    info = ClassInfo(
        fqn=fqn,
        simple_name=simple,
        file=str(path.relative_to(REPO_ROOT)).replace("\\", "/"),
        kind=kind,
        superclass=superclass,
        imports=imports,
        same_pkg_prefix=pkg,
    )

    if kind == "enum":
        for c in getattr(td, "body", []) or []:
            if isinstance(c, javalang.tree.EnumConstantDeclaration):
                info.enum_constants.append(c.name)

    if kind == "class":
        for f in td.fields:
            mods = set(getattr(f, "modifiers", set()) or [])
            if "static" in mods or "transient" in mods:
                continue
            for decl in f.declarators:
                if decl.name in SKIP_FIELD_NAMES:
                    continue
                type_str = render_type(f.type)
                pos = f.position.line if f.position else 0
                decl_pos = offset_of_line(source, pos)
                doc = extract_javadoc_before(source, decl_pos)
                info.fields.append(FieldInfo(
                    name=decl.name,
                    java_type=type_str,
                    comment=doc,
                    required=is_required(f, doc, markers),
                ))
        for cm in getattr(td, "methods", []) or []:
            mods = set(getattr(cm, "modifiers", set()) or [])
            if "static" in mods or "private" in mods:
                continue
            if cm.return_type is None:
                continue
            info.method_returns.append(render_type(cm.return_type))

    if kind == "interface":
        for m in td.methods:
            pos = m.position.line if m.position else 0
            decl_pos = offset_of_line(source, pos)
            doc = extract_javadoc_before(source, decl_pos)
            params = []
            for p in m.parameters:
                params.append({"name": p.name, "type": render_type(p.type)})
            info.methods.append({
                "name": m.name,
                "javadoc": doc,
                "returnType": render_type(m.return_type) if m.return_type is not None else "void",
                "parameters": params,
            })

    return info


def offset_of_line(source: str, line: int) -> int:
    if line <= 1:
        return 0
    idx = 0
    cur_line = 1
    while cur_line < line and idx < len(source):
        nl = source.find("\n", idx)
        if nl == -1:
            return len(source)
        idx = nl + 1
        cur_line += 1
    return idx


# ---------------------------------------------------------------------------
# Resolver
# ---------------------------------------------------------------------------

def resolve_fqn(simple: str, ctx: ClassInfo, registry: Dict[str, ClassInfo]) -> Optional[str]:
    head = simple.split("<", 1)[0].split("[", 1)[0].strip()
    if head in ctx.imports:
        candidate = ctx.imports[head]
        if candidate in registry:
            return candidate
        return candidate
    if ctx.same_pkg_prefix:
        candidate = f"{ctx.same_pkg_prefix}.{head}"
        if candidate in registry:
            return candidate
    if head in registry:
        return head
    return None


def parse_generic_args(type_str: str) -> List[str]:
    lt = type_str.find("<")
    if lt == -1:
        return []
    depth = 0
    buf: List[str] = []
    out: List[str] = []
    for ch in type_str[lt:]:
        if ch == "<":
            depth += 1
            if depth > 1:
                buf.append(ch)
            continue
        if ch == ">":
            depth -= 1
            if depth == 0:
                token = "".join(buf).strip()
                if token:
                    out.append(token)
                return out
            buf.append(ch)
            continue
        if depth == 1 and ch == ",":
            token = "".join(buf).strip()
            if token:
                out.append(token)
            buf = []
            continue
        buf.append(ch)
    return out


# ---------------------------------------------------------------------------
# Skeleton builder
# ---------------------------------------------------------------------------

@dataclass
class BuildCtx:
    registry: Dict[str, ClassInfo]
    markers: List[str]
    visited: Set[str] = field(default_factory=set)


def skeleton_for_type(type_str: str, owner: ClassInfo, ctx: BuildCtx) -> Tuple[object, dict]:
    head = type_str.split("<", 1)[0].split("[", 1)[0].strip()
    array_dims = type_str.count("[]")
    meta: dict = {"raw": type_str}

    if array_dims:
        inner = type_str[:type_str.find("[")].strip()
        inner_val, inner_meta = skeleton_for_type(inner, owner, ctx)
        meta["category"] = "array"
        meta["element"] = inner_meta
        return [inner_val], meta

    if head in STRING_LIKE:
        meta["category"] = "string"
        return "", meta
    if head in BOOL_LIKE:
        meta["category"] = "boolean"
        return False, meta
    if head in PRIMITIVE_ZERO:
        meta["category"] = "number"
        return 0, meta
    if head in DECIMAL_LIKE:
        meta["category"] = "decimal"
        meta["hint"] = "pass as string for hessian2 safety, e.g. \"0\""
        return "0", meta
    if head in DATE_LIKE:
        meta["category"] = "date"
        meta["hint"] = {
            "Date": "yyyy-MM-dd HH:mm:ss",
            "LocalDate": "yyyy-MM-dd",
            "LocalDateTime": "yyyy-MM-dd'T'HH:mm:ss",
        }.get(head, "ISO-8601")
        return None, meta
    if head == "Object":
        meta["category"] = "unknown"
        return None, meta

    args = parse_generic_args(type_str)

    if head in COLLECTION_LIKE:
        meta["category"] = "collection"
        if args:
            inner_val, inner_meta = skeleton_for_type(args[0], owner, ctx)
            meta["element"] = inner_meta
            return [inner_val], meta
        return [], meta

    if head in MAP_LIKE:
        meta["category"] = "map"
        if len(args) == 2:
            _, kmeta = skeleton_for_type(args[0], owner, ctx)
            vval, vmeta = skeleton_for_type(args[1], owner, ctx)
            meta["key"] = kmeta
            meta["value"] = vmeta
            return {"": vval}, meta
        return {}, meta

    fqn = resolve_fqn(head, owner, ctx.registry)
    if fqn and fqn in ctx.registry:
        target = ctx.registry[fqn]
        if target.kind == "enum":
            meta["category"] = "enum"
            meta["fqn"] = fqn
            meta["values"] = target.enum_constants
            return target.enum_constants[0] if target.enum_constants else "", meta
        if target.kind == "class":
            meta["category"] = "object"
            meta["fqn"] = fqn
            return skeleton_for_class(target, ctx), meta

    meta["category"] = "unresolved"
    if fqn:
        meta["fqn"] = fqn
    return {}, meta


def collect_fields(cls: ClassInfo, ctx: BuildCtx) -> List[FieldInfo]:
    seen: Set[str] = set()
    out: List[FieldInfo] = []
    chain: List[ClassInfo] = []
    cur = cls
    guard = 0
    while cur is not None and guard < 20:
        chain.append(cur)
        if not cur.superclass:
            break
        parent_fqn = resolve_fqn(cur.superclass, cur, ctx.registry)
        if parent_fqn and parent_fqn in ctx.registry:
            cur = ctx.registry[parent_fqn]
        else:
            break
        guard += 1
    for c in reversed(chain):
        for f in c.fields:
            if f.name in seen:
                continue
            seen.add(f.name)
            out.append(f)
    return out


def skeleton_for_class(cls: ClassInfo, ctx: BuildCtx) -> Dict[str, object]:
    if cls.fqn in ctx.visited:
        return {"$circular": cls.fqn}
    ctx.visited.add(cls.fqn)
    try:
        result: Dict[str, object] = {}
        for f in collect_fields(cls, ctx):
            val, _ = skeleton_for_type(f.java_type, cls, ctx)
            result[f.name] = val
        return result
    finally:
        ctx.visited.discard(cls.fqn)


def field_info_for_class(cls: ClassInfo, ctx: BuildCtx) -> List[dict]:
    items: List[dict] = []
    for f in collect_fields(cls, ctx):
        _, meta = skeleton_for_type(f.java_type, cls, ctx)
        items.append({
            "name": f.name,
            "type": f.java_type,
            "required": f.required,
            "comment": f.comment,
            "typeInfo": meta,
        })
    return items


def extract_required_hints_from_javadoc(doc: str, markers: List[str]) -> Dict[str, str]:
    """Return {fieldName: comment} for names marked required in method javadoc."""
    hints: Dict[str, str] = {}
    # <li>fieldName|必传|desc</li>
    for m in SOFA_LI_PAT.finditer(doc):
        name, tag = m.group(1), m.group(2)
        if contains_required_marker(tag, markers):
            hints[name] = m.group(0)
    # @param fieldName 必传 ...
    for m in PARAM_DOC_PAT.finditer(doc):
        name, body = m.group(1), m.group(2)
        if contains_required_marker(body, markers):
            hints.setdefault(name, body.strip())
    return hints


def apply_javadoc_required(field_info: List[dict], required_fields: Set[str]) -> None:
    for fi in field_info:
        if fi["name"] in required_fields:
            fi["required"] = True


# ---------------------------------------------------------------------------
# Facade interface detection
# ---------------------------------------------------------------------------

def _type_head(type_str: str) -> str:
    return type_str.split("<", 1)[0].split("[", 1)[0].strip()


def _is_optional_type(type_str: str) -> bool:
    head = _type_head(type_str)
    return head == "Optional" or head.endswith(".Optional")


def detect_envelope_optional_warning(return_type: str, owner: ClassInfo,
                                     registry: Dict[str, ClassInfo]) -> Optional[str]:
    """If the response wrapper exposes Optional helper APIs, return a short
    warning string for the skill. Raw mode is still preferred when stub jars
    are complete; generic mode cannot recover nested custom collection types."""
    head = _type_head(return_type)
    if not head or head in {"void", "Object"}:
        return None
    fqn = resolve_fqn(head, owner, registry)
    if not fqn or fqn not in registry:
        return None
    ci = registry[fqn]
    if ci.kind != "class":
        return None
    for f in ci.fields:
        if _is_optional_type(f.java_type):
            return f"{ci.simple_name}.{f.name}: {f.java_type}"
    for rt in ci.method_returns:
        if _is_optional_type(rt):
            return f"{ci.simple_name} exposes Optional getter ({rt})"
    return None


def is_facade_interface(ci: ClassInfo, suffixes: List[str]) -> bool:
    if ci.kind != "interface":
        return False
    for suf in suffixes:
        if ci.simple_name.endswith(suf):
            return True
    return False


def build_registry(source_roots: List[Path], markers: List[str]) -> Dict[str, ClassInfo]:
    registry: Dict[str, ClassInfo] = {}
    for jf in iter_java_files(source_roots):
        for info in parse_file(jf, markers):
            registry[info.fqn] = info
    return registry


def main() -> int:
    cfg = load_config(project_root=REPO_ROOT)
    source_roots = iter_source_roots(cfg, project_root=REPO_ROOT)
    if not source_roots:
        sys.stderr.write("[build_index] no facade source roots in config\n")
        return 1
    for r in source_roots:
        if not r.exists():
            sys.stderr.write(f"[build_index] WARNING source root does not exist: {r}\n")

    INDEX_DIR.mkdir(parents=True, exist_ok=True)
    for old in INDEX_DIR.glob("*.json"):
        old.unlink()

    suffixes = cfg.get("interfaceSuffixes") or ["Facade", "Api"]
    markers = cfg.get("requiredMarkers") or ["必传", "必填", "required"]

    registry = build_registry(source_roots, markers)
    summary = {
        "sourceRoots": [str(r.relative_to(REPO_ROOT)).replace("\\", "/") for r in source_roots],
        "interfaceSuffixes": suffixes,
        "services": [],
    }

    for fqn, ci in sorted(registry.items()):
        if not is_facade_interface(ci, suffixes):
            continue
        ctx = BuildCtx(registry=registry, markers=markers)
        svc_methods = []
        for m in ci.methods:
            required_hints = extract_required_hints_from_javadoc(m["javadoc"], markers)
            params_skel = []
            params_info = []
            for p in m["parameters"]:
                val, meta = skeleton_for_type(p["type"], ci, ctx)
                params_skel.append(val)
                entry = {
                    "name": p["name"],
                    "type": p["type"],
                    "typeInfo": meta,
                }
                if meta.get("fqn") and meta.get("category") == "object":
                    target = registry.get(meta["fqn"])
                    if target:
                        entry["fields"] = field_info_for_class(target, ctx)
                        apply_javadoc_required(entry["fields"], set(required_hints.keys()))
                if p["name"] in required_hints:
                    entry["requiredHint"] = required_hints[p["name"]]
                params_info.append(entry)
            method_entry = {
                "name": m["name"],
                "javadoc": m["javadoc"],
                "returnType": m["returnType"],
                "paramTypes": [p["type"].split("<", 1)[0].split("[", 1)[0] for p in m["parameters"]],
                "paramsSkeleton": params_skel,
                "paramsFieldInfo": params_info,
            }
            envelope_reason = detect_envelope_optional_warning(m["returnType"], ci, registry)
            if envelope_reason:
                method_entry["responseWarning"] = (
                    "response wrapper exposes Optional/helper getters; prefer raw mode "
                    "when stub jars are complete, generic mode may lose nested DTO types"
                )
                method_entry["responseWarningReason"] = envelope_reason
            svc_methods.append(method_entry)
        payload = {
            "service": fqn,
            "file": ci.file,
            "methods": svc_methods,
        }
        save_json(INDEX_DIR / f"{fqn}.json", payload)
        summary["services"].append({
            "service": fqn,
            "file": ci.file,
            "methods": [m["name"] for m in svc_methods],
        })
        print(f"  + {fqn}  ({len(svc_methods)} methods)")

    save_json(INDEX_DIR / "_index.json", summary)
    print(f"\n[build_index] wrote {len(summary['services'])} services to {display_path(INDEX_DIR)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
