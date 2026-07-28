# ISSUE-001：CLI `call -plan` 重放路径未做嵌套类型名救援

| 字段 | 值 |
|---|---|
| Issue ID | ISSUE-001 |
| Type | product defect |
| Severity | P1（数据无损，但复现原 bug 的伪系统异常，且现象与"服务端坏了"无法区分） |
| Disposition | **OPEN** |
| Affected scenarios / edges | S5；边 J6→J1 |

## Expected

重放一份修复前捕获的 plan 存档（嵌套 `@type` 固化为源码规范点号名）时，执行前应把 `Args` 与 `ParamTypes` 中的嵌套类型名救援为 JVM 二进制名，服务端应能正确实例化 `CustomTimeWindow` 并进入业务校验，返回 `errorCode 003`。

依据：`internal/mcp/replay.go:76-77` 对 MCP replay 路径已实现该救援，并有单测 `TestReplay_RewritesDotNestedTypeNamesBeforeExecution` 覆盖。

## Actual

CLI `sofarpc-mcp call -plan <存档>` 返回 `errorCode 000 系统异常,请稍后再试!`——与修复前的原 bug 表现完全一致。

## Evidence / scene

- 存档（点号名，执行后未被篡改，已保留）：`scripts/s5-plan-dotform.archive.json`
- 原始输出：`evidence/s5-replay.out.json`

```
ok        : true
errorCode : 000 | errorMsg: 系统异常,请稍后再试!
transport : direct-bolt | responseStatus: 0
```

`responseStatus: 0` + `transport: direct-bolt` 证明传输层健康，失败发生在服务端反序列化/校验层，不是网络问题。

## Suspected code area

`cmd/sofarpc-mcp/call.go:93`

```go
outcome, err := invoke.Execute(context.Background(), plan, "call")
```

该行直接执行从文件读入的 plan，**没有**对应 `internal/mcp/replay.go:76-77` 的救援：

```go
execPlan.Args = contract.RewriteWireTypeNames(execPlan.Args, store)
execPlan.ParamTypes = contract.WireParamTypes(execPlan.ParamTypes, store)
```

两条路径同源（都是"重放已捕获 plan"），修复只覆盖了 MCP 一侧。此留白在 code-diagnose run `code-diagnose-task-20260728` 的最终报告「已知留白」中已被静态识别，本次执行为其提供了**运行时证据**，性质从"已知留白"升级为"可复现缺陷"。

## Reproduction steps

```bash
# 1. 取一份 contract-assisted 的 plan，把嵌套 @type 改回点号名（模拟修复前存档）
sofarpc-mcp call -project "C:/Users/hexin/Desktop/project/fundsalesmrksupport" \
  -service com.thfund.sales.fundsalesmrksupport.facade.background.SeasonBgFacade \
  -method upsertSeasonTimeWindow -args-file scripts/s1-default-no-type.args.json -dry-run > plan.json
# 手工把 plan.args[0].customTimeWindows[0]["@type"] 的 $CustomTimeWindow 改成 .CustomTimeWindow

# 2. 重放
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call \
  -project "C:/Users/hexin/Desktop/project/fundsalesmrksupport" -plan plan.json
# 实际：errorCode 000    期望：errorCode 003
```

## Fix constraints

- 与 `mcp/replay.go` 保持同一语义：只重写执行副本，**不得**篡改用户的存档文件（S5 已断言存档未被就地修改，修复后须保持）。
- store 不可解析时优雅降级为原样执行，不得让此前能重放的存档变成报错。
- `call.go` 当前没有 contract store 句柄，需按 `-project` 解析项目 store（与 `callArgs` 路径一致的解析方式）；store 解析失败时按上一条降级。
- 复用 `contract.RewriteWireTypeNames` / `contract.WireParamTypes`，不要另写一套转换。

## Verification command or scenario

重跑 S5：

```bash
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call \
  -project "C:/Users/hexin/Desktop/project/fundsalesmrksupport" \
  -plan <run>/scripts/s5-plan-dotform.archive.json
```

期望 `result.fields.errorCode == "003"`，且存档文件内容仍为点号名。

建议同时补一个 `cmd/sofarpc-mcp` 包内单测，镜像 `TestReplay_RewritesDotNestedTypeNamesBeforeExecution`。

## Post-fix E2E rerun

选择集：**S5**（本次唯一 failed）+ 其 DAG 下游（无）。S5 依赖 N1 产出的 `PLAN_DOT`，存档已保留在 `scripts/s5-plan-dotform.archive.json`，可直接复用，无需重跑 N1。

## Closure rule

S5 的 `errorCode` 由 `000` 变为 `003`，且存档文件未被篡改，且 `go test ./internal/... ./cmd/...` 全绿。

## Cleanup / data impact

无。该场景使用非法日期（`startDate: "2026-07-32"`），无论成功失败都在服务端事务开始前返回；本次 post-check 已确认 `SEASON_ID=1000002925130006` 的窗口与基线逐字一致。
