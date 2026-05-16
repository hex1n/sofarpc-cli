---
title: Contract Generics And Overload Resolution
date: 2026-05-16
status: implemented
---

# Contract Generics And Overload Resolution

## 1. Problem Frame

`sofarpc_describe` depends on Java source scanning to pick overloads and render
editable JSON skeletons. Many SOFARPC projects use generic base facades and
generic request/result wrappers:

```java
interface BaseFacade<T> {
    Result<T> query(T request);
}

interface UserFacade extends BaseFacade<UserRequest> {}
```

Before this change, the source scanner resolved the base method eagerly inside
`BaseFacade<T>`, so `T` became its bound, often `java.lang.Object`. Downstream
effects:

- `sofarpc_describe` could not select the inherited method with concrete
  `UserRequest` paramTypes.
- skeleton generation fell back to an `Object`-like shape instead of the real
  DTO.
- overload disambiguation required exact strings even when the caller supplied
  a subtype that Java could pass to a declared base parameter.

## 2. Decisions

- Preserve generic metadata in the shared Java model:
  - class/interface `typeParams`
  - field `typeTemplate`
  - method `paramTypeTemplates`
  - method `returnTypeTemplate`
- Materialize source classes with both a conservative resolved type and the
  original generic template where they differ.
- During inherited method and field traversal, bind type arguments from
  `extends` / `implements` clauses and substitute templates.
- Support method-level generic bounds such as
  `<T extends BaseRequest> Result<T> query(T request)`.
- Keep overload selection conservative:
  - exact paramTypes match first
  - assignable match only when exact match fails
  - multiple assignable matches remain ambiguous

## 3. Runtime Contract

Assignable overload matching is a selection aid only. The invoke plan still
uses the selected method's declared `paramTypes` as the SOFARPC wire signature.
If the user supplies an explicit subtype payload via `@type`, normalization
keeps the subtype payload while validating that it is assignable to the
declared parameter type.

Example:

```java
class UserRequest extends BaseRequest {}
interface Svc {
    Result query(BaseRequest request);
}
```

If the caller asks for `paramTypes=["com.foo.UserRequest"]`, the resolver may
select `query(BaseRequest)`, but the generated plan sends:

```json
{
  "paramTypes": ["com.foo.BaseRequest"],
  "args": [
    {
      "@type": "com.foo.UserRequest"
    }
  ]
}
```

## 4. Implemented Tests

- `internal/sourcecontract`:
  - generic interface argument substitution through source scanning
  - method-level generic bounds with multi-line annotated params
- `internal/core/contract`:
  - inherited generic method substitution
  - inherited generic field substitution
  - exact overload match wins over assignable match
  - ambiguous assignable overloads remain ambiguous
- `internal/core/invoke`:
  - assignable input paramTypes select the declared wire signature

## 5. Follow-ups

- Consider assignability for generic type arguments only after there are real
  project examples. The current matcher compares the outer type and Java
  inheritance chain; it does not try to prove deep generic variance.
- Consider surfacing a small `selectionReason` field in describe/invoke output
  if agents need to explain why assignable matching picked a declared base
  signature.
