---
title: SOFARPC Java Reference Runner 有界验证计划
date: 2026-09-01
status: proposed
---

# SOFARPC Java Reference Runner 有界验证计划

**模式**: Plan
**深度**: Deep
**输入来源**: `README.md`、`docs/architecture.md`、原性能计划、性能测量契约研究、当前 Go transport、Java wire fixture、官方 SOFARPC 5.4.0/5.7.6/5.8.0 与 sofa-bolt 1.5.9 源码

## TL;DR

当前最佳路径是先建设一个只面向本地 fake provider 的官方 Java Consumer
reference runner，用最小实验验证长连接复用、request-id 并发关联、分离 timeout
和零重试。它不是新的生产功能，也不连接真实业务 provider。

该 spike 通过后，才能判断现有 pure-Go transport 应该升级为 pooled transport，
还是正式性能采样继续以 Java runner 为测量主体。原计划的后台 MCP
`start/status/result/cancel`、trace、DB probe 和业务批量写入全部暂缓。

## 当前最佳路径

1. 新建隔离的 `tests/perfref/`，不把性能生命周期塞进只负责序列化的
   `internal/sofarpcwire/testdata/java-fixtures/`。
2. Java runner 使用官方 `ConsumerConfig` + direct BOLT URL，显式冻结版本、
   `connectionHolder=all`、`lazy=false`、`connectionNum=1`、connect/invoke
   timeout、`retries=0` 和 connection reuse；每次 run 输出 NDJSON raw samples
   与 manifest，不先做聚合平台。
3. Go fake provider 持续接受并解析同一 TCP 连接上的多个 BOLT request，记录
   accept 数、request id、到达/响应顺序和连接关闭；支持正常、乱序响应、
   connect-refused、invoke-timeout 四种模式。
4. 只运行本地 loopback 验证。任何真实 provider、业务写 facade、MCP 客户端
   配置变更都不属于本计划。
5. 以 5.8.0 为首个 baseline；通过后用同一套 oracle 验证 5.4.0 和 5.7.6，
   不为每个版本复制 runner。

## 价值门禁与决策信封

```yaml
decision: BUILD
decision_source: 用户已要求执行下一验证步骤
target_outcome: 获得一个可执行、可复核的生产式 Java Consumer 测量基准，阻止把 fresh-connection CLI latency 误写成生产 Consumer latency
baseline_and_frequency: 当前只有 pure-Go fresh-connection 路径；正式性能任务频率未知
expected_benefit: 用 4 个本地场景在真实业务采样前否定错误测量主体，避免继续投入 6.5-10.5 人日建设错误平台
delivery_and_maintenance_cost: spike 约 1.5-2.5 人日；仅测试依赖，后续维护跟随三版本兼容矩阵
status_quo_or_existing_mechanism: 继续使用现有 Go fake spike，只能证明当前 Go transport，不能证明 Java reference
decision_flip_condition: 若现有业务 Runner 能提供源码、固定版本/配置及相同四类原始证据，直接复用它并取消本 spike
review_scope: implementation-authorization
review_budget: bounded spike
```

完整后台 perf 平台仍是 `RESEARCH_FIRST`：必须先证明 reference runner 可用、
任务确实重复发生且 artifact/lifecycle 的收益大于维护成本。

## 验收 oracle

所有 oracle 必须由 fake provider 的 wire 观测和 runner 的 raw artifact 共同证明：

| 场景 | 必须观察到 | 失败即证明 |
|---|---|---|
| 串行复用 | 1 次 accept；1 次 warmup + N 次正式 request；正式阶段 0 次新连接 | runner 不是稳态长连接主体 |
| 并发乱序 | 同一连接上至少 2 个不同 request id；响应逆序后每个调用仍拿到自己的结果 | request-id demux 或并发模型不成立 |
| connect failure | 连接本地已关闭端口时在 connect 边界失败；正式 request 数为 0；错误分类为 `CONNECT_FAILED` | connect/invoke failure 未分离 |
| invoke timeout | 只收到 1 个 request；无第二次写入；分类为 `INVOKE_TIMEOUT` | retries=0 未真正生效 |

额外不变量：

- manifest 记录 SOFARPC/BOLT 解析后版本、JVM、git revision、所有最终客户端配置；
- warmup 不进入正式分位，建连耗时单独记录；
- runner raw sample 保留 logical sample id、单调耗时和 outcome；fake provider
  artifact 保留 wire request id，两者通过唯一参数/响应关联，不要求官方 API
  暴露内部 BOLT request id；
- 测试失败时仍保留临时 artifact 路径供复核；成功时 CI 可清理临时产物；
- 不声称 live-provider 或外部 MCP-client 互操作。

loopback 无法稳定制造 TCP connect timeout：关闭端口通常立即返回 connection
refused。因而本地 spike 只把 connect failure 与 invoke timeout 分类分开；真正的
`connectTimeout` 运行时命中必须在可控黑洞网络中单独验证。未运行时 manifest
记录 `connectTimeoutRuntimeVerified=false`，且 artifact 只能叫
`official-default-reference`，不能叫 `production-equivalent-consumer`。

## 预计改动面

