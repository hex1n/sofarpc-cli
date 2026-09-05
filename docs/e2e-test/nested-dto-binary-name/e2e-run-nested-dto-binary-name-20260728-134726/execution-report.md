# 执行报告（重跑）：ISSUE-001 修复后验证

## Execution Summary

**1 passed / 0 failed / 0 blocked / 0 skipped**（选择集：前轮唯一 failed 场景 S5 + 其 DAG 下游=无）

**结论**：ISSUE-001 已修复并在真实链路闭环。S5 的 `errorCode` 由 `000` 变为 `003`，用户存档未被就地篡改，展示的 plan 保留捕获时的点号拼写。

## Run Lineage & Emergent Scenarios

| 字段 | 值 |
|---|---|
| Upstream plan | [`../2026-07-28-nested-dto-binary-name-e2e-test-plan.md`](../2026-07-28-nested-dto-binary-name-e2e-test-plan.md) |
| **Upstream run** | [`../e2e-run-nested-dto-binary-name-20260728-133003/execution-report.md`](../e2e-run-nested-dto-binary-name-20260728-133003/execution-report.md)（6 passed / 1 failed / 1 skipped） |
| 修复对象 | [ISSUE-001](../e2e-run-nested-dto-binary-name-20260728-133003/issues/ISSUE-001-cli-plan-replay-missing-nested-type-rescue.md) → 本轮 **CLOSED** |
| 编排 run | `e2e-verify-nested-dto-binaryname-20260728`（N3 重跑） |

选择集依据：前轮 `failed` = {S5}；S5 无 DAG 下游（`Depends on: N1` 是其前驱，产出的 `PLAN_DOT` 存档已保留可直接复用，无需重跑 N1）。S1/S2/S3/S6/S0/S7 前轮已 passed 且本次改动只触及 `cmd/sofarpc-mcp/call.go` 的 `-plan` 分支，不在其路径上——但仍由全量 `go test` 覆盖回归。

### Emergent Scenarios

| 来源触发 | 新场景 | 状态 |
|---|---|---|
| 前轮 S5 失败 | **S9**：`call -plan` 重放**非嵌套** plan 的降级回归 | ✅ 已随修复落为单测 `TestRunCall_PlanFileWithoutContractStoreReplaysVerbatim`（store 不可解析时逐字节原样重放） |
| 前轮 D2 | **S8**：MCP `sofarpc_replay` 端到端验证 | 仍待用户重连 MCP（见 Failures 表） |

## Environment State Ledger

| 字段 | 值 |
|---|---|
| 环境类型 | test |
| 目标 | `bolt://10.74.194.42:12200`（同前轮） |
| 客户端部署指纹 | **临时构建二进制** `scratchpad/sofarpc-mcp-fix.exe`（含 ISSUE-001 修复，`version` 仍显示 `d35539c` 基线戳 + `+dirty`——修复尚未提交）。**未覆盖 `~/go/bin/sofarpc-mcp.exe`**：该文件被其他会话的 MCP 子进程占用，`go install` 报 `being used by another process`；改用临时二进制避免影响他人会话 |
| 服务端部署指纹（行为式） | `errorMsg = "自定义时间区间开始日期格式错误，正确格式为yyyyMMdd"`（与前轮一致，环境未变） |
| 创建的数据 | 无 |
| 清理策略 | preserve；本轮无数据可清 |
| 残留痕迹 | 无。post-check 与前轮基线逐字一致 |
| 不可清理项 | `../e2e-run-...-133003/scripts/s5-plan-dotform.archive.json`（本轮复现输入，作为回归素材保留） |

## Scenario Results

