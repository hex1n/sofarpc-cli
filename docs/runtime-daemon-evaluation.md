**Question**: 是否应该把 `rpcctl` 改成常驻 runtime / daemon 模式，以及最合适的落地方式是什么？
**Depth**: Deep
**Key finding**: 值得做，但不该做成“全局单 daemon”。最稳妥的方案是保留 launcher，新增按 `(sofaRpcVersion + stub-path 集合)` 分片的 runtime daemon，并在 daemon 内缓存 `GenericService`。
**Open questions**: 3 — 见文末

## 结论

当前实现每次调用都会新起一个 runtime JVM，然后在 runtime 里重新 `refer()` 一个 SOFA consumer。这对偶发排障是合理的，但对高频调用会重复支付 JVM 启动、类加载、SOFA client 初始化、注册中心订阅的成本。

真正的阻碍不是“能不能常驻”，而是当前 classpath 不是固定的：

- launcher 会按调用参数决定是否走 `-jar` 还是 `-cp`；只要有 `--stub-path`，runtime 进程的 classpath 就会包含这些路径，见 `ProcessRuntimeInvoker.java:109-123`
- DTO 物化也依赖这些本地类是否在 classpath 中；`InvocationPayloads` 只有在 `preferLocalBeans` 为真且类本地可解析时才会转本地 bean，见 `InvocationPayloads.java:124-125`

所以最合适的设计不是单一 daemon，而是：

1. launcher 仍负责参数解析、manifest/context/version 决策
2. launcher 根据 `runtime key` 选择或启动 daemon
3. daemon 内部复用 JVM、类加载结果、以及 `GenericService` 引用
4. 不同 `sofaRpcVersion` 或不同 `stub-path` 集合使用不同 daemon，继续保持隔离

## 当前链路

```text
rpcctl CLI
  -> InvokeCommand 组装 RuntimeInvocationRequest
  -> ProcessRuntimeInvoker 启动新 JVM
  -> RuntimeMain 解码请求
  -> SofaRpcInvoker.resolve payload + buildConsumer().refer()
  -> $invoke / $genericInvoke
  -> JSON 输出回 launcher
```

证据：

- `InvokeCommand` 在 launcher 里组装 `RuntimeInvocationRequest` 并调用 `runtimeInvoker.invoke(...)`，见 `RpcCtlApplication.java:368-415`
- `ProcessRuntimeInvoker` 每次都 `new ProcessBuilder(...).start()`，见 `ProcessRuntimeInvoker.java:30-45`
- runtime 入口 `RuntimeMain` 只支持单次 `invoke`，执行后 `System.exit(...)`，见 `RuntimeMain.java:17-38`
- `SofaRpcInvoker` 每次调用都会 `buildConsumer(...).refer()`，见 `SofaRpcInvoker.java:22-31`

## 为什么它现在是短进程

这个仓库本来就把 launcher 和 runtime 刻意拆开了。

- README 明确写了 “launcher 和 runtime 是刻意拆开的”，目的是做版本隔离，见 `README.md:320-339`
- `RuntimeLocator` 也是按 `sofa-rpc` 版本查找独立 jar，见 `RuntimeLocator.java:17-45`
- `ProcessRuntimeInvoker` 在 `--stub-path` 存在时，改走 `-cp runtimeJar:stubPaths`，见 `ProcessRuntimeInvoker.java:113-116`

这说明现有设计的核心目标是隔离：

- SOFARPC 版本隔离
- 业务 stub / DTO classpath 隔离
- 进程级资源与静态状态隔离

daemon 方案如果破坏这三个目标，就会和仓库当前方向冲突。

## 主要收益

### 1. 去掉每次新起 JVM 的固定成本

当前每次调用都会：

- 编码请求，见 `ProcessRuntimeInvoker.java:39-40`
- 启动新进程，见 `ProcessRuntimeInvoker.java:42-45`
- 等待子进程输出 JSON，见 `ProcessRuntimeInvoker.java:54-73`

常驻 daemon 后，这部分可以变成一次握手 + 多次请求。

### 2. 复用 SOFA consumer，比复用 JVM 更值钱

当前 `SofaRpcInvoker` 每次都重新创建 `ConsumerConfig` 并 `refer()`，见 `SofaRpcInvoker.java:91-120` 与 `SofaRpcInvoker.java:27-31`。如果 registry 模式下 `refer()` 会触发订阅、地址解析、连接建立，那么这部分往往比 JVM 启动更贵。

