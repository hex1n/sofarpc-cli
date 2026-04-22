---
title: Contract-Aware Argument Normalization
date: 2026-04-21
status: implemented
landedAt: 2026-04-22
landedIn: internal/core/contract/normalize.go
---

# Contract-Aware Argument Normalization

## 1. Problem Frame

`sofarpc-cli` 现在已经能用 pure-Go 路径完成真实 generic invoke，但入参体验仍然偏“协议专家模式”。

当前行为的关键断点是：

- [internal/mcp/invoke.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/mcp/invoke.go) 只负责把 `args` 解析成 `[]any`
- [internal/core/invoke/plan.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/invoke/plan.go) 在 facade 模式下只做 overload 选择和 skeleton 回填
- [internal/sofarpcwire/wire.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/wire.go) / [encoder.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/encoder.go) 只做 wire-level 归一化，不理解 contract 字段类型

这导致：

- `BigDecimal` 这类类型仍要求用户显式写 `{"@type":"java.math.BigDecimal","value":"..."}`
- 嵌套 DTO、`List<DTO>`、`Map<String, DTO>` 的 `@type` 注入仍依赖用户自己知道 Java 类型
- facade 模式下虽然已经有 contract store，但 `invoke` 没有真正利用它来帮用户补齐类型信息

目标不是做一个“万能 Java 对象映射器”，而是让 **facade-backed invoke** 默认把常见 Java 类型自动归一化到 wire-ready 的 canonical shape。

## 2. Scope

本计划只覆盖 **facade-backed invoke 的参数归一化**。

在范围内：

- 按方法 `paramTypes` 和 DTO 字段类型递归归一化用户参数
- 自动补齐 root DTO 和 nested DTO 的 `@type`
- 自动包装 `BigDecimal` / `BigInteger`
- 自动处理 `List<T>` / `Set<T>` / `Map<String, V>` / enum / 常见 number wrapper
- 让 `describe` 和 `invoke` 尽量共享同一套 Java type 解析与字段解析逻辑

不在范围内：

- trusted mode 的自动推断
- registry 路径
- 任意 Java 类型全覆盖
- `byte[]`、多维数组、多态层级校验、`record` / `sealed` / 内部类深支持
- 改写 wire 层的 Hessian 协议结构

## 3. Current Constraints

### 3.1 必须保住的行为

- trusted mode 仍然允许用户完全手工控制 `paramTypes + args`
- [internal/core/invoke/plan.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/invoke/plan.go) 现有 dry-run / replay / session 语义不能被改坏
- [internal/sofarpcwire/wire.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/wire.go) 仍然只消费通用 Go 值，不直接依赖 contract store

### 3.2 当前已经可用的基础

- [internal/core/contract/skeleton.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/skeleton.go) 已有 Java type 字符串解析的半套逻辑
- [internal/javatype/role.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/javatype/role.go) 已有 invocation-time 类型角色分类
- [internal/javatype/placeholder.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/javatype/placeholder.go) 已有 describe-time placeholder 分类
- [internal/sourcecontract/store.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sourcecontract/store.go) 已能从源码构建 DTO / method / field 基础信息

## 4. Decisions

### 4.1 只在 facade-backed mode 自动归一化

`BuildPlan` 只有在 `facade != nil` 且成功 resolve 了具体 overload 后，才运行 contract-aware normalization。

trusted mode 保持现状：

- 不自动注入 `@type`
- 不自动包装 `BigDecimal`
- 不新增额外验证

理由：

- trusted mode 的设计目标就是允许 agent/用户完全掌控 payload
- 当前最大的价值来自“项目内已知 facade”场景，不值得为 trusted mode 强加猜测逻辑

### 4.2 归一化产物停留在 canonical JSON-like shape，而不是 wire 内部类型

normalizer 的输出仍然是 `[]any`，其内部使用：

- 标量
- `[]any`
- `map[string]any`
- `{"@type":"...","value":"..."}` 这类 canonical typed object shape

wire 层继续负责把 canonical shape 转成 `javaTypedObject` / Hessian 对象。

理由：

- dry-run plan 需要可读、可序列化
- replay 不应该持有 wire 私有类型
- `core/invoke` 不该依赖 Hessian 细节

### 4.3 共享 Java type parser，不再在 skeleton 里私藏解析逻辑

需要从 [internal/core/contract/skeleton.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/skeleton.go) 抽出共享的 type parser。

建议新增：

- [internal/core/contract/typespec.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/typespec.go)
- [internal/core/contract/typespec_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/typespec_test.go)

建议模型：

```go
type TypeSpec struct {
	Base       string
	Args       []TypeSpec
	ArrayDepth int
	Wildcard   WildcardKind
}
```

`skeleton` 和 `normalizer` 都基于这个 AST 工作。

