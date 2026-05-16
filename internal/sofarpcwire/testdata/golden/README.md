# SOFARPC wire golden fixtures

Drop SOFARPC/Hessian wire fixtures here as `*.json`. These fixtures are the
default Go test gate for Java wire compatibility.

`contentHex` is always the Hessian payload inside the BOLT frame, not the full
BOLT frame.

## Fixture shape

```json
{
  "name": "response-success-bigdecimal",
  "description": "Captured from the Java fixture generator",
  "kind": "response-content",
  "contentHex": "4f...",
  "want": {
    "isError": false,
    "appResponseType": "com.example.Result",
    "appResponseJson": {
      "type": "com.example.Result",
      "fields": {
        "success": true
      }
    }
  }
}
```

Supported `kind` values:

- `request-content`: Go-produced `SofaRequest` Hessian content. The Go test
  validates fixture schema and committed coverage; the Java verifier decodes it
  and checks semantic expectations.
- `response-content`: Java-produced `SofaResponse` or throwable Hessian
  content. The Go test decodes it with `DecodeResponse` and checks semantic
  expectations. `want.appResponseJson` is matched as a semantic subset of the
  decoded Go tree, so fixtures can lock important fields without depending on
  every Java runtime implementation detail.

The Go test fails when no JSON fixtures are present, when either required kind
is missing, when `contentHex` is empty, or when an unknown field/kind appears.

## Request fixture example

```json
{
  "name": "request-dto-bigdecimal-list",
  "description": "Go request content decoded by the Java verifier",
  "kind": "request-content",
  "contentHex": "4f...",
  "want": {
    "service": "com.example.FixtureFacade",
    "method": "query",
    "paramTypes": ["com.example.FixtureRequest"],
    "targetServiceUniqueName": "com.example.FixtureFacade:1.0",
    "argsJson": [
      {
        "type": "com.example.FixtureRequest",
        "fields": {
          "amount": {
            "type": "java.math.BigDecimal",
            "value": "1000.5"
          }
        }
      }
    ]
  }
}
```

## Response fixture example

```json
{
  "name": "response-success-dto",
  "description": "Java response content decoded by Go",
  "kind": "response-content",
  "contentHex": "4f...",
  "want": {
    "isError": false,
    "appResponseType": "com.example.FixtureResult",
    "appResponseJson": {
      "type": "com.example.FixtureResult",
      "fields": {
        "message": "ok",
        "success": true
      }
    }
  }
}
```

## Error response fixture example

```json
{
  "name": "response-error",
  "description": "Java SofaResponse error decoded by Go",
  "kind": "response-content",
  "contentHex": "4f...",
  "want": {
    "isError": true,
    "errorMsg": "fixture error"
  }
}
```

Keeping these fixtures Java-backed is intentional: they prove compatibility
beyond Go encoder/decoder round trips.

## Regeneration and version matrix

Committed fixtures use the repository's baseline Java fixture version. Company
projects may run different SOFARPC versions, so the Java fixture project accepts
`-Dsofarpc.version=...` for manual compatibility checks.

For matrix runs, generate fixtures into a temporary directory and point the Go
test at it with `SOFARPCWIRE_GOLDEN_DIR`. Do not commit duplicate fixture files
for every supported SOFARPC version unless a version-specific behavior needs a
documented fixture.
