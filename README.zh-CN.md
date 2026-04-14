# sofarpc-cli

用于调用 SOFARPC 服务的 CLI。

结构（有意多语言，各自做自己最擅长的事）：

- **Go** —— CLI 控制面、daemon 生命周期、runtime 缓存。冷启动快、
  Windows 子进程语义干净、单文件二进制分发。
- **Java** —— 随目标版本对齐的 SOFARPC worker runtime，以及基于 Spoon 的 facade 索引器。

入口文档：

- 使用说明和命令参考：[docs/usage.zh-CN.md](./docs/usage.zh-CN.md)
- 设计文档：[docs/sofarpc-cli-design.md](./docs/sofarpc-cli-design.md)

## 运行流程

```mermaid
flowchart LR
    A[执行 `sofarpc <command>`] --> B["`internal/cli`: 解析参数与 manifest/context"]
    B --> C["`internal/runtime`: ResolveSpec 并生成 daemon key"]
    C --> D["`Manager.EnsureDaemon`: 启动/复用运行时 daemon"]
    D --> E["`Manager.Invoke`: 通过 TCP 与 Java runtime 通信"]
    B -->|`call` 命令| F["组装调用参数（service/method/args/target）"]
    F --> G{是否需要参数类型推断？}
    G -->|是| H["`DescribeService`: 走 `action=describe` 请求"]
    G -->|否| I["直接调用请求"]
    H --> E
    I --> E
    B -->|`describe` 命令| H
    E --> J{"`request.action`"}
    J -->|`describe`| K["WorkerMain describe 缓存（JVM 进程内，按 service）"]
    J -->|其他| L["WorkerMain 常规 invoke 路径"]
    K --> M["返回 ServiceSchema"]
    L --> M
    M --> N{"`response.ok`"}
    N -->|失败| O[返回结构化错误与诊断]
    N -->|成功| P[格式化结果输出]
```

说明：

- schema 缓存放在运行时 daemon 的 JVM 进程内存中，按同一 daemon key 的 CLI 实例共享；
- daemon 进程退出后缓存失效，不写本地 schema 文件；
- 可通过 `--refresh` / `--no-cache` 控制 describe 缓存刷新（会透传到 daemon 请求）。

## 快速开始

构建：

```powershell
mvn -f runtime-worker-java/pom.xml package
go build -o bin/sofarpc ./cmd/sofarpc
```

运行：

```powershell
go run ./cmd/sofarpc help
```

## Claude Code skill

仓库内置 `call-rpc` skill，安装后就是“触发 `sofarpc call` 的薄入口”。
用户级安装一次即可：

```powershell
sofarpc skills install          # 将 skills/call-rpc 复制到 ~/.claude/skills/
sofarpc skills where            # 查看源路径 / 目标路径
```

该 skill 不负责：
- 接入项目、构建索引、管理 cases
- 结果验证和业务语义解读

它只负责把一次调用映射到 `sofarpc call` 命令并透传执行结果。

完整用法、manifest 格式、runtime source 管理和诊断命令，请看
[docs/usage.zh-CN.md](./docs/usage.zh-CN.md)。
