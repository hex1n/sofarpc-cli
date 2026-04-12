# sofa-rpcctl

[English](./README.md)

`sofa-rpcctl` 是一个独立的命令行工具，用来从 terminal 直接调用 SOFABoot / SOFARPC 服务，不依赖业务接口 jar。

这个项目围绕三个约束设计：

1. 必须能在不同 SOFABoot 项目之间复用。
2. 必须直接使用原生 SOFARPC，而不是逼每个服务再维护一套 REST 接口。
3. 必须诚实面对 SOFARPC 在运行时无法普适发现全部方法元数据这个事实。

## 它能做什么

- 通过 `directUrl` 或注册中心调用 SOFARPC 服务。
- 使用泛化调用，不需要把业务 DTO 类放到调用端 classpath。
- 接收 JSON 参数，并把复杂对象转换成 `GenericObject`。
- 把可选元数据放在独立 YAML 文件里，用于 `list`、`describe` 和更安全的写操作确认。

## 当前范围

当前实现是一个可移植的 CLI MVP：

- 已实现：`invoke`、`list`、`describe`
- 已实现：`direct` 和 `registry` 两种环境模式
- 已实现：基于元数据的写操作确认
- 未实现：生产环境 gateway 模式
- 未实现：通用的运行时方法发现

最后这一点不是实现偷懒，而是结构限制。SOFARPC 本身没有提供一个对所有项目都可靠的通用方法目录，让独立客户端在运行时直接发现所有服务方法。因此元数据对 `invoke` 是可选的，但对 `list` / `describe` 是必需的。

## 构建

```bash
./scripts/build.sh
```

如果你需要和某个 provider 的 SOFARPC 版本保持一致，可以覆盖默认版本：

```bash
./scripts/build.sh 5.4.0
```

或者：

```bash
SOFA_RPC_VERSION=5.4.0 ./scripts/build.sh
```

## 运行

```bash
./bin/rpcctl invoke \
  --env local-direct \
  --service com.example.UserService \
  --method getUser \
  --types java.lang.Long \
  --args '[123]'
```

使用元数据补全参数类型：

```bash
./bin/rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method getUser \
  --args '[123]'
```

带显式类型标记的嵌套复杂对象：

```bash
./bin/rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method updateUser \
  --args '[
    {
      "@type": "com.example.UserUpdateRequest",
      "id": 123,
      "profile": {
        "@type": "com.example.UserProfile",
        "nickname": "neo"
      }
    }
  ]'
```

查看元数据中的服务列表：

```bash
./bin/rpcctl list
```

查看某个服务的元数据：

```bash
./bin/rpcctl describe --service com.example.UserService
```

## 安装

直接从源码安装：

```bash
./scripts/install.sh
```

安装后就可以在任意 terminal 目录中运行：

```bash
rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method getUser \
  --args '[123]'
```

## 发行包

构建一个不依赖源码目录的发布包：

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

也可以直接从本地归档或远端 URL 安装：

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh /path/to/sofa-rpcctl-0.1.0.tar.gz
```

或者：

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh https://example.com/sofa-rpcctl-0.1.0.tar.gz
```

## 配置

`config/rpcctl.yaml` 保存环境定义，`config/metadata.yaml` 是可选但推荐的元数据文件。

配置查找顺序：

1. `--config`
2. `RPCCTL_CONFIG`
3. `./config/rpcctl.yaml`
4. `~/.config/sofa-rpcctl/rpcctl.yaml`

如果 `metadataPath` 是相对路径，会相对于配置文件本身解析，而不是相对于当前 shell 工作目录。

## 分发模型

源码仓库本身不再是安装单元，真正的安装单元是生成出来的发布归档：

- `bin/rpcctl`
- `lib/sofa-rpcctl.jar`
- `share/sofa-rpcctl/*.yaml`
- `install.sh`
- `install-from-archive.sh`

这意味着你可以：

1. 构建一次
2. 只保留 `tar.gz`
3. 在另一台机器上安装，而不需要复制整个仓库

如果你想让它真的达到“像 curl 一样一行安装”，还需要把生成好的 `tar.gz` 放到一个可访问地址上。只要归档有了下载地址，安装链路就可以完全基于发布包，而不是基于源码目录。

环境模式：

- `direct`：使用 `directUrl`，例如 `bolt://127.0.0.1:12200`
- `registry`：使用 `registryProtocol` + `registryAddress`，或者直接写完整的 `registryAddress` URI

为了兼容更多项目，默认建议：

- protocol: `bolt`
- serialization: `hessian2`
- Java: `8`

## 数据模型规则

- `--types` 使用逗号分隔。
- `--args` 必须是 JSON 数组。
- 复杂对象会被转换成 SOFARPC 的 `GenericObject`。
- 如果嵌套字段不是普通 `Map`，建议显式写 `@type`。
- 数组类型可以写成 `java.lang.String[]`，不需要手写 JVM descriptor。

## 使用注意

- 写操作方法建议在元数据里标成 `risk: write` 或 `risk: dangerous`。
- `risk: write` 和 `risk: dangerous` 必须显式传 `--confirm`。
- 如果 provider 对版本敏感，重新构建时请把 `sofa-rpc.version` 对齐。

## 目录结构

```text
sofa-rpcctl/
  bin/rpcctl
  config/rpcctl.yaml
  config/metadata.yaml
  scripts/build.sh
  src/main/java/com/hex1n/sofarpcctl/...
```
