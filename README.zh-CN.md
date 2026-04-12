# sofa-rpcctl

[English](./README.md)

`sofa-rpcctl` 是一个可移植的 CLI，用来在 terminal 中直接调用 SOFABoot / SOFARPC 服务，不依赖业务接口 jar。

这个项目遵循四个约束：

1. 必须能跨不同 SOFABoot 项目复用。
2. 必须直接使用原生 SOFARPC，而不是逼每个团队维护第二套 REST 接口。
3. 必须诚实面对版本兼容问题，不能假装一个客户端 runtime 能覆盖所有 provider。
4. 必须尽量保留 `curl` 式体验：目标地址 inline 传入时应当立刻可用，更智能的行为则来自可选的项目或用户元数据。

## 它能做什么

- 通过 `directUrl` 或注册中心调用 SOFARPC 服务。
- 直接使用原生 SOFARPC 调用，不需要把业务 DTO 放进调用端 classpath。
- 支持三种 payload 形态：`raw` 适合标量 / `Map` / `List`，`generic` 适合显式 `GenericObject` 兼容报文，`schema` 适合由 metadata 提供方法签名的场景。
- 支持通过 `--stub-path` 进入 stub-aware 调用，让本地业务类参与复杂 DTO 序列化并走 `$invoke`。
- 把 CLI 拆成稳定的 launcher 和按版本隔离的 SOFARPC runtime。
- 支持显式 `--sofa-rpc-version`、自动版本推断、runtime 自动下载和本地缓存。
- 支持从当前项目或 `~/.config/sofa-rpcctl/` 自动发现 `rpcctl-manifest.yaml`。
- 支持通过 `rpcctl context` 维护可复用的全局 context/profile。
- 支持从已有 `config/rpcctl.yaml` 和 `config/metadata.yaml` 生成 manifest。
- 支持在生成 manifest 时保留 Java 重载方法签名；如果仅靠 metadata 无法唯一定位重载，调用时会明确要求传 `--types`。
- 输出结构化诊断字段，比如 `payloadMode`、`paramTypeSource`、`invokeStyle`、`errorPhase`、`retriable`、runtime 版本解析信息和 provider 可达性 hint。
- 支持生成发布资产和 bootstrap installer，不需要复制源码目录就能安装。

## 命令

- `invoke`：完整形式的 RPC 调用。
- `call`：`invoke` 的短语法。
- `doctor`：在真正调用前诊断 context 发现、runtime 解析和目标可达性。
- `list`：从 metadata 或 manifest 列出服务。
- `describe`：查看单个服务的 metadata。
- `context`：管理 `~/.config/sofa-rpcctl/contexts.yaml` 里的全局 context/profile。
- `manifest generate|init`：从现有配置生成 `rpcctl-manifest.yaml`，或者初始化一个骨架。
- `manifest generate` 也可以直接从本地接口 jar 或编译产物导入方法签名，包括重载方法。

## 快速开始

构建 launcher 和默认 runtime：

```bash
./scripts/build.sh
```

把 `runtime-manifests/sofa-rpc/` 下声明的全部 runtime 都构建出来：

```bash
./scripts/build-all-runtimes.sh
```

零配置直连调用：

```bash
./bin/rpcctl invoke \
  --direct-url bolt://127.0.0.1:12200 \
  --service com.example.UserService \
  --method getUser \
  --types java.lang.Long \
  --args '[123]'
```

同一个调用的短语法：

```bash
./bin/rpcctl call \
  com.example.UserService/getUser \
  '[123]' \
  --direct-url bolt://127.0.0.1:12200
```

通过注册中心调用：

```bash
./bin/rpcctl call \
  com.example.UserService/getUser \
  '[123]' \
  --registry zookeeper://127.0.0.1:2181
```

如果当前项目提供了 `rpcctl-manifest.yaml`，`rpcctl` 会自动补 `defaultEnv`、`uniqueId` 和方法参数类型：

```bash
./bin/rpcctl call com.example.UserService/getUser '[123]'
```

## 智能模式

`rpcctl` 有两种工作方式：

