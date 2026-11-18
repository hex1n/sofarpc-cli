---
title: SOFARPC 完整性能分析能力最佳改进计划
date: 2026-08-31
status: superseded
superseded_by: 2026-09-01-sofarpc-java-reference-runner-spike.md
---

# SOFARPC 完整性能分析能力最佳改进计划

> **评审结论（2026-09-01）**：本计划不是当前最佳实施入口。现有 pure-Go
> transport 每次调用新建连接，而兼容矩阵中的官方 Java Consumer 默认复用
> 长连接；在测量主体校准前建设后台 perf 平台会规模化错误结论。当前执行入口
> 已收敛为 `2026-09-01-sofarpc-java-reference-runner-spike.md`，详细证据见
> `../research/2026-08-31-sofarpc-performance-measurement-contract.md`。本文保留为
> 后续平台化候选及需求覆盖清单。

## TL;DR

当前 `sofarpc-cli` 已经能够可靠完成单次 generic invoke、dry-run、replay、
目标探测和结构化诊断，但这些能力不足以独立完成一次可审计的业务性能
分析。问题不是 BOLT/Hessian2 调用能力缺失，而是缺少批量调度、调用边界
计时、异步终态关联、服务端分段观测和业务状态证明。

建议新增一个与 `sofarpc_invoke` 隔离的性能执行域：

```text
sofarpc_perf_start
        |
        v
  scenario preflight
        |
        v
  isolated performance worker
        |
        +--> direct SOFARPC call timing
        +--> timeout/retry policy
        +--> terminal-state observer
        +--> trace correlation
        +--> optional read-only state probe
        |
        v
  raw samples + summary report + evidence manifest
```

普通 `sofarpc_invoke` 保持单次调用语义；性能能力复用同一份 contract、
target、policy 和 transport 核心，但由专门的 `internal/core/perf` 编排。
不要给普通 `invoke` 直接增加一个无边界的 `loop` 参数。

本计划的最小闭环是：在测试目标上以 `timeout=3000ms`、`retries=0`，对
多个组合执行预热和正式采样，记录调用方真实耗时，并在受理超时后用同一
`requestNo` 查询任务终态。完整业务归因还需要服务端 trace 和可选的只读
状态探针配合。

## 1. 根问题与决策

### 1.1 根问题

使用 MCP 单次工具调用可以回答“这次 RPC 返回了什么”，但不能稳定回答：

- 200 笔正式样本中有多少笔在 3 秒调用方边界内返回；
- 一个超时是连接超时、客户端等待超时、服务端 dry-run 慢，还是异步任务
  后续失败；
- 多个组合的耗时是否具有可比性；
- RPC 超时后是否已经在服务端产生任务或业务写入；
- 结果是否包含 MCP 外层排队、模型调度和工具序列化时间。

### 1.2 价值门禁

```yaml
decision: BUILD
target_outcome: 能对测试环境的 SOFARPC 业务调用给出可复核的客户端性能、终态和失败归因证据
baseline_and_frequency: 当前可单次调用或手工重复，无法形成稳定的多组合逐样本性能分布；频率由具体性能验证任务决定
expected_benefit: 将一次性能结论从“人工观察”提升为可重放、可分组、可审计的原始样本和统计报告
delivery_and_maintenance_cost: 新增性能执行域、worker、结果协议、测试门禁和文档；持续维护场景 schema、版本兼容和安全上限
status_quo_or_existing_mechanism: 继续使用现有 `sofarpc_invoke` 做少量冒烟，或使用外部 Java/Go Runner 做正式采样
decision_flip_condition: 如果 MCP 宿主已经提供可靠的批量执行、内部单调计时、后台任务生命周期和结果持久化，则可只补协议适配而不新增独立 worker
review_scope: implementation-authorization
review_budget: bounded first release
```

选择 `BUILD` 的原因是：已有独立 Runner 证明真实 SOFARPC 调用和多组合
采样是可行的，当前缺口集中在能力收敛和证据标准，而不是协议研发。

