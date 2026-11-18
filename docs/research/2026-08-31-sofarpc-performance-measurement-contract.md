**问题**: `sofarpc-cli` 的性能结果究竟测量什么，当前 pure-Go 路径能否代表生产 Java Consumer 的服务性能？
**深度**: Deep
**核心结论**: 当前 pure-Go 路径只能声明为 `fresh_connection` 客户端调用测量；官方 SOFARPC 5.4.0、5.7.6、5.8.0 Java Consumer 默认均预连接并复用长连接，因此该路径不能代表生产等价测量。若目标是生产 Java Consumer 等价，当前最佳下一步是先建立固定版本和配置的官方 Java reference runner，而不是先实现后台 MCP performance worker。
**产物类型**: supporting
**验证状态**: code-only + local fake runtime tested + official-source checked
**开放问题**: 1 - 见文末

## TL;DR

- 当前 `boltclient.Invoke` 每次样本都会 `DialContext`，并在返回前关闭连接；
  没有连接池或连接复用。
- 官方 SOFARPC 5.4.0、5.7.6、5.8.0 的默认 Consumer 都使用 `all`
  connection holder、非 lazy 预连接和长连接复用；5.7.6/5.8.0 还显式以
  `transport.connection.reuse=true` 选择共享连接管理器。底层 BOLT 在同一连接
  上以 request id 维护多个 `InvokeFuture`，不是一请求一连接模型。
- `connectTimeoutMs` 当前只用于 target probe，没有进入真实 invoke 的拨号
  路径。真实 invoke 只有 request/socket timeout，且该 timeout 在拨号完成后才
  设置到连接上。
- 本地 fake BOLT 实验连续三轮得到相同结构结果：5 个样本对应 5 次 TCP
  accept；timeout 场景只接收 1 次请求，没有 transport 自动重发。
- 每轮第一个样本约 0.93-0.95s，后续样本约 15.9-18.0ms；fake server 固定
  延迟 15ms。该绝对值只说明首调用存在本机冷启动噪声，不能外推到真实
  provider。
- 仓库当前没有计划所称的批量 Runner。`cmd/spike-invoke` 是单次、明确标注
  throwaway 的开发工具。历史上存在 Java worker pool，但它已随 pure-Go
  mainline 切换而删除，不能视为当前可复用能力。
- 决策保持 `RESEARCH_FIRST`，但研究方向已经收窄：默认目标按“生产 Java
  Consumer 等价”处理，先建立 Java reference runner；只有用户把目标改为
  “测量 sofarpc-cli 自身”时，pure-Go fresh-connection runner 才成为正确主体。

## 1. 决策边界

默认目标是获得可以与生产 Java Consumer 比较的服务性能证据，而不是只测量
`sofarpc-cli` 自身的一次性调用耗时。

该目标要求先区分三种不同问题：

| 问题 | 可回答的结论 | 当前 pure-Go 支持 |
|---|---|---|
| CLI 一次调用要多久 | 包含编码、建连、wire wait、解码的客户端总耗时 | 是，连接模式固定为 fresh |
| 常驻 Consumer RPC 要多久 | 已建立/可复用连接上的请求响应耗时 | 否，当前没有 pooled transport |
| 业务最终完成要多久 | 受理、异步任务、通知和终态的端到端耗时 | 否，需要业务 observer/trace |

在没有明确声明这三者中的哪一个是报告主体时，增加样本数量、p95 或 trace
字段不会让结论变得有效。

## 2. 当前调用路径

```mermaid
flowchart LR
  P[core/invoke.Execute] --> D[ExecuteDirectIfPossible]
  D --> E[BuildGenericRequest]
  E --> B[boltclient.Invoke]
  B --> C[Encode BOLT frame]
  C --> N[DialContext: 每次新连接]
  N --> W[Write request]
  W --> R[Read response]
  R --> X[Close connection]
  X --> H[Decode Hessian response]
```

代码证据：

- `internal/core/invoke/direct.go:67-90` 将 `timeoutMs` 传给
  `sofarpcwire.InvokeDirect`，但没有传递 `ConnectTimeoutMS`。
