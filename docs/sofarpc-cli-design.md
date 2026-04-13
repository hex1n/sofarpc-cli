**Depth**: Standard
**Input sources**: Official SOFA RPC docs, official SOFAStack GitHub org, user prompt

## TL;DR

如果让我从零实现一个调用 SOFARPC 的 CLI，我不会做“纯 Go / 纯 Python / 纯 Java 单体”。

最佳设计是：

- **Go launcher** 负责 CLI 体验、配置发现、安装分发、daemon 管理
- **Java runtime workers** 负责真正的 SOFARPC 调用
- runtime 按 **`sofaRpcVersion + classpath hash`** 分片常驻
- 协议上先只做 **direct + registry**、**Bolt + Hessian2 generic path**，把不确定性压到最小

原因很简单：SOFARPC 官方能力面和文档中心都在 Java 侧，generic 调用也明确绑定 `Bolt + Hessian2`，而 CLI 的跨平台分发、后台进程管理、并发和 UX 更适合 Go 来做。

## What To Build

### P0 架构

| Priority | Change | Effort | Risk | Value |
|---|---|---:|---|---|
| P0 | 定义稳定的 Go CLI surface：`call` / `doctor` / `context` / `manifest` | 2-3d | 低 | 高 |
| P0 | 定义 Java runtime worker 协议：请求/响应 JSON + versioned runtime contract | 2d | 中 | 高 |
| P0 | 实现按 `sofaRpcVersion + classpath hash` 分片的 daemon 池 | 3-5d | 中 | 高 |
| P0 | 实现 direct / registry 两种目标模式 | 2-4d | 中 | 高 |
| P0 | 实现三种 payload 模式：`raw` / `generic` / `schema` | 4-6d | 中 | 高 |
| P1 | 增加 manifest 驱动的智能模式和方法签名导入 | 4-6d | 中 | 高 |
| P1 | 增加 structured diagnostics 与 `doctor` | 2-3d | 低 | 高 |
| P1 | 增加 runtime auto-download / plugin install | 3-4d | 中 | 中 |
| P2 | 增加 daemon metrics / cache 管理 / 回收策略 | 2-3d | 中 | 中 |
| Total | 首个可用版本 | 19-36d |  |  |

### 不要做

- 不要做“纯 Go 直连 SOFARPC”第一版
- 不要做“自动发现所有服务和方法”第一版
- 不要把所有 SOFA 版本塞进一个 runtime
- 不要把所有业务 jar 都塞进单个全局 daemon

## 问题定义

### 真实问题

不是“做一个命令行工具”。

真实问题是：

> 在多版本、多项目、多 DTO 依赖的 SOFARPC 环境里，给开发者一个**可重复、可诊断、低摩擦**的调用入口，用于调试、排障、冒烟验证和临时联调。

### Solved 的标准

- 任意一台新机器能在几分钟内完成安装和第一次调用
- 调用失败时，错误能区分“没 provider / provider 不可达 / 方法签名不匹配 / payload 不兼容”
- 不同项目的 SOFARPC 版本不互相污染
- 复杂 DTO 调用不要求用户手工拼 `GenericObject`
- 高频调用时不被 JVM 冷启动拖死

## Ground Truth

### ✅ verified

- SOFARPC 官方 generic 调用文档明确写了：generic 调用当前只支持 **Bolt + Hessian2**  
  Source: https://www.sofastack.tech/en/projects/sofa-rpc/generic-invoke/

- 官方文档展示的调用入口是 Java `ConsumerConfig<GenericService>` / `GenericService.$invoke()` / `$genericInvoke()`  
  Source: https://www.sofastack.tech/en/projects/sofa-rpc/generic-invoke/

- 官方配置文档里，`ConsumerConfig` 有 `protocol`、`serialization`、`directUrl`、`registry`、`generic`、`timeout` 等关键配置，说明 direct / registry 是一等概念  
  Source: https://www.sofastack.tech/en/projects/sofa-rpc/configuration-common/

- SOFAStack 官方 GitHub 组织里，核心 `sofa-rpc` 仓库和相关基础设施都以 Java 为主；`sofa-rpc` README 也明确把它定义为 Java RPC framework  
  Sources: https://github.com/sofastack/sofa-rpc , https://github.com/sofastack

### ❓ inferred

- 复杂业务 DTO 在真实项目里通常需要本地 classpath 才能让“易用的 CLI”成立，否则用户只能自己构造 `GenericObject`
- 多版本共存是必须能力，不是 nice-to-have
- 高频场景下，consumer 建链和 JVM 启动都会成为明显成本

这些推断不是拍脑袋，而是由官方 Java-first API 形态和 generic 调用约束直接推出来的。

## 推荐架构

```text
Go CLI
  -> Config / manifest / context resolver
  -> Runtime manager
  -> Daemon pool selector
  -> JSON RPC over local socket / stdio

Java Runtime Worker (versioned)
  -> Consumer factory
  -> Consumer cache
  -> DTO/schema/generic payload adapter
  -> SOFARPC invoke
  -> Structured result + diagnostics
```

## 为什么是 Go launcher + Java runtime

### Go 负责什么

- 单文件分发
- 跨平台安装
- 命令行参数解析
- config/context/manifest 发现
- 后台 daemon 管理
- 统一 JSON 输出和错误码

Go 擅长“工具层”和“运维层”。

### Java 负责什么

- SOFARPC client
- `ConsumerConfig` / `RegistryConfig`
- `GenericService`
- Hessian2 generic invoke
- DTO classpath 兼容

Java 擅长“协议正确性”和“生态贴合度”。

### 为什么不做纯 Java 单体

可以做，但不是最佳。

缺点：