### 1.3 真实约束、约定与假设

| 项目 | 分类 | 约束或验证方式 |
|---|---|---|
| 普通调用必须保持现有返回和错误码语义 | 真实约束 | 复用现有 `core/invoke` 与现有 Go 测试 |
| 3 秒是调用方边界，不等于服务端一定停止 | 真实约束 | timeout 后同一业务幂等键查询终态 |
| 交易维护是有副作用的业务调用 | 真实约束 | 测试目标、服务白名单、显式 real-invoke、owner 前缀和样本上限 |
| direct + BOLT + Hessian2 是当前 pure-Go mainline | 真实约束 | 不在本计划中引入 registry 或新的 wire mode |
| 性能测试需要多个组合和批量采样 | 业务要求 | 场景输入显式提供组合矩阵和样本数 |
| provider 能接受并记录 invocation baggage | 未验证假设 | 先用本地 fake provider 验证编码，再用测试服务确认 `invoke.baggage.enable` |
| 能为具体业务配置只读状态探针 | 未验证假设 | 先实现探针接口和 fake diff，再确认测试环境 DB 访问与表清单 |
| 所有业务都存在可查询的终态幂等键 | 未验证假设 | 场景必须声明 observer；没有 observer 的写调用只能做客户端测试，不能宣称完整业务分析 |

## 2. 当前能力与缺口

仓库现有能力已经提供了以下可复用基础：

- `sofarpc_open`、`sofarpc_target`、`sofarpc_describe` 提供项目、目标和
  contract 上下文；
- `sofarpc_invoke` 通过 `internal/core/invoke` 走 plan + execute；
- direct transport 已有 BOLT/Hessian2、`timeoutMs`、`connectTimeoutMs`、
  `SOFARPC_ALLOW_INVOKE`、服务白名单和目标主机门禁；
- `sofarpc_replay` 可复用已捕获的调用计划；
- invocation properties 已能编码为
  `SofaRequest.requestProps["rpc_req_baggage"]`；
- 现有 fake BOLT e2e 和 golden fixture 可以承接本地验证。

缺口不是“再发一次 RPC”，而是以下五个证据层：

1. **调用层**：调用边界内的单调时钟、连接/调用超时分类和禁止自动重试。
2. **调度层**：预热、正式样本、组合矩阵、并发度、节流和取消。
3. **状态层**：写调用超时后的同键查询、异步终态和幂等关联。
4. **归因层**：服务端 dry-run、replay、DB、outbox 等阶段耗时。
5. **业务层**：只读快照、逐表规范化差异和失败场景零写入证明。

## 3. 推荐架构

### 3.1 模块边界

新增：

```text
internal/core/perf/
  scenario.go       场景及矩阵模型
  scheduler.go      预热、正式样本、并发和节奏
  runner.go         单次调用编排
  observer.go       终态查询和状态分类
  measurement.go    单调时钟和耗时模型
  statistics.go     分位数、计数和小样本标记
  artifact.go       原始样本和 manifest
  guard.go          性能运行专用安全限制

internal/mcp/
  perf.go           MCP 输入解析、任务启动和结果返回
  perf_schema.go    工具 schema
  perf_resources.go run/status/result 资源读取
```

必要时增加：

```text
cmd/sofarpc-mcp/perf.go       CLI 的 perf run/status/result
internal/core/perf/worker.go  后台 worker 或 worker 子进程协议
internal/core/probe/          只读状态探针接口及通用结果模型
```

`internal/core/perf` 不直接知道基金交易维护的 DTO 字段；它只知道：

- 如何复用已经构造好的 invoke plan；
- 如何通过显式字段替换生成下一笔样本；
- 哪个字段是请求相关性键；
- 是否存在一个只读终态 observer；
- 如何收集和归档观测结果。

具体业务场景应作为配置或受控 adapter 提供，不能把某个基金项目的表名和
业务字段硬编码进 generic invoke 核心。

