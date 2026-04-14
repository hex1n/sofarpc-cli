# cases/

One file per `<Service>_<method>.json`. Each file holds multiple named scenarios
the agent (or human) can replay. Layout:

```json
{
  "service": "com.foo.bar.ExampleFacade",
  "method": "query",
  "paramTypesSnapshot": ["com.foo.bar.QueryRequest"],
  "cases": [
    {
      "name": "happy",
      "notes": "typical success path",
      "context": null,
      "payloadMode": null,
      "timeoutMs": null,
      "params": [
        { "id": 1, "fromDate": "20260414" }
      ]
    },
    {
      "name": "empty_list",
      "notes": "edge: empty id list should return empty data, not error",
      "context": null,
      "payloadMode": null,
      "timeoutMs": null,
      "params": [
        { "ids": [] }
      ]
    }
  ]
}
```

* `context` / `payloadMode` / `timeoutMs` = `null` means "use default from
  manifest / --context / --payload-mode override". Set a value only when the
  scenario must pin it.
* `params` is always a JSON array matching the method's formal parameter
  arity. It goes straight into `sofarpc call -data ...`.
* Run all cases: `sofarpc facade run-cases`
  Run a subset: `sofarpc facade run-cases --filter <ServiceShortName>`
  Point at another project: `sofarpc facade run-cases --project <root>`
  Save results: `sofarpc facade run-cases --save` → writes `_runs/<case>-<name>.json`