- transport 模式：像 `curl` 一样，直接传 `--direct-url` 或 `--registry` 就能调。
- metadata 模式：不是“猜”，而是读取项目或用户元数据，所以会更智能。

manifest 查找顺序：

1. `--manifest`
2. `RPCCTL_MANIFEST`
3. 从当前目录向上搜索 `rpcctl-manifest.yaml` 或 `rpcctl-manifest.yml`
4. `~/.config/sofa-rpcctl/rpcctl-manifest.yaml`

第 4 条就是“任意目录下也能用智能模式”的关键。只要做一次用户级安装，后续不在项目目录里也能走 manifest 驱动调用。

## Payload 模式

`rpcctl` 不会假装自己能从任意 JSON 自动推断任意 DTO 对象图。现在支持的是三种明确形态：

- `raw`：最适合标量参数，以及方法签名本来就是 `Map`、`List`、`Set`、数组、基础类型的场景。这是最接近 `curl` 的模式。
- `generic`：最适合你已经知道 DTO 声明类型，并且愿意显式提供 `@type` / `@value` 这类提示的场景。
- `schema`：最适合业务 DTO，由 `rpcctl-manifest.yaml` 或其他 metadata 提供方法签名。

调用结果里会通过 `payloadMode`、`paramTypeSource`、`invokeStyle` 告诉你这次实际走的是哪条路径。

`raw` 例子：

```bash
rpcctl call \
  com.example.PayloadService/submit \
  '[{"requestId":"payload-01","meta":{"channel":"smoke"},"lines":[{"sku":"sku-apple","quantity":2}]}]' \
  --registry zookeeper://127.0.0.1:2181 \
  --types java.util.Map \
  --unique-id payload-service
```

带显式类型提示的 `generic` 例子：

```bash
rpcctl invoke \
  --direct-url bolt://127.0.0.1:12200 \
  --service com.example.OrderService \
  --method submit \
  --types com.example.OrderRequest \
  --args '[{"@type":"com.example.OrderRequest","customer":{"@type":"com.example.Customer","name":"alice"}}]'
```

stub-aware DTO 例子：

```bash
rpcctl call \
  com.example.OrderService/submit \
  '[{"requestId":"order-01","customer":{"name":"alice","address":{"city":"Shanghai"}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3}]}]' \
  --direct-url bolt://127.0.0.1:12241 \
  --manifest ./rpcctl-manifest.yaml \
  --stub-path ./target/classes \
  --confirm
```

需要明确的边界：

- 官方 generic 路径只定义在 `bolt + hessian2` 上。
- 没有 schema 或业务 stub 时，嵌套 DTO 对象图不保证稳定反序列化。
- 如果只是临时 smoke check，优先用 `raw` 的 `Map` / `List` 报文，或者直接用服务暴露的 REST binding。

## Registry 注意事项

注册中心模式是否成功，不只取决于“能不能从 zk 找到 provider”，还取决于 provider 发布出来的地址是否真的可达。如果 provider 发布的是 `localhost`、容器内 IP，或者另一张本机不可达的地址，那么服务发现虽然成功，真正建链时还是会失败。

所以在本地或容器化测试里，provider 侧最好显式设置 `ServerConfig#setVirtualHost(...)`，必要时再配 `ServerConfig#setVirtualPort(...)`，让注册中心里发布的是调用端真正能连到的地址。现在 `rpcctl` 会尽量区分：

- 注册中心里根本没找到 provider
- 找到了 provider，但 provider 地址不可达
- 方法签名不匹配
- DTO / payload 不兼容导致的反序列化失败

失败响应还会携带稳定的机器可读字段：

- `errorCode`：稳定错误类别，比如 `RPC_PROVIDER_UNREACHABLE`、`RPC_METHOD_NOT_FOUND`
- `errorPhase`：失败发生在哪个阶段，比如 `discovery`、`connect`、`invoke`、`serialize`、`deserialize`
- `retriable`：同一份请求不改 payload 再重试，是否有机会成功
- `diagnostics`：结构化底层诊断，比如 `targetMode`、`configuredTarget`、`providerAddress`、`invokeStyle`