- `internal/sofarpcwire/client.go:26-39` 在进入 `boltclient` 前构造请求，在返回
  后解码响应。
- `internal/boltclient/client.go:64-98` 每次调用编码 frame、拨号、
  `defer conn.Close()`、写入并读取一个响应。
- `internal/core/target/probe.go:22-35` 才使用 `ConnectTimeoutMS`；这是 target
  reachability probe，不是真实 invoke。

这条路径证明当前实现测量的是 fresh-connection client path。它没有证明该路径
与任何具体生产 Java Consumer 的连接、线程池、序列化缓存或负载均衡行为等价。

## 3. 本地 fake BOLT 实验

### 3.1 实验设计

临时程序对本机 loopback fake BOLT server 执行：

1. 每轮连续调用 `boltclient.Invoke` 5 次；
2. server 对每次请求固定等待 15ms 后返回一个合法空 BOLT response；
3. 记录 server 的 TCP accept 数和完整 request 数；
4. 另起一个 server，收到 request 后等待 100ms；client request timeout 为
   20ms，记录 accept 和 request 数；
5. 编译一次后独立运行三轮，避免把单轮偶然结果当作事实。

执行命令：

```powershell
go build -o tmp\measurement-spike\measurement-spike.exe .\tmp\measurement-spike
.\tmp\measurement-spike\measurement-spike.exe
```

### 3.2 结果

| 轮次 | 正式调用 | TCP accept | 完整 request | 首样本 | 后续样本范围 | timeout accept/request |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 5 | 5 | 5 | 931.512ms | 16.435-17.999ms | 1 / 1 |
| 2 | 5 | 5 | 5 | 951.206ms | 15.999-17.002ms | 1 / 1 |
| 3 | 5 | 5 | 5 | 942.707ms | 16.741-17.000ms | 1 / 1 |

所有 timeout 均返回 `read response header ... i/o timeout`。实验只证明：

- 一次 `boltclient.Invoke` 对应一次 TCP 连接；
- 当前 transport 没有在 read timeout 后自动发起第二次请求；
- 首调用与后续调用存在足以污染小样本分位数的冷启动差异。

实验不证明：

- 0.93-0.95s 来自哪个 Windows 本机组件；
- 真实网络、真实 provider 或 Java Consumer 的绝对耗时；
- provider 在 client timeout 后是否继续执行业务任务；
- `core/invoke` 以上 MCP 排队或进程调度的耗时。

因此本文不使用实验绝对值评价 SOFARPC 服务性能。

## 4. 仓库能力与历史核对

### 4.1 当前没有批量 Runner

仓库内容、全部 git refs 和提交主题中没有可验证的 performance/benchmark
Runner。当前最接近的 `cmd/spike-invoke/main.go`：

- 文件头明确写明 throwaway developer harness；
- 只构造一个 plan 并调用一次 `coreinvoke.Execute`；
- 没有组合调度、原始样本、终态 observer 或 artifact lifecycle。

所以原计划“已有独立 Runner 证明多组合采样可行”的主张目前为
`Unsupported`，不能用于支持 `BUILD` 或工期估算。

### 4.2 历史 Java worker pool 不是当前能力

提交 `44b7b10` 曾包含 `internal/worker/pool.go` 和长驻 Java worker。提交
`f6a0e76` 切换到 pure-Go runtime 时删除了整个 worker/pool/process 子系统，
并将 mainline 明确改为 pure-Go `direct + bolt + hessian2`。

历史实现可以作为“生产式长驻客户端可能需要生命周期管理”的参考，但复活它
意味着重新引入 Java runtime、worker process 和 pool 维护，违背当前安装与架构
边界。它不是比 CLI-first measurement runner 更便宜的现有能力。

### 4.3 官方 Java Consumer 适用性核对

仓库 wire fixture 明确覆盖 SOFARPC `5.4.0`、`5.7.6`、`5.8.0`。本次对三个
官方 tag 的固定 commit 做了源码核对：