因此，daemon 真正的价值不只是“少起一个进程”，而是：

- 缓存 `GenericService`
- 复用 direct 连接
- 复用 registry 订阅结果

## 主要约束

### 1. `--stub-path` 使 classpath 按请求变化

这是最大约束。

- launcher 在有 `--stub-path` 时，会把 stub 路径拼到 classpath，见 `ProcessRuntimeInvoker.java:113-116`
- `InvocationPayloads` 会尝试本地 DTO bean 物化，见 `InvocationPayloads.java:124-125`

如果只起一个全局 daemon，就必须二选一：

- 要么 runtime 启动时把所有可能 stub 都塞进 classpath。这个会把隔离完全打烂。
- 要么 runtime 里做动态 classloader。这个复杂度高，而且要处理类冲突与卸载。

所以更合理的是：daemon key 必须包含 `stub-path` 集合。

### 2. 当前 runtime 没有“服务循环”协议

`RuntimeMain` 现在只认两种参数形式：

- `invoke <base64-request>`
- `invoke --request-file <file>`

见 `RuntimeMain.java:20-25` 与 `RuntimeMain.java:42-53`。

也就是说，当前 runtime 不是服务器，只是一次性 worker。要 daemon 化，至少要新增一个 `serve` 子命令和对应 IPC 协议。

### 3. 当前代码没有显式 consumer 回收路径

在仓库代码里，我只看到 `refer()`，见 `SofaRpcInvoker.java:31`；没有看到和 SOFA consumer 生命周期对应的 `unrefer` / `destroy` / shutdown 逻辑。仓库内搜索 `unrefer|unRefer|destroy|shutdown` 没有命中 runtime 中的 consumer 清理代码。

这意味着当前实现实际上依赖“进程退出即清理”。短进程模式下这没问题；daemon 模式下就必须显式设计：

- consumer cache 的 key
- cache eviction
- daemon 退出时的资源释放

这一点是 daemon 方案的主要新增风险。

## 备选方案

### 方案 A: 仅保留现状

优点：

- 实现最简单
- 版本和 classpath 隔离已经成立
- 出错后依赖进程退出自动清理

缺点：

- 高频调用场景会持续支付冷启动成本
- registry/direct 连接无法复用
- 批量调用体验差

适合：

- 以排障、smoke check 为主
- 调用频率低

### 方案 B: 单全局 daemon

优点：

- 形式上最简单
- CLI 体验最好理解

缺点：

- 与 `--stub-path` 的 classpath 隔离冲突
- 多版本 runtime 隔离也会被削弱
- 更容易出现类冲突与静态状态污染

结论：

不建议。它和当前仓库的核心隔离模型冲突。

### 方案 C: 按 runtime key 分片的 daemon 池

建议的 key：

- `sofaRpcVersion`
- `runtimeJar` 绝对路径
- `javaBinary` 路径
- 归一化后的 `stub-path` 列表

环境目标如 `directUrl` / `registryAddress` 不应进 daemon key，因为这些更适合在 daemon 内部作为 `GenericService` cache key。

优点：

- 保留版本隔离和 stub 隔离
- 显著降低高频调用成本
- 复用 `GenericService` 成为可能

缺点：

- 需要 daemon 管理协议
- 需要 cache 生命周期管理
- 多个项目 / 多组 stub 时会出现多个后台进程

结论：

这是最稳妥、和当前架构最一致的方案。

### 方案 D: 单 daemon + 动态 classloader

优点：

- 理论上后台进程数量更少

缺点：

- 需要把 `InvocationPayloads`、DTO 解析、SOFA consumer 构造都搬进可切换 classloader 的模型
- 需要非常小心线程上下文类加载器和类卸载
- 一旦 library 内部用了全局静态缓存，类隔离可能仍然失效

结论：

复杂度明显高于收益，不建议作为第一阶段。

## 推荐改造路径

### 阶段 1: 先把 runtime 变成可循环服务

新增 `RuntimeMain serve`：

- stdin 读一行请求
- stdout 回一行结果
- 请求体继续沿用 `RuntimeInvocationRequest` JSON / base64
- 每个请求带 `requestId`
- 输出 `RuntimeInvocationResult`

原因：

- 你已经有稳定的请求/响应模型，见 `RuntimeInvocationRequest.java` 与 `RuntimeInvocationResult.java`
- 先复用现有模型，能避免同时重写 transport 和业务逻辑

### 阶段 2: 新增 `DaemonRuntimeInvoker`

把 launcher 里的 runtime 调用抽象成接口：