| 范围 | 组件 | 预计位置 | 工作量 | 风险 | 价值 |
|---|---|---|---:|---|---|
| **核心** | Java Consumer runner + 显式配置 + NDJSON | `tests/perfref/java/` | 0.75-1.0 人日 | 中 | 建立真实 reference 主体 |
| **核心** | 可持续多请求/乱序/超时 fake BOLT provider | `tests/perfref/fakeprovider/` 或 Go test helper | 0.5-0.75 人日 | 中 | 从 wire 侧证伪连接和重试 |
| **支撑** | 5.8.0 自动化 oracle 与 artifact 校验 | `tests/perfref/`、`scripts/` | 0.25-0.5 人日 | 低 | 一条命令复核结论 |
| **支撑** | 5.4.0/5.7.6 兼容矩阵 | 复用同一 Maven property 和测试 | 0.25 人日 | 中 | 覆盖仓库声明版本 |
| **可选** | Windows/Linux 双脚本包装 | `scripts/` | 0.25 人日 | 低 | 本地易用性；不影响 oracle |
| **总计（核心 + 支撑）** | | | **1.5-2.5 人日** | 中 | 关闭测量主体门禁 |

如果为了让 fake provider 支持完整 SOFARPC 响应而需要新增通用 provider runtime，
或总成本超过 2.5 人日，应立即停下并改用官方 Java provider；不得借 spike 扩展
成通用压测框架。

## 根问题、约束与假设

根问题不是“缺一个批量循环”，而是缺少一个可证明与生产 Java Consumer 一致的
测量主体。当前 Go 调用每次建连；官方兼容版本默认复用长连接，因此直接扩大量级
只会更精确地测错对象。

真实约束：

- 仓库 mainline 继续是 pure-Go direct + BOLT + Hessian2；reference runner
  只能是隔离测试工具，不能重新引入主进程 Java worker。
- 真实调用默认禁用；本计划只允许 `127.0.0.1` 动态端口。
- 兼容矩阵是 SOFARPC 5.4.0、5.7.6、5.8.0。
- retries 必须为 0；timeout 后禁止自动重发写请求。
- 现有 wire fixture 证明编码兼容，但不能替代连接生命周期测试。

未验证假设：

| 假设 | 若为假 | 验证 |
|---|---|---|
| SOFARPC Java direct Consumer 可对当前 Go fake provider 完成多次调用 | 换成官方 Java provider 并从 socket 层统计连接 | 先做 5.8.0 单请求 smoke |
| 三版本可用同一 runner source | 为版本差异加极小 adapter；不能复制三套逻辑 | Maven property 矩阵编译 |
| 业务生产配置接近官方默认 | reference 只能称“官方默认基准” | 后续取得实际 Runner/部署配置 |
| 性能分析会重复发生 | 完整平台 ROI 不成立 | reference 通过后记录实际使用频率 |

## 机制比较与最佳性检查

| 机制 | 适配结果 | 主要代价/失败条件 |
|---|---|---|
| 保持现状 | 最便宜，但只能测 CLI fresh connection | 无法回答生产 Consumer 性能 |
| 手工检查实际业务 Runner | 若现有 runner 材料齐全则最优 | 当前缺源码、配置和原始报告 |
| 给 pure-Go transport 直接加池 | 可能成为长期产品方向 | 必须同时补 demux、并发、坏连接、heartbeat/reconnect；验证面过大 |
| Java reference bounded spike | 最小化地复用官方客户端语义并提供可执行 oracle | 增加测试侧 Java/Maven 成本 |
| 直接建后台 MCP perf 平台 | 生命周期最完整 | 在主体错误时规模化错误结论，成本最高 |

**最佳性检查**：

- 适配标准：测量主体正确、能被 wire 证伪、不污染 pure-Go mainline、无真实业务
  风险、成本小于完整平台。
- 胜出方案：Java reference bounded spike；它以最小新代码复用官方 Consumer
  语义，并直接杀死当前最大不确定性。
- 最接近替代方案：复用现有业务 Runner；一旦能提供相同证据，它立即胜出。
- 击败条件：现有 Runner 材料齐全，或真实部署明确使用 fresh connection。
- 边际收益停止点：四个 oracle 通过后停止；不在 spike 中加入 UI、MCP lifecycle、
  DB probe、trace backend、容量加压或真实业务写入。

反向检验：如果实际生产配置禁用了复用，Java 默认 runner 会成为错误 reference；
因此 artifact 必须叫 `official-default-reference`，直到实际配置核对后才允许升级为
`production-equivalent-consumer`。该命名门禁可以缓解，不需要阻塞本地 spike。

## 实施与回退边界

- 先让 5.8.0 单请求 smoke 绿，再增加串行复用；之后才加并发乱序和两个 timeout。
- fake provider 必须在测试进程结束时关闭 listener/connection；不写固定端口或
  machine-local config。
- Maven `target/`、NDJSON 和临时 manifest 都是生成物，不提交；只提交 runner、
  fake provider、断言和文档。
- 如果任一版本需要不同语义，manifest 分版本报告；不把三个结果平均成一个结论。
- 删除 `tests/perfref/` 即可完整回退，不应触碰生产调用路径或用户 MCP 配置。

## 后续决策

spike 通过后只做一次选择：

- 若正式性能证据只服务少量人工任务：保留 Java runner + runbook，`NO_BUILD`
  通用后台平台；
- 若需要频繁、agent-managed、可恢复的批量任务：以该 runner 为 reference，重新
  规划 artifact/journal/lifecycle；
- 若产品必须保持零 Java 依赖：另立 pooled pure-Go transport 项目，并用本 runner
  做黑盒等价 oracle，不能只靠单元测试自行声明等价。
