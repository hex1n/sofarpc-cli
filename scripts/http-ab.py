#!/usr/bin/env python3
"""Compare a read-only HTTP request across two independently identified deployments."""

import argparse
from collections import namedtuple
from contextlib import closing
from decimal import Decimal, InvalidOperation
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
from urllib.parse import urlsplit


LANES = ("a", "b")
MAX_RESPONSE = 2 * 1024 * 1024
HEADER_NAME = re.compile(r"[!#$%&'*+\-.^_`|~0-9A-Za-z]+")
BUILD_DIGEST = re.compile(
    r"(?:(?:[^@\s]+@)?sha256:)?([0-9a-f]{64})|(?:sha1:)?([0-9a-f]{40})",
    re.IGNORECASE,
)
CREDENTIAL_HEADERS = {"authorization", "cookie", "proxy-authorization", "set-cookie"}
FRAMING_HEADERS = {"host", "content-length", "transfer-encoding", "connection", "upgrade"}
MISSING = object()
AMBIGUOUS = object()
Response = namedtuple("Response", "status headers body", defaults=(None, (), MISSING))


def canonical(value):
    def encode(item):
        if isinstance(item, Decimal):
            return str(item)
        if isinstance(item, dict):
            return "{" + ",".join(json.dumps(key) + ":" + encode(item[key]) for key in sorted(item)) + "}"
        if isinstance(item, list):
            return "[" + ",".join(map(encode, item)) + "]"
        return json.dumps(item, allow_nan=False)

    return encode(value).encode("utf-8")


