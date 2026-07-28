# 执行报告：Java 静态嵌套 DTO 二进制类名修复（E2E）

## Execution Summary

**6 passed / 1 failed / 0 blocked / 1 skipped**（选择集：Core Slice + S7；S4 未获授权跳过）

**核心结论**：修复在真实 Hessian2 链路上生效。三种 `@type` 拼写（不写 / `$` 二进制名 / 点号规范名）全部发出 `$` 名并穿透到服务端业务校验（`errorCode 003`），原 bug 的 `errorCode 000` 伪系统异常不再出现。

**唯一失败**：S5 暴露了 CLI `call -plan` 重放路径**未做**嵌套类型名救援（`errorCode 000`）——MCP replay 路径已修且有单测，CLI 是同源的另一半。该留白在诊断阶段已被静态识别并写入「已知留白」，本次执行为其提供运行时证据 → [ISSUE-001](issues/ISSUE-001-cli-plan-replay-missing-nested-type-rescue.md)。

**数据影响：零**。全部场景使用非法日期探针，在服务端事务开始前返回；post-check 与基线逐字一致。

## Run Lineage & Emergent Scenarios

| 字段 | 值 |
|---|---|
| Upstream plan | [`../2026-07-28-nested-dto-binary-name-e2e-test-plan.md`](../2026-07-28-nested-dto-binary-name-e2e-test-plan.md)（Delta Plan） |
| Upstream run | 无（首次执行）。相关前序：`code-diagnose-task-20260728`（修复本体，7 轮换执行器审查 pass）；`docs/e2e-test/season-default-period-strategy/e2e-run-...-20260727-154933`（提供 `SEASON_ID` 与 trusted 存档素材） |
| 编排 run | `e2e-verify-nested-dto-binaryname-20260728`（N3 节点） |
| Downstream | 待 ISSUE-001 修复后重跑 S5 |

### Emergent Scenarios

| 来源触发 | 新场景 | 风险族 | 待更新的计划位置 | 状态 |
|---|---|---|---|---|
| S5 失败暴露 CLI/MCP 重放路径不对等 | **S8**：MCP `sofarpc_replay` 重放点号名存档（对照 S5，验证 MCP 侧救援真实生效） | 契约兼容（存档重放） | Scenario Inventory + Coverage Matrix F3 行 | 待补（依赖 G2 解除：用户重连 MCP） |
| 执行中发现 `-plan` 与 `-args-file` 走不同代码路径 | **S9**：`call -plan` 重放**非嵌套** plan 的回归（确认修 ISSUE-001 时不破坏普通重放） | 回归 | 随 ISSUE-001 修复一并补 | 待补 |

## Environment State Ledger

| 字段 | 值 |
|---|---|
| 环境类型 | **test**（非 preprod/prod，允许真实调用） |
| 目标 | `bolt://10.74.194.42:12200`，`transport=direct-bolt`，`serialization=hessian2`，`timeoutMs=10000` |
| 客户端部署指纹 | `sofarpc-mcp v0.0.0-20260728050932-**d35539c**44715+dirty`（构建于 13:09:55；`+dirty` 来自工作区一个无关未跟踪文档，非代码改动） |
| 服务端部署指纹（行为式） | `errorMsg = "自定义时间区间开始日期格式错误，正确格式为yyyyMMdd"`；本地 HEAD 源码为 `"自定义赛程日期格式不正确"` → **测试环境部署版本 ≠ 本地 HEAD**（记为 G3，已 accepted：判据用 `errorCode` 不用 `errorMsg`） |
| 数据源 | `mrk_competition_schedule`（经 facade 间接访问；无 DB 直连凭据，用 `querySeasonTimeWindow` 作等价探针） |
| 隔离命名空间 | `SEASON_ID=1000002925130006`；自定义窗口 code 前缀 `E2EBIN_` |
| 创建的数据 | **无**。全部场景在服务端事务前返回 |
| 清理策略 | `preserve`（计划默认）——本次无数据可清 |
| 残留痕迹 | 无。post-check 确认窗口集合与基线逐字一致，无 `E2EBIN_` 泄漏 |
| 不可清理项 | `scripts/s5-plan-dotform.archive.json`（ISSUE-001 的复现素材与重跑输入，**修复验证前不得删除**） |
| 工具权限 | CLI + `SOFARPC_ALLOW_INVOKE=true` + 项目 allowedServices 含 SeasonBgFacade（已确认）；MCP `sofarpc_invoke` 通道**旧二进制**（G2） |