### 4.4 第一阶段只做“安全、确定”的自动归一化

第一阶段支持矩阵：

- `java.lang.String` / `CharSequence`
- `boolean` / `Boolean`
- `Byte` / `Short` / `Integer` / `Long`
- `Float` / `Double`
- `BigDecimal` / `BigInteger`
- enum
- `List<T>` / `Set<T>` / `Collection<T>`
- `Map<String, V>`
- DTO / nested DTO / `List<DTO>`

第一阶段不主动承诺：

- `byte[]`
- 原生 Java array 精确语义
- `Map` 非字符串 key
- `LocalDateTime` / `Date` 的强类型对象编码
- 多态字段的 subtype assignability 校验

这些类型先保持当前行为或作为第二阶段补充。

### 4.5 默认是 best-effort normalization，不立刻变成强校验器

第一阶段只在这类明显结构错误时返回 `input.args-invalid`：

- 方法参数声明是对象，用户传了标量
- 声明是集合，用户传了对象
- 声明是 `Map<String, V>`，用户传了非对象

其余情况优先做无损 canonicalization：

- `BigDecimal` 的 number/string 自动转 typed object
- DTO map 自动补 `@type`
- nested DTO/list/map 递归处理

理由：

- 现在很多调用虽然“不标准”，但服务端 generic revise 还能吃下去
- 第一阶段目标是减少手写 Java 类型负担，不是突然把 invoke 变成严格 schema validator

## 5. Implementation Units

### Unit A: 抽出共享 type parser

文件：

- 新增 [internal/core/contract/typespec.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/typespec.go)
- 新增 [internal/core/contract/typespec_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/typespec_test.go)
- 修改 [internal/core/contract/skeleton.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/skeleton.go)

内容：

- 把 `parseGenerics` / `splitTopLevelCommas` / wildcard 解析提取成共享 parser
- `skeleton.go` 改为消费 `TypeSpec`，不再自己拆字符串

验收：

- 现有 skeleton tests 全绿
- `TypeSpec` 能表达：
  - `java.util.List<com.foo.Req>`
  - `java.util.Map<java.lang.String, com.foo.Req>`
  - `com.foo.Req[][]`
  - `? extends com.foo.Base`

### Unit B: 增加 contract field resolver

文件：

- 新增 [internal/core/contract/fields.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/fields.go)
- 新增 [internal/core/contract/fields_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/fields_test.go)

内容：

- 提供 `ResolvedFields(store, fqn)` 一类 helper
- 按 `class -> superclass` 合并字段
- 避免 DTO 继承场景下 normalizer 看不到父类字段

验收：

- 继承 DTO 的字段能被完整枚举
- 未知 class 返回空而不是 panic

### Unit C: 实现 contract-aware normalizer

文件：

- 新增 [internal/core/contract/normalize.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/normalize.go)
- 新增 [internal/core/contract/normalize_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/normalize_test.go)

建议入口：

```go
func NormalizeArgs(paramTypes []string, args []any, store Store) ([]any, error)
```

内部递归：

- `normalizeForType(spec TypeSpec, raw any, store Store) (any, error)`
- scalar normalization
- container normalization
- DTO normalization
- enum normalization

建议行为：

- root DTO:
  - 输入 `map[string]any{"amount":1000.5}`
  - 输出 `map[string]any{"@type":"com.foo.Req","amount":{"@type":"java.math.BigDecimal","value":"1000.5"}}`
- nested DTO:
  - 自动补字段对象 `@type`
- `List<DTO>`:
  - slice 内每个元素递归补 `@type`
- `Map<String, DTO>`:
  - value 递归补 `@type`
- enum:
  - 字符串保持为 enum constant
- `BigDecimal` / `BigInteger`:
  - number 或 string 自动包装成 typed object

### Unit D: 把 normalizer 接进 BuildPlan

文件：

- 修改 [internal/core/invoke/plan.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/invoke/plan.go)
- 修改 [internal/core/invoke/plan_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/invoke/plan_test.go)

改法：

- `resolveArgs(...)` 在 `userArgs != nil` 且 `facade != nil` 时调用 `contract.NormalizeArgs`
- skeleton 路径不变
- trusted mode 路径不变

关键策略：

- 不改 `ArgSource` 语义，仍然保持 `"user"` / `"skeleton"`
- 让 dry-run 通过 `plan.Args` 直接展示归一化结果

### Unit E: 保持 wire 层尽量不动，只补必要适配

优先目标是 **不修改**：

- [internal/sofarpcwire/wire.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/wire.go)
- [internal/sofarpcwire/encoder.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/encoder.go)

原因：

- 它们现在已经能把 canonical `@type` map 转成 typed Hessian object
- 风险不在 wire，风险在 pre-wire canonicalization