| 场景 | 状态 | Expected | Actual | 诊断 | Issue | 证据 |
|---|---|---|---|---|---|---|
| **S5** | ✅ **passed**（前轮 failed） | 点号名存档重放被救援 → `errorCode=003`；存档不被篡改；展示保留原拼写 | `errorCode=003`；存档点号名计数仍为 1；展示 `@type` 为点号名 | — | [ISSUE-001](../e2e-run-nested-dto-binary-name-20260728-133003/issues/ISSUE-001-cli-plan-replay-missing-nested-type-rescue.md) → CLOSED | [§S5](#s5) |

## Evidence & Failure Scenes

<a id="s5"></a>
### S5 —— replay 旧存档（修复后）

**Probe**：`SOFARPC_ALLOW_INVOKE=true <新二进制> call -project "C:/Users/hexin/Desktop/project/fundsalesmrksupport" -plan ../e2e-run-...-133003/scripts/s5-plan-dotform.archive.json`

**Expected**：`errorCode == "003"`；存档文件仍为点号名；`callOutput.Plan` 保留捕获拼写

**Actual**：

```
errorCode : 003 | errorMsg: 自定义时间区间开始日期格式错误，正确格式为yyyyMMdd
transport : direct-bolt | responseStatus: 0
reported plan @type (应保留点号名): com.thfund.sales.fundsalesmrksupport.facade.model.request.challenge.bg.UpsertSeasonTimeWindowRequest.CustomTimeWindow
```

存档文件复检：`grep -c 'UpsertSeasonTimeWindowRequest\.CustomTimeWindow'` → `1`（未被就地篡改）

**前后对比**：前轮 `errorCode 000 系统异常` → 本轮 `errorCode 003 业务校验`。同一存档、同一服务端，唯一变量是 `cmd/sofarpc-mcp/call.go` 的救援接入。

**Raw**：`evidence/s5-replay.out.json`
**Re-query**：同 Probe 命令
**Scene**：零写入（存档携带非法日期，事务前返回）。

### Post-check —— 零副作用确认

```
前轮基线: {"strategy":"FIXED","codes":["ALL:null-null","c7a4b9d2:20260720-20260803"]}
本轮复查: {"strategy":"FIXED","codes":["ALL:null-null","c7a4b9d2:20260720-20260803"]}
IDENTICAL: true
```

**Raw**：`evidence/postcheck-query.out.json`

## Failures / Defects / Plan Gaps

| ID | 类型 | Disposition | 内容 | 去向 |
|---|---|---|---|---|
| D1 / ISSUE-001 | product | ✅ **CLOSED** | CLI `call -plan` 未做嵌套类型名救援 | 已修：`call.go` 在执行分支对 plan 副本接入 `contract.RewriteWireTypeNames` + `contract.WireParamTypes`（仅 `-plan` 路径，store 为 nil 时降级）。闭环判据全部满足：`003` ✓ / 存档未篡改 ✓ / `go test ./cmd/... ./internal/...` 全绿 ✓ |
| D2 | tooling | **BLOCKED-BY-TOOLING** | MCP 通道仍为旧二进制 | 需用户重连 sofarpc MCP。**注意**：重连后 MCP 会加载 `~/go/bin/sofarpc-mcp.exe`，该文件当前仍是**修复前**版本（被占用无法覆盖）——需先关闭占用进程或重启后 `go install`，再重连，S8 才有意义 |
| G4 | plan gap | **CONDITIONAL** | S4（真实落库）未执行 | 前置条件不变：需用户授权 `destructive-delete` |
| G1 / G3 | — | OUT-OF-SCOPE / ACCEPTED | 同前轮，未变化 | — |

## 修复内容（供审查）

`cmd/sofarpc-mcp/call.go` 执行分支新增（仅 `-plan` 路径生效）：

```go
execPlan := plan
if strings.TrimSpace(in.planFile) != "" {
    if store := callContractStore(projectRoot); store != nil {
        execPlan.Args = contract.RewriteWireTypeNames(execPlan.Args, store)
        execPlan.ParamTypes = contract.WireParamTypes(execPlan.ParamTypes, store)
    }
}
```

`policy.Validate` 与 `invoke.Execute` 改用 `execPlan`；输出的 `callOutput.Plan` 仍指向原始 `plan`。

新增单测 `cmd/sofarpc-mcp/call_wirename_test.go`（用捕获请求字节的 fake BOLT server，走完整 `runCall`）：

- `TestRunCall_PlanFileRescuesDotNestedTypeNames` —— 线路含 `$` 名、不含点号名、存档未篡改、展示保留原拼写
- `TestRunCall_PlanFileWithoutContractStoreReplaysVerbatim` —— 无 store 时逐字节原样重放（降级不失效）

**反向验证**：① 去掉两行救援 → 前者报 `dot-form nested type name still on the wire`（红）；② 把展示的 `Plan` 换成 `execPlan` → 前者报 `reported plan should keep the captured @type`（红）。两次均已恢复并复验绿。

## Re-run Instructions

```bash
BIN=<含修复的 sofarpc-mcp>
P="C:/Users/hexin/Desktop/project/fundsalesmrksupport"
ARCHIVE="../e2e-run-nested-dto-binary-name-20260728-133003/scripts/s5-plan-dotform.archive.json"
SOFARPC_ALLOW_INVOKE=true "$BIN" call -project "$P" -plan "$ARCHIVE"
# 期望 errorCode 003；存档文件仍为点号名
```

## Next Actions for Agent

无 OPEN 可执行项。剩余两项均需用户操作：① 关闭占用 `~/go/bin/sofarpc-mcp.exe` 的进程后 `go install` 并重连 MCP（解除 D2，补跑 S8）；② 授权 S4 后可验证真实落库（G4）。
