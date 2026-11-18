# HTTP A/B comparison

Use `http-ab.py` to send the same read-only HTTP request to two configured deployment lanes and compare their business data. It requires Python 3.8 or later and uses only the standard library. Use approved test targets, credentials and comparable test data.

## Scenario

```json
{
  "lanes": {
    "a": {
      "origin": "https://test-a.example.test",
      "expectedBuild": "build-id-a",
      "headers": {"Origin": "https://test-a.example.test"},
      "credentialHeaders": {"Cookie": "TEST_A_COOKIE"}
    },
    "b": {
      "origin": "https://test-b.example.test",
      "expectedBuild": "build-id-b",
      "headers": {"Origin": "https://test-b.example.test"},
      "credentialHeaders": {"Cookie": "TEST_B_COOKIE"}
    }
  },
  "request": {"method": "POST", "path": "/query", "readOnly": true, "json": {"filter": "sample"}},
  "auth": {
    "request": {"method": "GET", "path": "/session"},
    "success": {"pointer": "/ok", "equals": true},
    "identity": {"pointer": "/user/id"}
  },
  "result": {
    "success": {"pointer": "/ok", "equals": true},
    "data": {"pointer": "/data"}
  },
  "version": {"selector": {"header": "X-Deployment-Build"}}
}
```

Both lanes must have distinct expected immutable build IDs. They can share an origin if their explicit static headers differ and select authorized routes. The actual build is verified from each response or an independent receipt. Full hexadecimal Git SHA and SHA-256 digest IDs are compared case-insensitively. An OCI image reference with a repository and SHA-256 digest is compared by its digest. Other IDs are case-sensitive. Use an origin without a path and a request path that starts with `/`. Plain HTTP is for literal loopback IP addresses. `GET` has no body. A `POST` requires a JSON body and `readOnly: true`, which declares a read-only business operation the test owner has verified.

Put credential values in the designated process environment variables. The scenario stores only their variable names. `auth` is optional. When present, both auth responses must meet the predicate and return the same nonempty string identity before the business requests run. A predicate compares a selected response value with `equals` using JSON types. Selectors use a case-insensitive `header` name or an RFC 6901 JSON `pointer` such as `/result/items/0`. Use `"pointer": ""` to select the whole JSON value.

## Capture and compare

```text
python scripts/http-ab.py capture scenario.json capture.json
python scripts/http-ab.py compare scenario.json capture.json
```

`capture` sends the requests without redirects or retries and writes a new file. It records observed status and fingerprints without storing credentials, account identities or response bodies. A successful capture command means the file was written. `compare` runs offline, verifies that the scenario still matches the capture, checks both observed builds and then compares successful business data. It preserves array order and ignores object key order. Exit codes are `0` for a match, `1` for a business difference, and `2` for an unproved comparison or invalid input.

If the deployment exposes a response correlation identifier instead of a build ID, configure `"version": {"correlation": {"header": "X-Request-ID"}}` or a JSON Pointer to that identifier. The capture stores its SHA-256 fingerprint. Obtain the original identifier independently from trusted gateway or provider records. Each receipt must bind it and the configured origin to the build that handled the exact request. Use `source` to reference the authoritative evidence. Verify before the run that the selected metadata identifies the handling deployment and comes from the server.

```json
{"receipts": [
  {"origin": "https://test-a.example.test", "correlation": "trace-a", "build": "build-id-a", "source": "provider trace and deployment record A"},
  {"origin": "https://test-b.example.test", "correlation": "trace-b", "build": "build-id-b", "source": "provider trace and deployment record B"}
]}
```

```text
python scripts/http-ab.py compare scenario.json capture.json receipts.json
```

An absent or ambiguous response identifier or receipt, identical observed builds, or a mismatched observed build prevents comparison. Build and correlation selectors must point to non-secret deployment evidence.