| SOFARPC tag | 固定 commit | 默认连接语义 | 默认 connect/invoke timeout | 默认 retries |
|---|---|---|---|---:|
| 5.4.0 | `37b7ce58ae5e6544b36dcd6a9e89526f763e590c` | `all` holder，非 lazy；`BoltClientConnectionManager` 缓存并引用计数长连接 | 5000 / 5000ms | 0 |
| 5.7.6 | `84d09c200d1940d1cec417dbb5bd611cd3a1b15d` | `all` holder，非 lazy；默认 `ReuseBoltClientConnectionManager` | 1000 / 3000ms | 0 |
| 5.8.0 | `75a672ae8bcf6524487b3a9ed49fc54b17ebfa25` | 与 5.7.6 相同，并保留连接健康检查和重连 holder | 1000 / 3000ms | 0 |

三个版本的 `BoltClientTransport` 都把 connect timeout 用于取得连接，把 invoke
timeout 单独传给 `RpcClient.invokeSync`。5.4.0 的连接管理器已经以
`ClientTransportConfig -> Connection` 的并发 map 复用连接；5.7.6/5.8.0
进一步把 reusable/standalone 管理器显式拆分。SOFARPC 默认 `failover` cluster
虽然支持在 busy/timeout 后按配置重试，但默认 `consumer.retries=0`，所以正式
性能 run 仍必须把最终生效值冻结为 0，不能只依赖默认值。

底层官方 sofa-bolt 对应版本为 1.4.1、1.5.6、1.5.9。以 1.5.9 为代表，
`Connection` 按 request id 保存并发 `InvokeFuture`，`BaseRemoting` 在发送前
登记、响应/失败/超时后移除；连接 pipeline 还包含 heartbeat，并有 reconnect
事件处理。这证明生产等价的 pooled transport 不只是“把 `net.Conn` 放进一个
字段”，还包含请求关联、并发安全、坏连接淘汰及生命周期语义。

#### Mixed applicability check

| 证据 | 对当前仓库是否适用 | 结论 |
|---|---|---|
| 三个官方 tag 的 wire/Consumer 默认 | 适用于仓库声明的兼容版本矩阵 | Java reference 的默认主体是复用长连接 |
| 当前 Go `boltclient.Invoke` | 直接适用 | 每次调用 dial + close，且 connect timeout 未进入 invoke |
| wire fixture 跨版本一致 | 只证明编码兼容 | 不能推出连接生命周期、重试或计时边界等价 |
| 官方默认配置 | 适用于未覆盖配置时 | 真实业务 Runner 仍可能覆盖 reuse、lazy、connectionNum、timeout、retries |

因此不存在证据冲突：当前 Go 实现可以 wire-compatible，同时不是
runtime/performance-equivalent。实际业务 Runner 的配置仍是最终对照所需输入，
但它不再是判断“当前 pure-Go 能否直接声称等价”的前置条件；官方默认与当前
实现已经足以否定该声称。

### 4.4 机制比较与最低等价契约

| 机制 | 能回答的问题 | 主要失败条件 | 当前判断 |
|---|---|---|---|
| 现有 pure-Go fresh connection | CLI 一次完整调用成本 | 被误标为服务端/生产 Consumer latency | 仅可作为 CLI baseline |
| 给 Go transport 增加连接池 | 可逼近常驻 Consumer RPC | 只复用 socket，却缺少 request-id demux、并发、心跳、坏连接和重连验证 | 需要独立 transport 项目，不是最短 reference path |
| 官方 Java Consumer reference runner | 固定版本/配置下的生产式客户端证据 | Java runtime 成本；若与业务实际版本/配置不同仍不能外推 | production-equivalence 的当前最佳机制 |
| 后台 MCP perf 平台 | 长任务生命周期和 artifact 管理 | 测量主体未校准时会把错误模型规模化 | 暂缓 |

任何声称 `production-equivalent-consumer` 的执行器至少要证明：

1. 连接在 warmup 后复用，连接数和 `connectionNum` 可审计；
2. connect timeout 与 invoke timeout 分离并实际生效；
3. 同一连接并发请求按 BOLT request id 正确关联响应；
4. 坏连接、heartbeat 和 reconnect 策略被固定或明确禁用，并写入 manifest；
5. retries 的最终生效值为 0；
6. wire bytes/序列化与目标版本兼容（当前 fixture 已覆盖这一层）；
7. 计时边界明确排除建连和 warmup，或将其作为独立字段，而不是混入稳态 RPC。