### 3.2 后台任务模型

性能批次不应占住一次 MCP 请求直到 200 笔全部完成。推荐 `start` 返回
`runId`，worker 在后台运行，调用方通过 `status` 和 `result` 获取进度。

每个 run 至少有以下状态：

```text
PLANNED -> RUNNING -> COMPLETED
                  -> FAILED
                  -> CANCELLED
                  -> ABANDONED
```

`ABANDONED` 表示 MCP 进程或 worker 在没有完成最终收敛前退出；它不能被
当作业务成功。若 run 可能执行写调用，进程重启后不得自动从上次位置重发，
应由调用方按原始样本状态人工决定是否继续查询。

首版可以使用本地临时目录保存 manifest、NDJSON 原始样本和 summary，配合
进程内 TTL 状态表；不把数据库或远程持久化作为 generic CLI 的强依赖。

### 3.3 普通 invoke 与性能 invoke 的关系

```text
sofarpc_invoke
  -> BuildPlan
  -> Execute
  -> one result

sofarpc_perf_start
  -> Build/validate one canonical plan
  -> expand scenario matrix
  -> PerfScheduler
  -> Execute one plan per sample
  -> Observe terminal state
  -> write raw evidence
  -> calculate summary
```

两条路径共享 target resolution、contract normalization、execution policy 和
wire transport，但不共享“重试样本”“样本计数”“终态等待”等性能编排状态。

## 4. 性能运行协议

### 4.1 `sofarpc_perf_start` 输入

建议首版支持以下核心字段：

```json
{
  "project": "/abs/path/to/java-project",
  "targetProfile": "test",
  "service": "com.example.FundDiagnosisTradeMaintenanceFacade",
  "method": "modifyTrade",
  "types": ["com.example.FundTradeModifyRequest"],
  "baseArgs": [{"...": "..."}],
  "combinations": [
    {"name": "M01", "overrides": {"mpCode": 484975891244523520}},
    {"name": "M02", "overrides": {"mpCode": 484975909061926912}}
  ],
  "correlation": {
    "requestNoField": "requestNo",
    "prefix": "PERF-20260831-"
  },
  "sampling": {
    "warmups": 2,
    "samples": 20,
    "concurrency": 1,
    "interSampleDelayMs": 0
  },
  "transport": {
    "timeoutMs": 3000,
    "connectTimeoutMs": 3000,
    "retries": 0
  },
  "terminalObserver": {
    "service": "com.example.FundDiagnosisTradeMaintenanceFacade",
    "method": "queryTradeMaintenanceTask",
    "types": ["com.example.FundTradeMaintenanceTaskQueryRequest"],
    "argsTemplate": [{"mpCode": "${mpCode}", "requestNo": "${requestNo}"}],
    "pollIntervalMs": 200,
    "awaitTerminalMs": 20000,
    "terminalStatuses": ["COMPLETED", "FAILED"]
  },
  "trace": {
    "runIdProperty": "perf.runId",
    "sampleIdProperty": "perf.sampleId"
  },
  "stateProbeProfile": "fund-maintenance-readonly"
}
```

字段命名需要在实现时与现有 schema 统一；上例只表达职责，不要求照抄
具体 DTO。`baseArgs`、组合 override 和 observer args 必须在 contract
解析后校验，不能用任意表达式绕过类型检查。

### 4.2 `start` 返回

`start` 不返回 200 笔完整结果，只返回：

```json
{
  "ok": true,
  "runId": "perf_20260831_...",
  "status": "RUNNING",
  "plannedSamples": 220,
  "formalSamples": 200,
  "artifactUri": "sofarpc://perf-run/perf_...",
  "policy": {
    "target": "test",
    "timeoutMs": 3000,
    "retries": 0,
    "concurrency": 1
  }
}
```

