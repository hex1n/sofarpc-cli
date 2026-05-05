# Java SOFARPC golden fixtures

Drop Java-produced SOFARPC/Hessian response-content fixtures here as `*.json`.
The Go test harness decodes every fixture with `kind: "response-content"`.

Fixture shape:

```json
{
  "name": "success-bigdecimal",
  "description": "Captured from a Java SOFARPC provider/client pair on JDK8",
  "kind": "response-content",
  "contentHex": "4f...",
  "want": {
    "isError": false,
    "appResponseType": "com.example.Result"
  }
}
```

Use `contentHex` for the Hessian payload inside the BOLT response frame, not
the full BOLT frame. Keeping these fixtures Java-generated is intentional: they
prove wire compatibility beyond Go encoder/decoder round trips.