## Run Metadata

- Run 目录：`e2e-run-nested-dto-binary-name-20260728-133003`
- 执行时间：2026-07-28 13:30–13:34
- 被验证改动：`d35539c`
- 数据策略：preserve（无创建数据）

## Environment & Capability Map

| 能力 | 状态 | 说明 |
|---|---|---|
| CLI `sofarpc-mcp call`（新二进制） | ✅ required，可用 | 主执行通道，身份已核 |
| CLI `-dry-run`（无网络） | ✅ 可用 | S6 客户端侧断言 |
| CLI `-plan`（重放） | ✅ 可用（但**有缺陷**） | S5 通道，见 ISSUE-001 |
| 目标可达 + invoke 策略 | ✅ required，可用 | `sofarpc_doctor` target=ok / invoke-policy=ok |
| `querySeasonTimeWindow` 只读探针 | ✅ 可用 | 替代无凭据的 DB 直查 |
| MCP `sofarpc_invoke` | ⛔ **BLOCKED-BY-TOOLING** | 会话子进程启动于 11:02:50，二进制构建于 13:09:55 → 旧代码。需用户重连 MCP |
| DB 直连 | ⛔ 无凭据 | optional，不阻塞（facade 探针等价） |

### Trigger Channel Gates

`SOFARPC_ALLOW_INVOKE=true`（每次调用显式注入）+ 项目 `.sofarpc/config.local.json` 的 `allowedServices` 含 `SeasonBgFacade` + `directUrl` 直连。三者齐备，未使用 target override。

## DAG Schedule

| 顺序 | 节点 | 调度理由 |
|---|---|---|
| 1 | N6（S6） | 无依赖、无网络，最先跑以确认客户端侧行为 |
| 2 | N0（S0） | 采基线；证实 `SEASON_ID` 存在（假设 A1） |
| 3 | N1（S1） | 主判据；产出 `PLAN_DOT` 供 N5 消费 |
| 4 | N2/N3（S2/S3） | 依赖 N1 的判据成立；串行执行（同 `SEASON_ID` 隔离键，虽为只读但保守串行） |
| 5 | N5（S5） | 消费 N1 产出的 `PLAN_DOT` |
| 6 | N7（S7） | 证据复用 N0 的原始输出切片 |
| — | N4（S4） | **skipped**：`destructive-delete` 且门 G-AUTH-S4 未取得 |
| 尾 | post-check | 与 N0 基线比对，证明零副作用 |

## Scenario Results