预热样本必须明确标记，不得混入正式分位统计。`artifactUri` 只暴露
脱敏后的 manifest 和结果资源，不暴露环境变量引用解析出的敏感值。

### 4.3 单样本记录

原始样本至少包含：

```json
{
  "runId": "perf_...",
  "sampleId": "M01-FORMAL-0001",
  "sampleKind": "FORMAL",
  "combination": "M01",
  "requestNo": "PERF-...",
  "startedAtEpochMs": 0,
  "clientElapsedMs": 0,
  "transportOutcome": "RETURNED|TIMEOUT|EXCEPTION",
  "acceptOutcome": "PENDING|REJECTED|UNKNOWN",
  "taskId": "...",
  "terminalOutcome": "COMPLETED|FAILED|UNKNOWN|NOT_APPLICABLE",
  "notifyOutcome": "SUCCESS|FAILED|UNKNOWN|NOT_APPLICABLE",
  "queryElapsedMs": 0,
  "terminalElapsedMs": 0,
  "errorCode": "",
  "errorClass": "",
  "serverTraceId": "",
  "stateDiff": {
    "status": "PASS|FAIL|NOT_COLLECTED",
    "tables": []
  }
}
```

请求参数和响应体要遵守现有脱敏规则；对于敏感 invocation property，只保留
property key 和 reference，不把解析后的值写进样本或日志。

## 5. 计时、超时和重试语义

### 5.1 调用方计时

计时必须发生在 worker 的 SOFARPC client call 边界：

```text
monotonicStart
  -> proxy/boltclient call
monotonicEnd
```

使用 Go 的单调时钟能力（例如 `time.Since`）计算耗时；`startedAtEpochMs`
只用于跨系统关联，不能用于耗时相减。MCP 请求到达时间、工具排队时间、
模型等待时间和结果渲染时间不能进入 `clientElapsedMs`。

### 5.2 三秒不是唯一结论

一个正式样本应被分类为：

- `RETURNED_WITHIN_DEADLINE`：客户端在 3 秒内收到 RPC 返回；
- `RETURNED_AFTER_DEADLINE`：只有在 transport 没有强制 deadline 时才可能出现，
  必须单独标记；
- `TIMEOUT_TASK_COMPLETED`：受理调用超时，但同一 requestNo 最终查询到完成；
- `TIMEOUT_TASK_FAILED`：受理调用超时且任务最终失败；
- `TIMEOUT_TASK_UNKNOWN`：在查询窗口内不能确认终态；
- `BUSINESS_REJECTED`：RPC 返回业务错误码；
- `TRANSPORT_EXCEPTION`：连接、序列化或其他 transport 异常。

超时样本的真实服务端耗时通常未知，不能伪造为精确的 3000ms。统计时同时
报告：

1. `returnedOnly` 分位数：只对真实返回的调用计算；
2. `deadlineViolationRate`：超时/超过 SLA 的比例；
3. `censoredAtDeadline` 分布：将超时作为“至少达到 deadline”的删失样本单独展示。

这样不会因把超时样本直接丢掉而虚假降低 p95。

### 5.3 重试规则

性能 run 默认强制 `retries=0`，并在结果 manifest 中记录最终 transport 配置。
任何“超时后自动重试写 RPC”的行为都必须被拒绝，而不是作为普通优化项。

写调用超时后的处理顺序固定为：

```text
accept call
  ├─ returned -> record response -> query terminal state
  └─ timeout/exception -> do not resubmit -> query same requestNo
```

如果场景没有幂等相关性键或 observer，性能执行器只能返回“客户端 transport
结果”，不能将其标为完整业务分析。

## 6. 多组合调度模型

首版采用组合内、组合间均串行：

```text
M01: W1 -> W2 -> F1 ... F20 -> terminal checks
M02: W1 -> W2 -> F1 ... F20 -> terminal checks
...
```

原因是交易维护写调用存在异步任务和业务状态副作用；并发压测应通过另一个
明确的场景参数开启，而不是让性能工具默认制造并发。