## Doctor

可以在真正发起调用前先跑一次诊断：

```bash
rpcctl doctor \
  --direct-url bolt://127.0.0.1:12200 \
  --sofa-rpc-version 5.4.0
```

它会输出：

- config / manifest / context 是从哪里发现的
- 最终解析出的 SOFARPC 版本，以及是否走了 fallback
- 本地 runtime jar 是否能解析到，是否会走自动下载
- direct 或 registry 的 TCP 可达性

## Context

context 是保存在下面这个文件里的全局 profile：

```text
~/.config/sofa-rpcctl/contexts.yaml
```

它可以固定默认 manifest、env、registry、direct target、runtime 下载地址和 SOFARPC 版本，因此不依赖当前工作目录。

创建或更新 context：

```bash
rpcctl context set test \
  --manifest ~/.config/sofa-rpcctl/rpcctl-manifest.yaml \
  --stub-path ~/workspace/demo/target/classes \
  --runtime-base-url https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0 \
  --current
```

列出 context：

```bash
rpcctl context list
```

查看当前 context：

```bash
rpcctl context show
```

切换 context：

```bash
rpcctl context use test
```

删除一个 context：

```bash
rpcctl context delete test
```

只要当前 context 已激活，下面这条命令在任意目录下都能工作：

```bash
rpcctl call com.example.UserService/getUser '[123]'
```

## Manifest 生成

从现有 `config/rpcctl.yaml` 和 `config/metadata.yaml` 生成 `rpcctl-manifest.yaml`：

```bash
rpcctl manifest generate
```

输出到别的路径并允许覆盖：

```bash
rpcctl manifest generate \
  --output /tmp/rpcctl-manifest.yaml \
  --force
```

直接从本地编译产物导入 service schema：

```bash
rpcctl manifest generate \
  --output rpcctl-manifest.yaml \
  --force \
  --stub-path ./target/classes \
  --service-class com.example.OrderService \
  --service-unique-id com.example.OrderService=order-service
```

如果导入的接口存在重载方法，`manifest generate` 会把它们保留在 `overloads:` 下。调用时，`rpcctl` 会优先按 `--types` 选重载；如果没传 `--types`，只有在参数个数可以唯一定位时才会自动选择。

初始化一个带根级默认值的 manifest：

```bash
rpcctl manifest init \
  --default-env test-zk \
  --sofa-rpc-version 5.4.0 \
  --protocol bolt \
  --serialization hessian2 \
  --timeout-ms 3000
```

最小示例：

```yaml
defaultEnv: test-zk
sofaRpcVersion: 5.4.0
protocol: bolt
serialization: hessian2
timeoutMs: 3000
envs:
  test-zk:
    mode: registry
    registryProtocol: zookeeper
    registryAddress: 127.0.0.1:2181
services:
  com.example.UserService:
    uniqueId: user-service
    methods:
      getUser:
        risk: read
        paramTypes:
          - java.lang.Long
```

## Runtime 模型

launcher 和 SOFARPC client runtime 是刻意拆开的：

- `rpcctl-launcher.jar`：稳定 CLI 外壳。
- `rpcctl-runtime-sofa-<version>.jar`：按版本隔离的 SOFARPC client runtime。

runtime 选择顺序：

1. 显式 `--sofa-rpc-version`
2. 当前选中的 context
3. manifest 或 config
4. 当前项目里的版本探测
5. 本地缓存或内置 runtimes
6. runtime 自动下载

这样不同项目就不会被强行绑到同一个 SOFARPC 版本上。

### Runtime 自动下载

如果本地缺少所需 runtime，launcher 会把它下载到缓存目录：

```text
~/.cache/sofa-rpcctl/runtimes/sofa-rpc/<version>/
```

相关环境变量：

- `RPCCTL_RUNTIME_BASE_URL`
- `RPCCTL_RUNTIME_HOME`
- `RPCCTL_RUNTIME_CACHE_DIR`
- `RPCCTL_RUNTIME_AUTO_DOWNLOAD`
- `RPCCTL_DEBUG_RUNTIME=1`
- `RPCCTL_RUNTIME_VERBOSE=1`