| 场景 | 状态 | Expected | Actual | 诊断 | Issue | 证据 |
|---|---|---|---|---|---|---|
| S6 | ✅ passed | plan 嵌套 `@type` 为 `$` 名；`paramTypes[0]` 不含 `$` | `...Request$CustomTimeWindow`；`paramTypes[0]=...UpsertSeasonTimeWindowRequest` | — | — | [§S6](#s6) |
| S0 | ✅ passed | `success=true`，`SEASON_ID` 存在 | `success=true`，2 个窗口（ALL、c7a4b9d2） | — | — | [§S0](#s0) |
| **S1** | ✅ passed | `errorCode=003`（非 `000`） | `errorCode=003`，发出 `$` 名 | — | — | [§S1](#s1) |
| **S2** | ✅ passed | 显式 `$` 名不被拒；`errorCode=003` | 未报 not assignable；`errorCode=003` | — | — | [§S2](#s2) |
| S3 | ✅ passed | 显式点号名被接受并转 `$` 名；`errorCode=003` | 发出 `$` 名；`errorCode=003` | — | — | [§S3](#s3) |
| **S5** | ❌ **failed** | 点号名存档重放被救援 → `errorCode=003` | `errorCode=000 系统异常`（救援未生效） | `product` | [ISSUE-001](issues/ISSUE-001-cli-plan-replay-missing-nested-type-rescue.md) | [§S5](#s5) |
| S7 | ✅ passed | 非嵌套调用路径类型名不含 `$`，调用成功 | `paramTypes=["...SeasonIdRequest"]`，`success=true` | — | — | [§S7](#s7) |
| S4 | ⏭️ skipped | — | 门 G-AUTH-S4 未取得（`destructive-delete` 需用户授权） | — | — | — |

## Evidence & Failure Scenes

<a id="s6"></a>
### S6 —— 客户端两侧表示

**Probe**：`sofarpc-mcp call ... -args-file scripts/s1-default-no-type.args.json -dry-run`（无网络）
**Expected**：嵌套 `@type` 为 `$` 名；`paramTypes[0]` 逐字节不变（顶层类非嵌套）
**Actual**：

```
nested @type : com.thfund.sales.fundsalesmrksupport.facade.model.request.challenge.bg.UpsertSeasonTimeWindowRequest$CustomTimeWindow
paramTypes[0]: com.thfund.sales.fundsalesmrksupport.facade.model.request.challenge.bg.UpsertSeasonTimeWindowRequest
```

**describe 侧（展示规范名）**：`skeleton.go` 不在 `d35539c` 中（`git show --stat d35539c | grep -c skeleton.go` → `0`），故新旧二进制 describe 输出必然一致；实测 MCP describe 返回嵌套 `@type` 为 `...Request.CustomTimeWindow`（点号名）。两侧承诺同时成立。
**Raw**：`evidence/s6-dryrun-plan.out.json`
**Re-query**：`node -e "const j=require('./evidence/s6-dryrun-plan.out.json');console.log(j.plan.args[0].customTimeWindows[0]['@type'])"`
**Scene**：无网络调用，零副作用。

<a id="s0"></a>
### S0 —— 基线（同时为 S7 提供证据）

**Probe**：`SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call ... -method querySeasonTimeWindow -args-file scripts/s0-query-baseline.args.json`
**Expected**：`success=true`，`SEASON_ID` 存在
**Actual**：

```
success   : true | errorCode: null
paramTypes: ["com.thfund.sales.fundsalesmrksupport.facade.model.request.challenge.SeasonIdRequest"]
data      : defaultPeriodStrategy=FIXED
            seasonTimeWindows=[{code:c7a4b9d2, name:E2E_READD, startDate:20260720, endDate:20260803, source:CUSTOM, isDefault:false, status:VALID}, {code:ALL, ...}]
```

**Created entities**：无（只读）
**Raw**：`evidence/s0-baseline.out.json`
**Re-query**：`SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project "C:/Users/hexin/Desktop/project/fundsalesmrksupport" -service com.thfund.sales.fundsalesmrksupport.facade.background.SeasonBgFacade -method querySeasonTimeWindow -args-file scripts/s0-query-baseline.args.json`
**Scene**：`BASELINE_WINDOWS = {strategy: FIXED, codes: [ALL:null-null, c7a4b9d2:20260720-20260803]}`

<a id="s1"></a>
### S1 —— 默认路径（主判据，P0）

**Probe**：`SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call ... -args-file scripts/s1-default-no-type.args.json`（args 中**不含任何 `@type`**）
**Expected**：`errorCode == "003"`——服务端 `paramCheck` 能迭代 `List<CustomTimeWindow>` 并读到 `startDate`，证明嵌套 DTO 被正确实例化；若退化为 Map 则 ClassCastException → `000`
**Actual**：

```
ok         : true
sent @type : ...UpsertSeasonTimeWindowRequest$CustomTimeWindow
errorCode  : 003 | errorMsg: 自定义时间区间开始日期格式错误，正确格式为yyyyMMdd
transport  : direct-bolt | responseStatus: 0
```

**Raw**：`evidence/s1-default-no-type.out.json`
**Re-query**：同 Probe 命令（幂等，非法日期恒在事务前返回）
**Scene**：零写入。

<a id="s2"></a>
### S2 —— 显式 `$` 二进制名（P0）

**Probe**：`... -args-file scripts/s2-explicit-binary.args.json`（嵌套元素显式 `"@type": "...Request$CustomTimeWindow"`）
**Expected**：不再出现 `input.args-invalid: ... is not assignable`（bug report 第 3 步）；`errorCode == "003"`
**Actual**：

```
ok         : true          ← 计划阶段未被拒绝
sent @type : ...UpsertSeasonTimeWindowRequest$CustomTimeWindow
errorCode  : 003 | errorMsg: 自定义时间区间开始日期格式错误，正确格式为yyyyMMdd
```

**Raw**：`evidence/s2-explicit-binary.out.json`
**Re-query**：同 Probe 命令
**Scene**：零写入。

<a id="s3"></a>
### S3 —— 显式点号 canonical 名（双向等价）

**Probe**：`... -args-file scripts/s3-explicit-canonical.args.json`（显式 `"@type": "...Request.CustomTimeWindow"`）
**Expected**：被接受且**发送前转为 `$` 名**（等价性双向成立，而非把拒绝方向调换）
**Actual**：

```
ok         : true
sent @type : ...UpsertSeasonTimeWindowRequest$CustomTimeWindow   ← 输入点号名，发出 $ 名
errorCode  : 003 | errorMsg: 自定义时间区间开始日期格式错误，正确格式为yyyyMMdd
```

**Raw**：`evidence/s3-explicit-canonical.out.json`
**Re-query**：同 Probe 命令
**Scene**：零写入。

<a id="s5"></a>
### S5 —— replay 旧存档（P0，**FAILED**）

**Probe**：
1. 断言存档形态：`grep -c 'Request\.CustomTimeWindow' scripts/s5-plan-dotform.archive.json` → `1`；`grep -c 'Request\$CustomTimeWindow'` → `0`（确为修复前形态的点号名存档）
2. `SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project ... -plan scripts/s5-plan-dotform.archive.json`

**Expected**：`errorCode == "003"`（执行前救援生效）
**Actual**：

```
ok        : true
errorCode : 000 | errorMsg: 系统异常,请稍后再试!
transport : direct-bolt | responseStatus: 0
```

存档文件执行后复检仍为点号名（`grep -c` → `1`）——未被就地篡改，符合预期语义。
`responseStatus: 0` 排除网络故障，失败在服务端反序列化层。

**Diagnosis**：`product`。`cmd/sofarpc-mcp/call.go:93` 直接 `invoke.Execute(ctx, plan, "call")`，缺少 `internal/mcp/replay.go:76-77` 的 `RewriteWireTypeNames` / `WireParamTypes` 救援。
**Raw**：`evidence/s5-replay.out.json`
**Re-query**：同 Probe 步骤 2
**Scene（保留）**：`scripts/s5-plan-dotform.archive.json` 为复现输入，**修复验证前不得删除**。

<a id="s7"></a>
### S7 —— 非嵌套调用回归

**Probe**：复用 S0 的批量探针输出切片（`querySeasonTimeWindow` 是不含嵌套 DTO 的入参路径）
**Expected**：类型名不含 `$`，调用正常成功
**Actual**：`paramTypes: ["...challenge.SeasonIdRequest"]`（无 `$`），`success=true`
**Raw**：`evidence/s0-baseline.out.json`（同一原始输出，不重复留存）
**Scene**：零副作用。

### Post-check —— 零副作用确认

```
baseline : {"strategy":"FIXED","codes":["ALL:null-null","c7a4b9d2:20260720-20260803"]}
postcheck: {"strategy":"FIXED","codes":["ALL:null-null","c7a4b9d2:20260720-20260803"]}
IDENTICAL: true
E2EBIN_ leaked: false
```

**Raw**：`evidence/postcheck-query.out.json`

## Failures / Defects / Plan Gaps

| ID | 类型 | Disposition | 内容 | 影响场景 | 去向 |
|---|---|---|---|---|---|
| D1 | **product** | **OPEN** | CLI `call -plan` 重放路径未做嵌套类型名救援，`errorCode 000`。与 `mcp/replay.go` 同源，修复只覆盖了 MCP 一侧 | S5 | [ISSUE-001](issues/ISSUE-001-cli-plan-replay-missing-nested-type-rescue.md)；建议转 `dev-delivery`（根因明确，改法已在 issue 的 Fix constraints 写清） |
| D2 | tooling | **BLOCKED-BY-TOOLING** | MCP `sofarpc_invoke` / `sofarpc_replay` 通道跑的是 11:02:50 启动的旧二进制，无法验证 MCP 侧行为 | S8（emergent，未执行） | 缺失能力：**需用户在 Claude Code 中重连 sofarpc MCP**。重连后补跑 S8 |
| G1 | plan gap | **OUT-OF-SCOPE** | 多级嵌套 / `Map<K,Outer.Inner>` / 嵌套 DTO 数组无真实 facade 方法承载 | — | 已由单测覆盖（`TestNestedDTO_WireTypeNameIsBinaryName` 含 Map/数组/多级、`TestNestedClassInsideLiteralDollarOuter`），端到端无载体 |
| G3 | environment | **ACCEPTED** | 测试环境部署版本 ≠ 本地 HEAD（`errorMsg` 文案不同） | 全部 | 判据用 `errorCode` 不用 `errorMsg`，两版本 `paramCheck` 都必须读 `startDate`，判据成立 |
| G4 | plan gap | **CONDITIONAL** | S4（真实落库）未执行 | S4 | 前置条件：用户显式授权 `destructive-delete`（服务端按 seasonId 全量 delete+insert）。基线 `BASELINE_WINDOWS` 已采集，可随时执行与回滚 |

## Data Created & Cleanup

**本次运行未创建任何业务数据。** 全部场景使用非法日期 `startDate: "2026-07-32"`（`isValidDate` 先判 `length() != 8`，必然拒绝），请求在服务端 `paramCheck` 阶段返回，从未进入 `businessTransactionService.transactionProcess`。

post-check 已证明 `SEASON_ID=1000002925130006` 的窗口集合与执行前逐字一致，无 `E2EBIN_` 前缀数据泄漏。

**无需清理脚本**（无创建数据）。保留项：`scripts/s5-plan-dotform.archive.json`（ISSUE-001 复现输入）。

## Re-run Instructions

```bash
cd C:/Users/hexin/Desktop/sofarpc-cli/docs/e2e-test/nested-dto-binary-name/e2e-run-nested-dto-binary-name-20260728-133003
P="C:/Users/hexin/Desktop/project/fundsalesmrksupport"
SVC="com.thfund.sales.fundsalesmrksupport.facade.background.SeasonBgFacade"

# 前置：确认二进制身份（跑到旧二进制会复现原 bug，误判为修复失效）
sofarpc-mcp version    # 必须含 d35539c 或更新

# 全量重跑 Core Slice
for S in s1-default-no-type s2-explicit-binary s3-explicit-canonical; do
  SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project "$P" -service "$SVC" \
    -method upsertSeasonTimeWindow -args-file "scripts/$S.args.json"
done

# 仅重跑失败项（ISSUE-001 修复后）
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project "$P" -plan scripts/s5-plan-dotform.archive.json
```

## Next Actions for Agent

1. **修 ISSUE-001**（`product`，OPEN）：给 `cmd/sofarpc-mcp/call.go` 的 `-plan` 路径接上 `contract.RewriteWireTypeNames` + `contract.WireParamTypes`，约束见 issue 的 Fix constraints；补 `cmd` 包单测镜像 `TestReplay_RewritesDotNestedTypeNamesBeforeExecution`。修完按 issue 的 Post-fix E2E rerun 重跑 S5（选择集：S5，无下游）。