调度器必须记录：

- planned sample count、started count、finished count；
- 每个组合的 warmup/formal 计数；
- 实际 sample sequence；
- 前一笔是否已完成终态；
- 取消位置和未发送样本数。

支持并发后仍需维护两种模式：

- `serial`：用于验证 3 秒调用方边界和业务链路；
- `controlled`：显式配置并发度、速率和最大在途数，用于容量测试。

不在首版引入无限循环、无上限并发或自动加压。

## 7. 终态观测与业务关联

### 7.1 Observer 接口

性能核心需要一个受控 observer 抽象：

```text
Observe(ctx, sampleContext) -> TerminalObservation
```

observer 只可执行已声明的只读查询。它必须使用当前样本的
`runId + combination + requestNo`，不能查询“最近一条任务”或使用全局最后
一次响应。

observer 结果要区分：

- 查询 RPC 成功但业务状态仍未终态；
- 查询 RPC 失败；
- 任务明确完成；
- 任务明确失败；
- 终态窗口耗尽。

### 7.2 幂等样本

同一 `requestNo` 的重复调用只能用于专门的幂等测试场景，不得进入首次性能
分布。幂等场景需要单独记录：

- 原始 taskId 与重发 taskId 是否相同；
- 任务总数是否增加；
- 业务表 diff 是否增加；
- 返回的 `idempotentHit` 或等价字段。

### 7.3 非写调用

对于纯查询 facade，可不配置 terminal observer，直接使用 transport 返回作为
终点；但 schema 仍要明确 `terminalOutcome=NOT_APPLICABLE`，不能把缺失 observer
伪装成异步业务成功。

## 8. 服务端分段耗时

仅有客户端耗时无法定位“3 秒花在何处”。推荐通过已有 invocation properties
传递非敏感关联信息：

```text
perf.runId
perf.sampleId
perf.combination
```

provider 侧应将这些值与实际 trace 关联，并在交易维护链路记录：

```text
acceptElapsedMs
dryRunElapsedMs
planElapsedMs
replayElapsedMs
dbElapsedMs
outboxElapsedMs
asyncCompletionElapsedMs
notifyElapsedMs
```

这些字段不是 generic CLI 可以凭空生成的事实。CLI 应做两件事：

1. 发送关联上下文并验证目标是否回传/记录可用 trace 标识；
2. 在结果中区分 `serverTraceCollected` 与 `serverTraceUnavailable`。

如果 provider 未启用 `invoke.baggage.enable`，不能宣称服务端逐请求归因已
完成，只能交付客户端性能和终态结果。

## 9. 只读状态探针与 DB 差异

### 9.1 通用核心不绑定业务数据库

`sofarpc-cli` 是 generic invoke 工具，不应把基金交易维护的表名、SQL 或
数据库驱动硬编码进通用核心。建议提供受控的 `StateProbe` 接口：

```text
Snapshot(ctx, scope) -> NormalizedSnapshot
Diff(before, after) -> TableDiff
```

具体业务通过显式 `stateProbeProfile` 注册允许的只读查询、表白名单、主键排序
和需要排除的非业务时间字段。数据库凭证只从环境变量引用读取，不进入场景
文件、plan、artifact 或日志。

### 9.2 差异规则

快照和 diff 必须：

- 按表、主键和稳定列排序；
- 排除非业务时间戳、运行节点和 trace 字段；
- 显示新增、删除、更新和未能读取的表；
- 只读执行，禁止 generic CLI 通过 probe 执行写 SQL；
- 在无法获取基线或终态时返回 `NOT_COLLECTED`，而不是 PASS。

失败场景的验收 oracle 是所有受保护业务表零差异；不是“RPC 返回错误”或
“任务不存在”。

## 10. 安全与生命周期门禁

性能能力的写入风险高于普通单次查询，建议新增独立门禁：

```text
SOFARPC_ALLOW_PERF_RUN=true
```

并继续继承已有：