Java reference runner 的最小验证不是先做 200 笔业务写调用，而是本地/测试 fake
provider 上的对照：warmup 后 N 次串行调用只新增 0 个连接；并发调用能正确按
request id 回收响应；invoke timeout 只发送一次且无重试。connect failure 可用
本地关闭端口验证分类，但 TCP connect timeout 需要可控黑洞网络，不能用 loopback
稳定伪造；未命中该运行时 oracle 时必须在 manifest 标为未验证。上述门禁通过后
才允许用它校准真实小批量读调用；写调用仍受 §5.5 的 journal/observer 门禁。

## 5. 测量契约 v0

### 5.1 必填身份

每个 run manifest 必须冻结：

```text
measurementSubject: sofarpc-cli | production-equivalent-consumer
connectionMode: fresh | pooled
clientImplementation: pure-go-bolt | java-sofarpc | other
clientVersion / gitRevision
targetProfile + redacted target identity
scenarioHash
timeoutPolicy
sampleOrdering
```

当前实现唯一合法声明是：

```text
measurementSubject: sofarpc-cli
connectionMode: fresh
clientImplementation: pure-go-bolt
```

在实现 pooled transport 或完成 Java Runner 对照前，
`production-equivalent-consumer` 必须被 preflight 拒绝。

### 5.2 计时字段

目标契约至少区分：

| 字段 | 起止边界 | 当前可独立获得 |
|---|---|---|
| `prepareElapsed` | contract-normalized plan 到 BOLT frame 完成 | 否 |
| `connectElapsed` | 开始 dial 到 TCP connect 成功 | 否 |
| `rpcElapsed` | 连接可用到完整 BOLT response 读完 | 否 |
| `decodeElapsed` | response bytes 到 JSON-friendly result | 否 |
| `clientTotalElapsed` | prepare 开始到 decode 结束 | 可在外层获得 |
| `terminalElapsed` | accept call 开始到业务终态 | 需要 observer |

在 transport 提供分段 receipt 前，报告只能给出 `clientTotalElapsed`，并明确
包含 fresh TCP connect。不能把它重命名成 server latency。

### 5.3 timeout 契约

- `connectTimeout`：只约束 dial；必须实际进入 `net.Dialer.Timeout` 或独立的
  connect context。
- `rpcTimeout`：连接成功后约束 write/read，并写入 BOLT request timeout。
- `totalDeadline`：可选，约束 prepare + connect + RPC + decode 的总边界。
- timeout 后禁止 transport 自动重试写请求。
- timeout 只证明“客户端没有收到完整响应”；不能证明 provider 未受理。

当前 `connectTimeoutMs` 不能写入 invoke performance manifest 的“已生效配置”，
只能标为 `configured-but-not-applied`，直到执行路径被修正并有测试证明。

### 5.4 样本与统计契约

- 所有 warmup 仍写入 raw artifact，但不进入正式统计。
- 每个 combination 的顺序必须记录；需要比较 combination 时使用交错 block
  或明确记录顺序偏差，不能默认按 M01 全部执行完再执行 M02 后声称可比。
- `deadlineViolationRate` 分母是所有已发送 formal 样本；timeout、迟到返回和
  未知结果均不能从分母中消失。
- `returnedOnly` 分位使用固定 nearest-rank：排序后取
  `ceil(p * n)` 对应的样本。
- `n < 100` 时不发布 p99；`n=20` 的 p95 只作为描述性 order statistic，
  不作为高置信 SLA 证明。
- censored timeout 与实际返回耗时分开保存；除非实现并声明生存分析方法，
  不生成混合“延迟分布”。

### 5.5 写调用安全契约

每个样本需要持久状态：

```text
PLANNED
  -> INTENT_PERSISTED
  -> DISPATCHING
  -> RETURNED | TIMEOUT | TRANSPORT_UNKNOWN
  -> TERMINAL_COMPLETED | TERMINAL_FAILED | TERMINAL_UNKNOWN
```