def fingerprint(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def object_at(parent, key):
    value = parent.get(key)
    if not isinstance(value, dict):
        raise ValueError(f"expected an object at {key}")
    return value


def read_object(path):
    value = decode(path.read_text(encoding="utf-8-sig"))
    if not isinstance(value, dict):
        raise ValueError("expected a JSON document object")
    return value


def decode(raw):
    def unique_object(pairs):
        value = dict(pairs)
        if len(value) != len(pairs):
            raise ValueError("duplicate JSON object keys")
        return value

    def invalid_constant(_value):
        raise ValueError("non-JSON numeric constant")

    try:
        return json.loads(raw, object_pairs_hook=unique_object, parse_float=Decimal, parse_constant=invalid_constant)
    except InvalidOperation as error:
        raise ValueError("invalid JSON number") from error


def identifier(value):
    return value.strip() if isinstance(value, str) and value.strip() else None


def build_id(value):
    identifier_value = identifier(value)
    digest = BUILD_DIGEST.fullmatch(identifier_value) if identifier_value is not None else None
    return (digest[1] or digest[2]).lower() if digest else identifier_value


def canonical_origin(value):
    if not isinstance(value, str):
        raise ValueError("expected an HTTP origin")
    parts = urlsplit(value)
    if (parts.scheme not in ("http", "https") or not parts.hostname or parts.username is not None
            or parts.path not in ("", "/") or parts.query or parts.fragment or parts.port == 0):
        raise ValueError("lane origin must contain only scheme, host, and optional port")
    host = parts.hostname
    if parts.scheme == "http" and not ipaddress.ip_address(host).is_loopback:
        raise ValueError("HTTP is supported only for loopback IP addresses")
    default = 443 if parts.scheme == "https" else 80
    host = f"[{host}]" if ":" in host else host
    return f"{parts.scheme}://{host}" + (f":{parts.port}" if parts.port not in (None, default) else "")


def normalize_headers(headers, credentials):
    if not isinstance(headers, dict):
        raise ValueError("headers must be objects")
    normalized = {}
    for name, value in headers.items():
        key = name.lower()
        if (not HEADER_NAME.fullmatch(name) or key in normalized or key in FRAMING_HEADERS
                or not isinstance(value, str) or "\r" in value or "\n" in value):
            raise ValueError("invalid, duplicate, or reserved header")
        if credentials and not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", value):
            raise ValueError("credential header values must be environment variable names")
        if not credentials and key in CREDENTIAL_HEADERS:
            raise ValueError("credential headers require an environment variable reference")
        normalized[key] = value
    return normalized


def validate_request(spec):
    method, path = spec.get("method"), spec.get("path")
    if (method not in ("GET", "POST") or not isinstance(path, str) or not path.startswith("/")
            or path.startswith("//") or not re.fullmatch(r"[!-~]+", path) or "#" in path or "\\" in path):
        raise ValueError("request requires GET or POST and an origin-relative URL path")
    if method == "POST" and (spec.get("readOnly") is not True or "json" not in spec):
        raise ValueError("POST requires readOnly=true and a JSON body")
    if method == "GET" and "json" in spec:
        raise ValueError("GET does not accept a body")
    if "json" in spec:
        canonical(spec["json"])


def validate_selector(selector, sensitive, assertion=False):
    keys = set(selector)
    if assertion and "equals" not in keys:
        raise ValueError("success selector requires equals")
    if assertion:
        keys.remove("equals")
        canonical(selector["equals"])
    if keys == {"header"}:
        name = selector["header"]
        if isinstance(name, str) and HEADER_NAME.fullmatch(name) and name.lower() not in sensitive:
            return
    elif keys == {"pointer"}:
        pointer = selector["pointer"]
        if isinstance(pointer, str) and (pointer == "" or pointer.startswith("/")) and not re.search(r"~(?![01])", pointer):
            return
    raise ValueError("selector requires one header name or JSON Pointer")


def read_scenario(path):
    scenario = read_object(path)
    lanes = object_at(scenario, "lanes")
    if set(lanes) != set(LANES):
        raise ValueError("lanes must be named a and b")
    sensitive = set(CREDENTIAL_HEADERS)
    for name in LANES:
        lane = object_at(lanes, name)
        lane["origin"] = canonical_origin(lane.get("origin"))
        lane["expectedBuild"] = build_id(lane.get("expectedBuild"))
        if lane["expectedBuild"] is None:
            raise ValueError("each lane requires an expected build ID")
        lane["headers"] = normalize_headers(lane.get("headers", {}), credentials=False)
        lane["credentialHeaders"] = normalize_headers(lane.get("credentialHeaders", {}), credentials=True)
        sensitive.update(lane["credentialHeaders"])
    a, b = (lanes[name] for name in LANES)
    if a["expectedBuild"] == b["expectedBuild"]:
        raise ValueError("expected builds must differ")
    if a["origin"] == b["origin"] and a["headers"] == b["headers"]:
        raise ValueError("lanes must differ by origin or static headers")
    for lane in lanes.values():
        if sensitive.intersection(lane["headers"]):
            raise ValueError("credential headers require an environment variable reference")
    validate_request(object_at(scenario, "request"))
    result = object_at(scenario, "result")
    validate_selector(object_at(result, "success"), sensitive, assertion=True)
    validate_selector(object_at(result, "data"), sensitive)
    if "auth" in scenario:
        auth = object_at(scenario, "auth")
        validate_request(object_at(auth, "request"))
        validate_selector(object_at(auth, "success"), sensitive, assertion=True)
        validate_selector(object_at(auth, "identity"), sensitive)
    version = object_at(scenario, "version")
    if set(version) not in ({"selector"}, {"correlation"}):
        raise ValueError("version requires a selector or correlation selector")
    version_selector = object_at(version, next(iter(version)))
    validate_selector(version_selector, sensitive)
    if "header" in version_selector and any(version_selector["header"].lower() in lane["headers"] for lane in lanes.values()):
        raise ValueError("the request must not supply a version evidence header")
    return scenario


def select(response, selector):
    if "header" in selector:
        name = selector["header"].lower()
        values = [value for key, value in response.headers if key.lower() == name]
        if len(values) > 1:
            return AMBIGUOUS
        return values[0] if values else MISSING
    value = response.body
    pointer = selector["pointer"]
    for token in pointer.split("/")[1:]:
        token = token.replace("~1", "/").replace("~0", "~")
        if isinstance(value, dict):
            value = value.get(token, MISSING)
        elif isinstance(value, list) and re.fullmatch(r"0|[1-9][0-9]*", token) and len(token) <= len(str(len(value))):
            index = int(token)
            value = value[index] if index < len(value) else MISSING
        else:
            return MISSING
    return value


def send(origin, spec, supplied_headers):
    parts = urlsplit(origin)
    secure = parts.scheme == "https"
    client = http.client.HTTPSConnection if secure else http.client.HTTPConnection
    headers = {"accept": "application/json", **supplied_headers}
    body = None
    if "json" in spec:
        headers.setdefault("content-type", "application/json")
        body = canonical(spec["json"])
    with closing(client(parts.hostname, parts.port or (443 if secure else 80), timeout=15)) as connection:
        try:
            connection.request(spec["method"], spec["path"], body=body, headers=headers)
            response = connection.getresponse()
            if not 200 <= response.status < 300:
                return Response(response.status)
            raw = response.read(MAX_RESPONSE + 1)
            result = MISSING
            if len(raw) <= MAX_RESPONSE:
                try:
                    result = decode(raw)
                except (ValueError, RecursionError):
                    pass
            return Response(response.status, response.getheaders(), result)
        except (OSError, ValueError, http.client.HTTPException):
            return Response()


def predicate(response, assertion):
    actual = select(response, assertion)
    return actual is not MISSING and actual is not AMBIGUOUS and canonical(actual) == canonical(assertion["equals"])


def capture(scenario, scenario_digest):
    observations, headers, identities = {}, {}, {}
    result = scenario["result"]
    auth = scenario.get("auth")
    version = scenario["version"]
    version_mode = next(iter(version))
    for name, lane in scenario["lanes"].items():
        record = {"origin": lane["origin"], "auth": "not-checked", "business": "not-requested"}
        observations[name] = record
        credentials = {key: os.environ.get(env) for key, env in lane["credentialHeaders"].items()}
        if any(not value for value in credentials.values()):
            record["auth"] = "credentials-missing"
            continue
        headers[name] = {**lane["headers"], **credentials}
        if auth is not None:
            reply = send(lane["origin"], auth["request"], headers[name])
            identity = select(reply, auth["identity"])
            if identifier(identity) is not None and predicate(reply, auth["success"]):
                identities[name] = identity
                record["auth"] = "verified"
            else:
                record["auth"] = "unproved"
    same_identity = None if auth is None else len(identities) == 2 and identities["a"] == identities["b"]
    if len(headers) == 2 and same_identity is not False:
        for name, record in observations.items():
            reply = send(record["origin"], scenario["request"], headers[name])
            if reply.status in (401, 403) or (reply.status is not None and 300 <= reply.status < 400):
                record["auth"] = "rejected-on-request"
                continue
            selected = select(reply, version[version_mode])
            combined = "header" in version[version_mode] and isinstance(selected, str) and "," in selected
            record["versionStatus"] = "ambiguous" if selected is AMBIGUOUS or combined else "missing"
            token = build_id(selected) if version_mode == "selector" else identifier(selected)
            if token is not None and not combined:
                record["versionStatus"] = "observed"
                if version_mode == "selector":
                    record["buildSha256"] = fingerprint(token)
                else:
                    record["correlationSha256"] = fingerprint(token)
            data = select(reply, result["data"])
            record["business"] = "unproved"
            if predicate(reply, result["success"]) and data is not MISSING and data is not AMBIGUOUS:
                record["dataSha256"] = fingerprint(data)
                record["business"] = "verified"
    return {"scenarioSha256": scenario_digest, "sameIdentity": same_identity, "lanes": observations}


def route_proof(scenario, observations, receipt_file):
    version = scenario["version"]
    if any(lane.get("versionStatus") != "observed" for lane in observations.values()):
        return "response-evidence-ambiguous" if any(lane.get("versionStatus") == "ambiguous" for lane in observations.values()) else "response-evidence-missing"
    if "selector" in version:
        builds = [observations[name].get("buildSha256") for name in LANES]
    else:
        receipts = read_object(receipt_file).get("receipts")
        if not isinstance(receipts, list) or not all(isinstance(receipt, dict) for receipt in receipts):
            raise ValueError("expected an array of receipts")
        correlations = [identifier(observations[name].get("correlationSha256")) for name in LANES]
        if None in correlations or correlations[0] == correlations[1]:
            return "response-correlation-not-unique"
        builds = []
        for name, correlation in zip(LANES, correlations):
            matches = [receipt for receipt in receipts if fingerprint(identifier(receipt.get("correlation"))) == correlation]
            if len(matches) != 1:
                return "receipt-ambiguous" if matches else "receipt-missing"
            receipt = matches[0]
            build = build_id(receipt.get("build"))
            if (receipt.get("origin") != scenario["lanes"][name]["origin"] or build is None
                    or identifier(receipt.get("source")) is None):
                return "receipt-does-not-prove-route"
            builds.append(fingerprint(build))
    if builds[0] == builds[1]:
        return "same-observed-build"
    if builds != [fingerprint(scenario["lanes"][name]["expectedBuild"]) for name in LANES]:
        return "unexpected-observed-build"
    return None


def compare(scenario, collected, receipt_file):
    observations = object_at(collected, "lanes")
    if (collected.get("scenarioSha256") != fingerprint(scenario) or set(observations) != set(LANES)
            or any(object_at(observations, name).get("origin") != scenario["lanes"][name]["origin"] for name in LANES)):
        raise ValueError("capture does not match scenario")
    auth_state = "verified" if "auth" in scenario else "not-checked"
    if (("auth" in scenario and collected.get("sameIdentity") is not True)
            or any(lane.get("auth") != auth_state for lane in observations.values())):
        return {"status": "authentication-unproved"}
    problem = route_proof(scenario, observations, receipt_file)
    if problem:
        return {"status": "routing-unproved", "reason": problem}
    hashes = [observations[name].get("dataSha256") for name in LANES]
    if (any(lane.get("business") != "verified" for lane in observations.values())
            or not all(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) for value in hashes)):
        return {"status": "business-response-unproved"}
    return {"status": "match" if hashes[0] == hashes[1] else "business-difference"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_subparsers(dest="mode", required=True)
    for mode in ("capture", "compare"):
        command = modes.add_parser(mode)
        command.add_argument("scenario", type=Path)
        command.add_argument("capture", type=Path)
        if mode == "compare":
            command.add_argument("receipts", nargs="?", type=Path)
    args = parser.parse_args()
    try:
        scenario = read_scenario(args.scenario)
        if args.mode == "capture":
            scenario_digest = fingerprint(scenario)
            with args.capture.open("x", encoding="utf-8") as output:
                json.dump(capture(scenario, scenario_digest), output, indent=2)
                output.write("\n")
            print(json.dumps({"status": "captured"}))
            return 0
        if (args.receipts is not None) != ("correlation" in scenario["version"]):
            raise ValueError("receipts file is required only for correlation mode")
        outcome = compare(scenario, read_object(args.capture), args.receipts)
    except (OSError, ValueError, RecursionError):
        print(json.dumps({"status": "invalid-input"}), file=sys.stderr)
        return 2
    print(json.dumps(outcome))
    return {"match": 0, "business-difference": 1}.get(outcome["status"], 2)


if __name__ == "__main__":
    sys.exit(main())