- `SOFARPC_ALLOW_INVOKE=true`；
- project `allowedServices`；
- `SOFARPC_ALLOWED_TARGET_HOSTS`；
- target profile 的显式目标配置。

每次 run 还应限制：

- 最大组合数；
- 最大正式样本数；
- 最大总请求数（含预热和 observer）；
- 最大并发度；
- 最大运行时间；
- 单次 artifact 大小；
- 是否允许写调用；
- 是否配置了 requestNo 生成规则和终态 observer。

安全拒绝必须发生在任何 wire IO 之前。`dryRun` 只生成场景和展开后的样本
计划，不建立真实 Consumer、不发送真实请求。

性能运行不能默认提供清理能力。测试产生的任务、日志和 outbox 必须由具体
业务 runbook 清理，generic CLI 只保留 owner 前缀和 artifact 引用。

## 11. 结果与统计报告

报告分为三层：

### 11.1 运行摘要

- target profile、direct URL 脱敏值、service/method、wire 配置；
- runId、开始/结束时间、版本和 git revision；
- planned/started/finished 样本数；
- warmup/formal 数量；
- timeout、transport exception、business reject、terminal failed、unknown 数量；
- `returnedOnly` p50/p95/p99/max；
- deadline violation rate；
- 每个组合的独立统计。

### 11.2 原始样本

原始样本不可只保留聚合值。每行必须能回到 requestNo、sampleId、taskId、
traceId 和状态查询结果。错误消息可截断并脱敏，但稳定 error code 必须保留。

### 11.3 证据 manifest

manifest 至少记录：

- 输入场景 hash；
- 解析后的 contract identity；
- target/policy 快照；
- worker 版本；
- artifact 文件 hash；
- 样本计数和统计方法；
- 未证明范围，如 `serverTraceUnavailable`、`stateProbeNotCollected`；
- 是否存在中断、取消、进程重启或未知终态。

20 个样本计算 p95 时应标记 small-sample；不能把一个最大值包装成高置信度
的总体 p95。若需要跨批次比较，报告还应保留固定场景 hash 和基线 runId。

## 12. 推荐 MCP/CLI 表面

建议首版只增加最小的四个入口：

| 入口 | 职责 |
|---|---|
| `sofarpc_perf_start` / `sofarpc-mcp perf start` | 校验场景并启动后台 run |
| `sofarpc_perf_status` / `sofarpc-mcp perf status` | 查询进度、当前样本和异常摘要 |
| `sofarpc_perf_result` / `sofarpc-mcp perf result` | 读取 summary、原始样本分页和 manifest |
| `sofarpc_perf_cancel` / `sofarpc-mcp perf cancel` | 显式取消未完成 run，不重发或清理业务数据 |

不建议为“预热”“单样本”“查询终态”“导出 CSV”各增加一个 MCP tool。
这些是一个 performance run 的内部阶段，拆成多个工具会把状态一致性和重试
责任重新推回 agent。

## 13. 分阶段实施计划

### Phase 0：协议和本地闭环

目标：不接真实业务 provider，也能证明性能模型不被普通 invoke 破坏。

- 定义 scenario、sample、observation、artifact 和 stable error code；
- 将现有 `core/invoke` 抽象成可注入 fake transport 的执行接口；
- 用 fake BOLT server 验证单调计时、超时、无重试和样本计数；
- 增加本地 fixture：快速返回、阻塞超时、返回错误、延迟终态；
- 增加 `gofmt`、`go vet`、`go test -race` 覆盖。

完成标准：所有核心错误路径都能用 fake transport 杀死，且普通
`sofarpc_invoke` 现有测试不变绿化。

### Phase 1：客户端批量性能执行器

目标：完成多组合的可审计客户端性能采样。