- 安装与分发体验通常比 Go 差
- 后台进程管理和多 runtime 生命周期做起来更重
- 如果想做 bootstrap installer、runtime auto-download、daemon 池，Go 做控制面更顺手

### 为什么不做纯 Go

官方 GenericService 能力、配置模型和示例都在 Java 侧。第一版如果做纯 Go，就要自己补：

- Bolt/Hessian2 细节
- generic invoke 语义
- registry 兼容
- DTO 与 classpath 替代方案

这相当于你在做一个“SOFARPC 兼容客户端”，而不是一个 CLI。

## 运行模型

### 1. Control Plane / Data Plane 分离

- **Control Plane**: Go launcher
- **Data Plane**: Java runtime workers

这样 CLI 表面稳定，但执行内核可以按版本替换。

### 2. Versioned runtime

每个 runtime 是独立 artifact：

- `runtime-sofa-5.4.x.jar`
- `runtime-sofa-5.7.x.jar`
- `runtime-sofa-5.14.x.jar`

不要试图让一个 runtime 兼容所有版本。

### 3. Daemon key

daemon key 必须包含：

- `sofaRpcVersion`
- `runtime artifact digest`
- `stub/classpath digest`
- `java major version`

这是最重要的隔离边界。

### 4. Consumer cache key

在 daemon 内，consumer cache key 应该包含：

- mode
- directUrl 或 registryAddress
- registryProtocol
- protocol
- serialization
- serviceName
- uniqueId
- timeout

因为这些字段会决定 `ConsumerConfig` 的行为。

## CLI Surface

### 必须有

- `call`
- `doctor`
- `context set/list/use/show/delete`
- `manifest init/generate`

### `call`

两类入口：

- transport-first  
  适合一次性调用
- metadata-first  
  适合项目内智能模式

示例：

```bash
sofarpc call \
  --direct-url bolt://127.0.0.1:12200 \
  --service com.foo.UserService \
  --method getUser \
  --types java.lang.Long \
  --args '[123]'
```

或：

```bash
sofarpc call com.foo.UserService/getUser '[123]'
```

### `doctor`

必须回答这几个问题：

- config / manifest / context 是从哪里来的
- 最终选了哪个 runtime version
- runtime 是本地命中、下载命中还是缺失
- direct / registry 是否可达
- classpath 里是否存在高风险冲突 jar

## Payload 设计

第一版我会明确支持三种模式：

### `raw`

面向：

- 标量
- `Map/List`
- 无 schema 的快速 smoke check

优点：最稳定、最少魔法。

### `generic`

面向：

- 需要手工指定 `@type`
- 当前 classloader 没有目标 DTO 类

本质是对官方 `GenericObject` 模型做 JSON 投影。

### `schema`

面向：

- 有 manifest / stub / 本地接口
- 想让 CLI 自动把普通 JSON 转成业务 DTO

这应该是“体验最好”的模式，但它必须依赖 metadata 或本地 classpath，不应假装自己能从任意 JSON 猜出对象图。

## 配置模型

### 三层配置

1. CLI flags
2. project manifest
3. user contexts

优先级应当永远是：

`flags > context > manifest > defaults`

### 为什么需要 manifest

SOFARPC 并不天然提供“列出所有服务和方法”的稳定用户态目录。  
所以 CLI 的智能化必须依赖显式元数据。

manifest 应承载：

- default env
- sofaRpcVersion
- protocol / serialization / timeout
- env bindings
- service uniqueId
- method paramTypes
- overload metadata
- stub-path

## 错误模型

不要只返回一条字符串。

我会设计稳定错误码：

- `PROVIDER_NOT_FOUND`
- `PROVIDER_UNREACHABLE`
- `METHOD_NOT_FOUND`
- `SERIALIZATION_ERROR`
- `DESERIALIZATION_ERROR`
- `TIMEOUT_CONNECT`
- `TIMEOUT_INVOKE`
- `RUNTIME_MISSING`
- `RUNTIME_VERSION_UNSUPPORTED`
- `STUB_CLASSPATH_CONFLICT`

以及结构化字段：

- `phase`
- `targetMode`
- `configuredTarget`
- `resolvedTarget`
- `invokeStyle`
- `payloadMode`
- `retriable`
- `hint`

## 安装与发布

最佳分发方式：

- Go launcher 单二进制
- Java runtimes 作为按版本下载的插件包
- 首次缺失时自动下载或提示安装

不要把所有 runtime 打进一个大包。

## 风险与缓解

### 风险 1: 版本兼容地狱

缓解：

- runtime 按版本拆分
- 支持矩阵显式声明
- runtime 缺失时明确报错，不做隐式猜测

### 风险 2: classpath 污染

缓解：

- daemon 按 classpath digest 分片
- `doctor` 扫描高风险 jar
- 文档明确 `stub-path` 最小化原则

### 风险 3: 智能化过度

缓解：

- 默认优先 explicit flags
- manifest 驱动智能，而不是“猜”
- raw mode 永远保留

## Dissenting Path

如果你坚持“只要最快交付，不要 control plane / data plane 分离”，那我会给一个缩减版：

- 纯 Java CLI
- 单进程 one-shot
- 支持 direct / registry
- 支持 raw / generic
- 暂不做 daemon、auto-download、manifest import

这更快落地，但不是最佳长期设计。它牺牲了：

- 分发体验
- 多 runtime 管理
- 高频调用性能
- 运维可观测性

## Self-Check

这个方案最可能失败的地方，是我把“Java runtime 的必要性”看得太重。如果未来证明 SOFARPC wire-level + registry 兼容在 Go 中可稳定复现，那 control plane / data plane 分离的价值会下降。

但基于当前官方文档和生态现实，我仍然认为：**最佳设计不是纯语言选择问题，而是“Go 控制面 + Java 执行面”的职责分离问题。**