只有在新 normalizer 暴露出新的 canonical shape 缺口时，才追加最小适配。

### Unit F: 文档更新

文件：

- 修改 [README.md](/C:/Users/hexin/Desktop/234/sofarpc-cli/README.md)
- 修改 [README.zh-CN.md](/C:/Users/hexin/Desktop/234/sofarpc-cli/README.zh-CN.md)
- 修改 [docs/architecture.md](/C:/Users/hexin/Desktop/234/sofarpc-cli/docs/architecture.md)

内容：

- 明确说明 facade-backed invoke 会自动归一化常见 Java 类型
- 明确 trusted mode 仍要求显式 `@type`
- 给出 `BigDecimal` 从裸 number 到 canonical typed object 的示例

## 6. Test Plan

### 6.1 Contract normalizer 单测

文件：

- [internal/core/contract/normalize_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/contract/normalize_test.go)

场景：

1. root DTO 自动补 `@type`
2. nested DTO 自动补 `@type`
3. `List<DTO>` 每个元素自动补 `@type`
4. `Map<String, DTO>` 的 value 自动补 `@type`
5. `BigDecimal` number 自动包装
6. `BigInteger` string 自动包装
7. enum string 保持常量
8. 继承 DTO 能识别父类字段
9. trusted-unknown field 保留原值
10. 结构错误返回 `input.args-invalid`

### 6.2 BuildPlan 集成测试

文件：

- [internal/core/invoke/plan_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/core/invoke/plan_test.go)

场景：

1. facade 模式下用户传裸 `1000.5`，plan 中变成 `BigDecimal` typed object
2. facade 模式下 `List<DTO>` 自动归一化
3. trusted mode 下同样输入保持原样，不做自动补齐

### 6.3 MCP dry-run 测试

文件：

- [internal/mcp/invoke_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/mcp/invoke_test.go)

场景：

1. `sofarpc_invoke dryRun=true` 返回的 `plan.Args` 包含自动补齐后的 canonical shape
2. 从 `@file` 读取 args 仍然能触发相同的 normalization

### 6.4 Wire 保底测试

文件：

- [internal/sofarpcwire/wire_test.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sofarpcwire/wire_test.go)

场景：

1. normalizer 产出的 canonical `BigDecimal` 仍能编码成 typed Hessian object
2. `List<DTO>` 归一化结果仍能被 `BuildGenericRequest` 正确编码

## 7. Sequencing

1. 先做 `TypeSpec` 抽取，不改用户可见行为
2. 再做 `ResolvedFields`
3. 再做 `NormalizeArgs`
4. 最后把 normalizer 接进 `BuildPlan`
5. 完成后再更新 README / architecture

这样做的原因是：

- type parser 和 field resolver 先稳定，normalizer 才不会变成第二套字符串拼接逻辑
- `BuildPlan` 最后接入，可以把风险限制在最后一步

## 8. Risks

### 8.1 sourcecontract 字段信息不完整

[internal/sourcecontract/store.go](/C:/Users/hexin/Desktop/234/sofarpc-cli/internal/sourcecontract/store.go) 目前还不是完整 Java parser。

风险：

- 内部类
- 更复杂泛型
- 某些继承场景

控制：

- normalizer 对未知 class 或未知字段采用保守策略
- 不因为缺元数据就 panic 或整包失败

### 8.2 过度校验破坏当前可用 payload

风险：

- 现在服务端能接的“宽松 payload”被本地提前拒绝

控制：

- 第一阶段 best-effort
- 只在明显容器/对象结构错误时返回 `input.args-invalid`
- trusted mode 完全不动

### 8.3 归一化与 skeleton 漂移

风险：

- `describe` 给的 skeleton 和 `invoke` 真正接受的 canonical shape 不一致

控制：

- 共享 `TypeSpec`
- 能复用字段解析就不要分叉实现

## 9. Definition of Done

以下条件全部满足，这个改动才算完成：

1. facade-backed `sofarpc_invoke dryRun` 能自动输出 canonical typed args
2. trusted mode 行为不变
3. `BigDecimal`、`List<DTO>`、`Map<String, DTO>` 有稳定单测
4. `go test ./...` 通过
5. README 明确写清 facade mode 与 trusted mode 的差异

## 10. Recommended First Cut

第一刀只做这三类：

- `BigDecimal` / `BigInteger`
- DTO / nested DTO
- `List<DTO>`

原因：

- 这是当前真实 pain point 最大、收益最直接的一组
- 也是你已经在 `salesfundmp` 里验证过的真实调用路径
- 做完这三类，agent 侧手写 `@type` 的负担会明显下降

等第一刀稳定后，再补：

- `Map<String, V>`
- enum
- `Set<T>`
- 日期时间类型