- 实现后台 worker 和 run lifecycle；
- 支持 warmup/formal、组合串行、并发度 1、样本上限和取消；
- 复用现有 plan/target/policy/transport；
- 强制 `retries=0`，记录 timeout/connectTimeout；
- 输出 NDJSON 原始样本和 summary；
- 提供 CLI，便于不经过 MCP 外层调度执行正式批次。

完成标准：10 个组合 × 20 个正式样本的本地 fake e2e 能证明数量、顺序、
耗时统计、错误分类和 artifact 完整性。

### Phase 2：异步终态与幂等关联

目标：支持交易维护这类“受理 + 异步完成”的业务调用。

- 增加声明式 terminal observer；
- timeout/exception 后只查询同一 requestNo；
- 建立 accept、task、notify 的关联模型；
- 将 `UNKNOWN` 与 `FAILED` 分开；
- 增加重复 requestNo 的专门幂等场景，不混入首次性能统计。

完成标准：fake provider 能制造“accept 超时但任务完成”“任务失败”“查询
窗口耗尽”三种形态，三种结果分别被正确分类。

### Phase 3：服务端 trace 关联

目标：把客户端超时进一步归因到 provider 内部阶段。

- 通过 invocation baggage 传递 run/sample/combination；
- 明确 provider 端 baggage 开关和日志字段契约；
- 在结果中接收或关联 traceId；
- 支持从测试环境日志/trace 查询分段耗时；
- 对没有 provider 能力的目标明确标记未采集。

完成标准：一个真实测试样本能从 requestNo 关联到服务端 trace，并能区分
clientElapsedMs 与 dryRun/replay 等服务端字段；未关联样本不能显示为完整归因。

### Phase 4：只读状态探针与发布门禁

目标：证明失败场景没有业务表副作用，并形成可交付报告。

- 定义 provider-specific `StateProbe` profile；
- 接入只读数据库或既有只读查询服务；
- 实现逐表规范化 snapshot/diff；
- 增加失败场景零差异 oracle；
- 在 release/人工 e2e 文档中加入 artifact 和未证明范围。

完成标准：成功和失败场景都能输出逐表 diff；数据库无法访问时测试明确
阻断“完整业务分析”结论，而不是静默跳过。

## 14. 测试与验证门禁

### 14.1 核心单元测试

- scenario 展开：组合数、预热数、正式数、唯一 sampleId；
- requestNo 生成：长度、唯一性、组合映射和敏感值不泄露；
- timing：使用 monotonic elapsed，不把 epoch 差当耗时；
- timeout：阻塞 transport 被分类为 timeout，且没有第二次写调用；
- retry：配置或代码意外打开 retry 时 run 被拒绝；
- observer：同一 requestNo 查询，不能使用最后一条全局响应；
- terminal classification：completed、failed、unknown 分开；
- statistics：预热不进入正式分位，超时不被静默丢弃；
- artifact：raw、summary、manifest 的 hash 和样本数一致；
- policy：未显式允许时，在 wire IO 之前拒绝。

### 14.2 本地 fake BOLT e2e

覆盖以下业务无关但可证伪的形态：

```text
fast return
slow return beyond 3s
transport exception
accept response + delayed completed task
accept timeout + completed task
accept timeout + failed task
query timeout -> UNKNOWN
duplicate requestNo -> idempotent hit
worker cancellation
worker restart -> no automatic write replay
```

### 14.3 真实测试环境验证

只在显式用户授权和目标白名单下执行：

- 先 `sofarpc_*` dry-run/describe/target；
- 再小批量 1 组合冒烟；
- 再多组合小样本；
- 最后正式批次；
- 任何 accept timeout 都只查询不重写；
- 结果包含 client、terminal、server trace 和 state probe 的各自状态。

仓库默认 CI 只运行本地 fake e2e；真实 provider 互操作继续作为手工或受控
workflow，不应让普通 PR 测试依赖业务网络。

## 15. 范围与成本估算

以下是首版实施估算，实际以代码勘探和现有测试复用程度校准：