- `INTENT_PERSISTED` 必须发生在任何 wire IO 前，包含 runId、sampleId、
  requestNo、目标和 plan hash。
- 进程崩溃后，`DISPATCHING` 及后续未知状态只能 observer/query，不得重发。
- cancel 先停止调度新样本，再收敛已发送样本；强制退出必须在 manifest 中保留
  未收敛样本清单。
- 实际授权应绑定 scenario hash、目标、service/method、最大请求数和 owner，
  不能只依赖进程级 `SOFARPC_ALLOW_PERF_RUN=true`。

## 6. 决策

```yaml
decision: RESEARCH_FIRST
target_outcome: 与声明的客户端模型一致、可复核且不会重复写入的性能证据
verified_baseline: pure-Go 是 fresh-connection 单请求路径；三个官方兼容版本默认均复用长连接；本地 timeout 无自动重发
unsupported_baseline: 已有批量 Runner；当前 pure-Go 与生产 Java Consumer 等价
current_best_path: 固定 SOFARPC 版本和配置的官方 Java reference runner
decision_flip_condition: reference runner 通过本地连接复用、request-id 关联、分离 timeout 和零重试契约，并取得实际业务 Runner 的版本/配置用于适用性核对
```

在翻转条件满足前：

- 不实现后台 MCP `start/status/result/cancel`；
- 不实现 DB probe 或 trace aggregation；
- 可以独立修正 `connectTimeoutMs` 的执行语义，但那属于 transport correctness，
  不是 performance platform 的建设授权。

默认目标已经按“生产 Java Consumer 等价”收敛，所以下一工作单元是 Java
reference runner 的最小本地对照，不是 pure-Go perf worker。如果目标被显式
改成只测 `sofarpc-cli` 自身，且该能力会重复使用，下一实现候选才是前台、
单进程、journal 驱动的 `sofarpc-mcp perf run`。

## 7. Research-to-work handoff

**Decision**

- 当前停止在 research gate；不要从原计划 Phase 0 开始实现后台 worker。

**Evidence**

- 当前代码每次 invoke 新建并关闭 TCP 连接；`connectTimeoutMs` 未进入调用路径。
- 官方 5.4.0/5.7.6/5.8.0 默认均为预连接、长连接复用、retries=0；connect
  与 invoke timeout 分开生效。
- 三轮 fake runtime 均为 5 samples / 5 accepts / 5 requests；timeout 为
  1 accept / 1 request。
- 仓库无批量 Runner；历史 Java pool 已被 pure-Go 重构删除。

**Risk**

- 未获得 reference Runner 就实现，会把 fresh-connection CLI latency 误写成
  production service latency。
- 未实现 write-ahead journal 就批量执行写 facade，崩溃时可能出现无法枚举的
  已受理任务。

**Verification**

下一工作流先构建并检查：

```text
<fixed SOFARPC tag and resolved dependency versions>
<explicit reuse/lazy/connectionNum/connectTimeout/invokeTimeout/retries config>
<fake provider connection-count and request-id correlation evidence>
<raw serial/concurrent/timeout samples and manifest>
```

进入真实业务对照前，仍需取得现有业务 Runner 的源码/版本/最终生效配置和一份
原始样本报告；如果不存在该 Runner，就把业务部署使用的 SOFARPC Consumer
版本和配置作为 reference applicability 输入。

**Next workflow**

- 为 Java reference runner 创建一个 bounded spike 计划；先在本地 fake provider
  杀死连接和 timeout 反例。只有该 spike 通过并证明需求具有重复价值，才重新
  评估后台 perf 平台是否 `BUILD`。

## 8. 开放问题

1. 原计划所称的现有 Java/Go Runner 位于哪里；若它不存在，实际业务部署使用
   哪个 SOFARPC Consumer 版本及最终连接/timeout/retry 配置？

## 9. 研究闭环

- **已确认答案**：当前 pure-Go path 是 fresh-connection 客户端路径；本地实验
  证明每样本建连一次且 transport timeout 不自动重发；三个官方兼容版本的
  Java Consumer 默认均为长连接复用，因而当前两者不等价。