默认下载基地址是：

```text
https://github.com/hex1n/sofa-rpcctl/releases/download/v<version>
```

如果你是离线环境，也可以把它指到本地目录或 `file://` URL。

现在 auto-download 失败时，`rpcctl` 会把前几个候选 URL 和失败原因一起带出来；如果要看逐个候选源的下载日志，可以设 `RPCCTL_RUNTIME_VERBOSE=1`。

## 兼容性策略

`rpcctl` 的关键策略是显式运行时隔离：

- 版本解析顺序为：显式 `--sofa-rpc-version`、context、manifest/config、当前项目探测、运行时缓存/本地资源、自动下载。
- 不假设一个 runtime 可以兼容所有 SOFARPC 版本；不同服务栈建议使用独立 `rpcctl-runtime-sofa-<version>.jar`。
- 复杂 DTO 时优先默认到官方文档中的兼容组合（`bolt` + `hessian2`）并保证签名类型可见。
- 调用输出里会带版本诊断字段，比如 `resolvedSofaRpcVersion`、`sofaRpcVersionSource`，以及在 fallback 或支持矩阵不匹配时出现的 `sofaRpcVersionFallback`、`supportedSofaRpcVersions`。

SOFAStack 的配置文档里对应关键字段：

- Consumer 配置中的 `protocol`、`serialization`、`generic`、`directUrl`、`registry`。
- ServerConfig 的 `virtualHost`/`virtualPort` 用于注册中心地址修正。

官方入口：

- https://www.sofastack.tech/en/projects/sofa-rpc/configuration-common/

## 配置

推荐用 `rpcctl-manifest.yaml` 表达项目级元数据。

老的配置形式仍然可用：

- `config/rpcctl.yaml`
- `config/metadata.yaml`

配置查找顺序：

1. `--config`
2. `RPCCTL_CONFIG`
3. `./config/rpcctl.yaml`
4. `~/.config/sofa-rpcctl/rpcctl.yaml`

contexts 查找顺序：

1. `RPCCTL_CONTEXTS`
2. `~/.config/sofa-rpcctl/contexts.yaml`

相对路径的 `metadataPath` 和 manifest 引用，都是相对于声明它们的文件本身解析，而不是相对于当前 shell 目录。

## 安装

从源码直接安装：

```bash
./scripts/install.sh
```

构建一个可移植发布包：

```bash
./scripts/dist.sh
```

会生成：

```text
dist/sofa-rpcctl-0.1.0.tar.gz
```

从解压后的发布包安装：

```bash
tar -xzf dist/sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install.sh
```

从本地归档或 URL 安装：

```bash
tar -xzf sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install-from-archive.sh /path/to/sofa-rpcctl-0.1.0.tar.gz
```

或者：

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh https://example.com/sofa-rpcctl-0.1.0.tar.gz
```

## 新电脑安装与使用

一台全新的机器通常只需要满足这几个条件：

- 本机有可用的 `java`
- 能访问你发布的 GitHub Release，或者你手里有拷过去的发布包
- 能访问目标 SOFARPC 的 provider 或 registry

在线安装：

```bash
curl -fsSL \
  https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0/get-rpcctl.sh \
  | bash -s -- 0.1.0
```

这个 bootstrap installer 在解压前会先用发布页里的 `checksums.txt` 校验归档；只有你明确要跳过校验时，才建议临时设 `RPCCTL_SKIP_CHECKSUM=1`。

如果 `~/.local/bin` 还没进 `PATH`：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

检查安装结果：

```bash
rpcctl --help
```

如果是离线场景，就从拷过去的发布包安装：

```bash
tar -xzf sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install.sh
export PATH="$HOME/.local/bin:$PATH"
```

如果你使用 `install-from-archive.sh`，并且归档旁边存在 `.sha256` 或 `checksums.txt`，它会优先校验；找不到校验文件时会打印 warning 后继续。

新机器上的第一条直连调用：

```bash
rpcctl call \
  com.foo.UserService/getUser \
  '[123]' \
  --direct-url bolt://test-provider-host:12200 \
  --types java.lang.Long \
  --unique-id user-service \
  --sofa-rpc-version 5.4.0