| 范围 | 组件 | 估算 | 风险 | 价值 |
|---|---|---:|---|---|
| **核心** | perf scenario/worker/scheduler/measurement | 2-3 人日 | 中 | 获得可信多样本客户端性能 |
| **核心** | timeout/retry/observer/terminal state | 1.5-2.5 人日 | 高 | 防止超时被误判或重复写入 |
| **核心** | MCP + CLI start/status/result/cancel | 1-2 人日 | 中 | 让 agent/CI 能管理完整批次 |
| **支撑** | artifact、统计、脱敏和 manifest | 1-1.5 人日 | 中 | 形成可复核交付证据 |
| **支撑** | fake BOLT e2e、race/vet/build 门禁 | 1-1.5 人日 | 低 | 防止性能层破坏普通 invoke |
| **可选** | provider trace 关联 | 1-3 人日 | 高 | 从“变慢”推进到“慢在哪里” |
| **可选** | 业务只读 StateProbe/DB diff | 2-4 人日 | 高 | 闭合业务副作用证明 |
| **总计（核心 + 支撑）** | 首版客户端完整性能执行能力 | **6.5-10.5 人日** | 中 | 覆盖客户端、批量、终态和证据闭环 |

Phase 3/4 是 provider 和业务环境相关的可选扩展，不应阻塞 Phase 1/2 的
客户端能力交付；但没有它们不能宣称“完整服务端归因”或“逐表业务状态已证明”。

## 16. 最佳性检查

| 检查项 | 结论 |
|---|---|
| 适配标准 | 不污染普通 invoke；计时可信；能处理副作用和超时；支持多组合；结果可审计 |
| 胜出方案 | 独立 `perf` 执行域 + 复用现有 core/invoke + 后台 worker；职责隔离且能渐进接入 trace/DB |
| 最接近替代方案 | 保留现有独立 Java/Go Runner，仅补强 CSV 和终态查询；短期最快，但 MCP/CLI 无统一生命周期和安全协议 |
| 替代方案胜出条件 | 近期只做一次或少数几次性能批次，且已有稳定外部 Runner、结果归档和 DB/trace 工具 |
| 边际收益停止点 | Phase 2 后已能回答 3 秒 SLA 和异步终态；在没有 provider/DB 支持前继续堆 generic 自动归因，收益低且会扩大安全面 |

### 反向检验

如果 provider 不记录 baggage、业务没有可查询终态，独立 perf worker 仍能证明
客户端 transport 性能，但不能证明“预演完成”或“无业务写入”。因此 schema
必须把这些条件作为能力前置检查，而不是在报告中隐式假设成功。

如果后台 worker 无法获得可靠的生命周期存储，`status` 可能在 MCP 进程重启
后丢失；此时只能降级为一次性 CLI run，不能自动恢复写样本。这个失败条件
支持保留 CLI 入口作为独立验证和应急手段。

## 17. 明确非目标

- 首版不支持 SOFARPC registry 发现；
- 首版不支持任意协议和任意序列化；
- 不把普通 `sofarpc_invoke` 改成无上限批量工具；
- 不默认开启真实网络调用；
- 不把业务数据库凭证或业务 SQL 写进 generic CLI；
- 不自动清理测试目标上的业务任务、日志、Redis 或 outbox；
- 不把 MCP 外层耗时当成 SOFARPC client latency；
- 不在缺少服务端 trace/DB probe 时声称已经完成完整归因。

## 18. 下一步验证

最便宜、最能改变方案的验证是实现 Phase 0 的 fake transport spike，并验证
以下四条：

1. 一个阻塞超过 3 秒的调用只出现一次 write call；
2. 同一 requestNo 的 observer 能把“超时但最终完成”与“任务失败”区分开；
3. 10 个组合 × 2 次预热 + 20 次正式样本的样本计数和正式分位正确；
4. MCP 进程取消或退出后不会自动重发未完成的写样本。

这四条任意一条无法在本地 fake e2e 中被杀死，就不应进入真实业务性能批次。
