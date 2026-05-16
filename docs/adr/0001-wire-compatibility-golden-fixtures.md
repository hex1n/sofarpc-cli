# ADR 0001: Wire Compatibility Golden Fixtures

Date: 2026-05-16

Status: accepted

## Context

`sofarpc-cli` owns a pure-Go `direct + bolt + hessian2` invoke path. The core
risk is not only Go unit-test correctness, but compatibility with the real Java
SOFARPC/Hessian runtime.

The current tests cover Go-side encoder/decoder round trips and fake BOLT
servers. That catches many regressions, but it cannot prove that Java can decode
Go-produced request content or that Go can decode Java-produced response
content. The existing golden fixture harness under
`internal/sofarpcwire/testdata/golden/` skips when no fixture is checked in, so
it is not yet a release gate.

## Decision

Use committed golden fixture JSON files as the default Go test gate, backed by a
Java generator/verifier that uses real SOFARPC/Hessian dependencies.

The default Go test path will not require Java. It will consume committed
fixtures and fail when required fixture coverage is missing.

The Java generator/verifier is optional for ordinary development and mandatory
for release validation. It is version-parameterized because downstream company
projects may use different SOFARPC versions. The fixture project keeps a
default baseline SOFARPC version for deterministic committed fixtures, while
manual validation can run the same generator/verifier against an explicit
support matrix.

It will live next to the wire test data:

```text
internal/sofarpcwire/testdata/
  golden/
  java-fixtures/
```

Fixture files use one unified JSON schema. The `kind` field selects the
verification direction:

- `request-content`: Hessian content for a `SofaRequest`; Java verifies the
  service, method, param types, and argument tree.
- `response-content`: Hessian content for a `SofaResponse` or Java throwable;
  Go verifies the decoded response tree. Response fixtures may include
  `want.appResponseJson`, which is matched as a semantic subset of the decoded
  Go tree to avoid overfitting to Java runtime implementation fields.

`contentHex` always stores the Hessian content, not the full BOLT frame. BOLT
framing remains covered by Go tests and the existing direct smoke test.

First-phase fixtures use `com.example.*` fake service and DTO classes. Real
project-derived, sanitized fixtures may be added later after the process is
stable.

The default baseline is not a claim that only one SOFARPC version is supported.
Supported versions must be declared and tested through the manual Java fixture
workflow, for example by passing `-Dsofarpc.version=5.4.0`,
`-Dsofarpc.version=5.7.6`, or `-Dsofarpc.version=5.8.0`.

## Consequences

Benefits:

- Go tests become a real compatibility gate without Java on every PR.
- Release validation has an explicit Java evidence chain.
- Fixture intent stays close to `internal/sofarpcwire`, where the wire contract
  is implemented.
- The project avoids another self-written Java Hessian verifier, which would
  only compare two custom implementations.

Costs:

- The repo gains a small Java test fixture project under `testdata`.
- Fixture generation depends on pinned Java SOFARPC/Hessian artifacts, selected
  by an explicit `sofarpc.version` Maven property.
- Wire behavior changes require regenerating fixture JSON and reviewing the
  semantic diff.

## Rules

- Missing `internal/sofarpcwire/testdata/golden/*.json` fixtures must fail Go
  tests.
- Missing `request-content` coverage must fail Go tests.
- Missing `response-content` coverage must fail Go tests.
- Unknown fixture kinds, empty `contentHex`, or invalid fixture JSON must fail
  Go tests.
- Default push/PR CI runs the Go golden tests only.
- A manual workflow runs the Java generator/verifier. It checks that generated
  baseline fixtures match committed fixtures and validates any declared
  compatibility matrix without committing every version's generated output.
- Release validation must run the manual Java fixture workflow.
