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
- 使用泛化调用，不需要把业务 DTO 放进调用端 classpath。
- 接收 JSON 参数，并把复杂对象转换成 `GenericObject`。
- 把 CLI 拆成稳定的 launcher 和按版本隔离的 SOFARPC runtime。
- 支持显式 `--sofa-rpc-version`、自动版本推断、runtime 自动下载和本地缓存。
- 支持从当前项目或 `~/.config/sofa-rpcctl/` 自动发现 `rpcctl-manifest.yaml`。
- 支持通过 `rpcctl context` 维护可复用的全局 context/profile。
- 支持从已有 `config/rpcctl.yaml` 和 `config/metadata.yaml` 生成 manifest。
- 支持生成发布资产和 bootstrap installer，不需要复制源码目录就能安装。

## 命令

- `invoke`：完整形式的 RPC 调用。
- `call`：`invoke` 的短语法。
- `list`：从 metadata 或 manifest 列出服务。
- `describe`：查看单个服务的 metadata。
- `context`：管理 `~/.config/sofa-rpcctl/contexts.yaml` 里的全局 context/profile。
- `manifest generate|init`：从现有配置生成 `rpcctl-manifest.yaml`，或者初始化一个骨架。

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

默认下载基地址是：

```text
https://github.com/hex1n/sofa-rpcctl/releases/download/v<version>
```

如果你是离线环境，也可以把它指到本地目录或 `file://` URL。

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
./sofa-rpcctl-0.1.0/install-from-archive.sh /path/to/sofa-rpcctl-0.1.0.tar.gz
```

或者：

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh https://example.com/sofa-rpcctl-0.1.0.tar.gz
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

## 当前范围

已实现：

- `invoke`、`call`、`list`、`describe`、`context`、`manifest`
- `direct` 和 `registry` 两种目标模式
- manifest 和全局 context 自动发现
- runtime 版本隔离
- runtime 自动下载与缓存
- release asset 生成和 bootstrap installer
- 基于 metadata 的写操作确认
- 带 hint 的结构化 JSON 错误输出

未实现：

- 生产环境 gateway 模式
- 仅靠 SOFARPC 做通用 service/method 自动发现

第二条不是实现偷懒，而是结构限制。SOFARPC 没有提供一个对所有项目都稳定可用的通用方法目录，所以 `invoke` 可以在没有 metadata 的情况下工作，但 `list` 和 `describe` 仍然需要 manifest 或 metadata catalog。