- **置信基础**：当前源代码、三轮独立 local fake runtime、git 历史、三个固定
  SOFARPC tag 和官方 sofa-bolt 1.5.9 的 request-id/连接生命周期代码一致。
- **最强未决反例**：实际业务 Consumer 可能覆盖默认配置而使用 fresh connection；
  这不会让当前 Go 路径自动等价，除非其最终版本、配置和对照样本均被证明一致。
- **翻转条件**：Java reference runner 通过 §4.4 的最小契约，或用户显式将测量
  主体改为 `sofarpc-cli` fresh-connection latency。
- **停止原因**：源码研究已连续得到相同结论；下一条能改变决策的证据必须来自
  可执行 reference runner 或实际业务 Consumer 配置，而不是更多静态搜索。

## 10. 来源审计

| 主张 | 来源 | 获取方式 |
|---|---|---|
| 每次 invoke 新建并关闭连接 | `internal/boltclient/client.go:64-98` | 本次读取 |
| `connectTimeoutMs` 只用于 probe | `internal/core/invoke/direct.go:67-90`; `internal/core/target/probe.go:22-35` | 本次读取与搜索 |
| 当前 spike 是单次 throwaway harness | `cmd/spike-invoke/main.go:1-6,47-116` | 本次读取 |
| 历史 Java pool 已删除 | commits `44b7b10`, `f6a0e76`; former `internal/worker/*` | 本次 `git show` |
| 5 samples = 5 connections；timeout 无重发 | 临时 local fake BOLT experiment，三轮结果见 §3.2 | 本次运行 |
| 5.4.0 默认缓存复用连接 | [BoltClientTransport](https://github.com/sofastack/sofa-rpc/blob/v5.4.0/extension-impl/remoting-bolt/src/main/java/com/alipay/sofa/rpc/transport/bolt/BoltClientTransport.java)；[BoltClientConnectionManager](https://github.com/sofastack/sofa-rpc/blob/v5.4.0/extension-impl/remoting-bolt/src/main/java/com/alipay/sofa/rpc/transport/bolt/BoltClientConnectionManager.java) | 固定 tag 浅克隆读取 |
| 5.7.6/5.8.0 默认选择 reusable connection manager | [5.7.6 BoltClientTransport](https://github.com/sofastack/sofa-rpc/blob/v5.7.6/remoting/remoting-bolt/src/main/java/com/alipay/sofa/rpc/transport/bolt/BoltClientTransport.java)；[5.8.0 BoltClientTransport](https://github.com/sofastack/sofa-rpc/blob/v5.8.0/remoting/remoting-bolt/src/main/java/com/alipay/sofa/rpc/transport/bolt/BoltClientTransport.java) | 固定 tag 浅克隆读取 |
| 三版本默认 holder/timeout/retry/reconnect 配置 | [5.4.0 defaults](https://github.com/sofastack/sofa-rpc/blob/v5.4.0/core/common/src/main/resources/com/alipay/sofa/rpc/common/rpc-config-default.json)；[5.7.6 defaults](https://github.com/sofastack/sofa-rpc/blob/v5.7.6/core/common/src/main/resources/com/alipay/sofa/rpc/common/rpc-config-default.json)；[5.8.0 defaults](https://github.com/sofastack/sofa-rpc/blob/v5.8.0/core/common/src/main/resources/com/alipay/sofa/rpc/common/rpc-config-default.json) | 固定 tag 浅克隆读取 |
| BOLT 同一连接按 request id 关联并发 future | [sofa-bolt v1.5.9 Connection](https://github.com/sofastack/sofa-bolt/blob/v1.5.9/src/main/java/com/alipay/remoting/Connection.java)；[BaseRemoting](https://github.com/sofastack/sofa-bolt/blob/v1.5.9/src/main/java/com/alipay/remoting/BaseRemoting.java) | 固定 tag 浅克隆读取 |
| 相关聚焦包当前测试通过 | `go test ./internal/boltclient ./internal/core/invoke ./internal/mcp` | 前序评审同一工作树运行 |