- `ProcessRuntimeInvoker`
- `DaemonRuntimeInvoker`

launcher 默认策略可以是：

- 未开启 daemon 时，继续走短进程
- 传 `--daemon` 或配置开启时，按 key 连接 / 启动 daemon
- daemon 不可用时回退到短进程

这样可以保留现有稳定路径，降低切换风险。

### 阶段 3: daemon 内缓存 `GenericService`

建议缓存 key：

- mode
- directUrl 或 registryAddress + registryProtocol
- protocol
- serialization
- timeoutMs
- serviceName
- uniqueId

理由：

- 这些字段正是当前 `ConsumerConfig` 构建所依赖的输入，见 `SofaRpcInvoker.java:91-120`

### 阶段 4: 加入 daemon 管理命令

建议新增：

- `rpcctl daemon status`
- `rpcctl daemon stop`
- `rpcctl daemon gc`

至少要能看到：

- 每个 daemon 的 key
- PID
- 启动时间
- 命中次数
- consumer cache 大小

## 最小可行版本

如果你想先验证收益，而不是一次性做完整架构，最小版本应该是：

1. daemon key 只支持 `sofaRpcVersion + stub-path`
2. IPC 先用本地 TCP `127.0.0.1` 随机端口或本地 socket 文件
3. daemon 先只复用 JVM，不做 `GenericService` cache
4. 指标对比首呼和连续 10 次调用耗时

如果这个版本都没有明显收益，再继续做 consumer cache 的价值就值得重估。

但从当前代码看，`refer()` 每次重建的成本很可能已经足以让 cache 有价值。

## 不建议的做法

- 不建议删除 `ProcessRuntimeInvoker`
- 不建议一开始就把默认路径切成 daemon-only
- 不建议做“一个 daemon 吃所有版本和所有 stub-path”
- 不建议先做动态 classloader

这些方案要么风险太高，要么和当前仓库的隔离模型不一致。

## 最弱结论

我没有在本仓库里验证 SOFA consumer 是否提供标准 `unrefer` / `destroy` API，也没有跑基准证明 `refer()` 在你的真实环境里占比多少。因此，“consumer cache 一定显著提速”这件事是高概率判断，不是已测量结论。

## Open Questions

1. SOFA `ConsumerConfig.refer()` 返回对象的推荐释放方式是什么？这需要查所用版本的官方 API。
2. 你们最常见的高频调用场景里，`stub-path` 组合是否稳定？如果非常稳定，daemon 池会更值。
3. 你是否接受后台常驻进程和额外管理命令？如果不接受，就只能做“会话级 worker”而不是 daemon。

## Source Audit

| Claim | Source | How obtained |
|-------|--------|-------------|
| launcher 每次调用都会组装 `RuntimeInvocationRequest` 并进入 `runtimeInvoker.invoke(...)` | `rpcctl-launcher/src/main/java/com/hex1n/sofarpcctl/RpcCtlApplication.java:368-415` | Fetched in this session |
| 当前 runtime 调用是通过 `ProcessBuilder` 新起 JVM | `rpcctl-launcher/src/main/java/com/hex1n/sofarpcctl/ProcessRuntimeInvoker.java:30-45` | Fetched in this session |
| `--stub-path` 会改变 runtime 启动 classpath | `rpcctl-launcher/src/main/java/com/hex1n/sofarpcctl/ProcessRuntimeInvoker.java:109-123` | Fetched in this session |
| runtime 入口当前只支持一次性 `invoke` 形式 | `rpcctl-runtime-sofa/src/main/java/com/hex1n/sofarpcctl/RuntimeMain.java:17-53` | Fetched in this session |
| 每次调用都会 `buildConsumer(...).refer()` | `rpcctl-runtime-sofa/src/main/java/com/hex1n/sofarpcctl/SofaRpcInvoker.java:18-31` | Fetched in this session |
| `ConsumerConfig` 的关键输入是 mode/target/protocol/serialization/timeout/service/uniqueId | `rpcctl-runtime-sofa/src/main/java/com/hex1n/sofarpcctl/SofaRpcInvoker.java:86-120` | Fetched in this session |
| 本地 DTO bean 物化依赖类可在当前 classpath 中解析 | `rpcctl-runtime-sofa/src/main/java/com/hex1n/sofarpcctl/InvocationPayloads.java:121-129` | Fetched in this session |
| 仓库设计明确把 launcher 和 runtime 分离以做 runtime 隔离 | `README.md:332-339` | Fetched in this session |