```

通过注册中心调用：

```bash
rpcctl call \
  com.foo.UserService/getUser \
  '[123]' \
  --registry zookeeper://test-zk-host:2181 \
  --types java.lang.Long \
  --unique-id user-service \
  --sofa-rpc-version 5.4.0
```

`rpcctl-manifest.yaml` 对调用本身不是强制要求。没有 manifest 也能调，但这时你要自己把目标地址写清楚，而且大多数情况下还要自己补 `--types`、`--unique-id`，有时还要补 `--sofa-rpc-version`。

如果你希望它在任意目录下都更智能，可以做一次用户级初始化：

```bash
mkdir -p ~/.config/sofa-rpcctl
cp rpcctl-manifest.yaml ~/.config/sofa-rpcctl/
rpcctl context set test \
  --manifest ~/.config/sofa-rpcctl/rpcctl-manifest.yaml \
  --current
```

做完之后，命令就可以缩短成：

```bash
rpcctl call com.foo.UserService/getUser '[123]'
```

## 发布资产

生成一套可发布的 release assets：

```bash
./scripts/release.sh
```

会产出：

- `dist/release-assets/sofa-rpcctl-<version>.tar.gz`
- `dist/release-assets/rpcctl-runtime-sofa-<version>.jar`
- `dist/release-assets/get-rpcctl.sh`
- `dist/release-assets/checksums.txt`

把这些文件发到 GitHub Release 之后，安装命令可以压缩成：

```bash
curl -fsSL \
  https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0/get-rpcctl.sh \
  | bash -s -- 0.1.0
```

仓库里也附带了 GitHub Actions workflow，给 `v*` tag 自动构建并发布这些资产。

## E2E Smoke

仓库里现在附带了一套可重复执行的 smoke 脚本：

```bash
./scripts/e2e-smoke.sh
```

依赖：

- `java`
- `javac`
- 一个兼容 Docker 的运行环境，比如 Docker Desktop 或 OrbStack

脚本会做这几件事：

1. 构建指定版本的 SOFARPC runtime
2. 编译 `e2e/fixtures/src` 里的本地 provider 样例
3. 验证一条 direct 模式的 `UserService#getUser(Long)` 调用
4. 验证一条基于导入 manifest schema 的 stub-aware DTO 调用
5. 启动本地 ZooKeeper，再验证一条 registry 模式的复杂 `Map` payload 调用

可选环境变量：

- `RPCCTL_E2E_DIRECT_PORT`
- `RPCCTL_E2E_REGISTRY_PORT`
- `RPCCTL_E2E_ZK_PORT`
- `RPCCTL_E2E_ZK_CONTAINER`

## 当前范围

已实现：

- `invoke`、`call`、`list`、`describe`、`context`、`manifest`
- `direct` 和 `registry` 两种目标模式
- `raw`、`generic` 和 metadata 驱动的 `schema` payload 分类
- 通过 `--stub-path` 进行 stub-aware DTO 调用
- manifest 和全局 context 自动发现
- 从本地 jar / 编译产物导入 manifest schema
- runtime 版本隔离
- runtime 自动下载与缓存
- release asset 生成和 bootstrap installer
- 基于 metadata 的写操作确认
- 带 hint 的结构化 JSON 错误输出
- 仓库内可重复执行的 direct / registry smoke 样例 `./scripts/e2e-smoke.sh`

未实现：

- 生产环境 gateway 模式
- 仅靠 SOFARPC 做通用 service/method 自动发现

第二条不是实现偷懒，而是结构限制。SOFARPC 没有提供一个对所有项目都稳定可用的通用方法目录，所以 `invoke` 可以在没有 metadata 的情况下工作，但 `list` 和 `describe` 仍然需要 manifest 或 metadata catalog。
