# sofarpc-cli

面向 Agent 的本地 MCP Server，用于执行 SOFARPC 泛化调用。

- 设计文档：[docs/architecture.md](./docs/architecture.md)
- 主线形态：pure-Go `direct + bolt + hessian2`
- 启动入口：`cmd/sofarpc-mcp`

## MCP 工具

| 工具 | 用途 |
| --- | --- |
| `sofarpc_open` | 打开工作区。返回项目根目录、已解析 target、能力标识和 session id。 |
| `sofarpc_target` | 解析目标并探测可达性。 |
| `sofarpc_describe` | 当存在 contract information 时，解析重载并生成 JSON skeleton。 |
| `sofarpc_invoke` | 构建 plan 并执行调用。`dryRun=true` 只返回 plan。 |
| `sofarpc_replay` | 用 `sessionId` 或完整 `payload` 重放一次 plan。 |
| `sofarpc_doctor` | 对 target、workspace 状态和 invoke 前提做结构化诊断。 |

所有失败都会返回稳定的 `errcode.Code`，并且可能带有结构化的
`nextTool` 提示。Agent 应该直接跟随这个提示，而不是从错误文案里自己推导。

## 安装

新机器上直接在线安装，不需要 Java 运行时：

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@latest
```

仓库内也提供辅助脚本：

```sh
./scripts/install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

## 快速开始

构建：

```sh
go build -o bin/sofarpc-mcp ./cmd/sofarpc-mcp
```

配置项目级 MCP env：

```sh
export SOFARPC_PROJECT_ROOT=/abs/path/to/project
export SOFARPC_DIRECT_URL=bolt://host:12200
export SOFARPC_PROTOCOL=bolt
export SOFARPC_SERIALIZATION=hessian2
```

可选覆盖项：

```sh
# 替代 target 来源
export SOFARPC_REGISTRY_ADDRESS=zookeeper://host:2181

# 可选 direct invoke 提示
export SOFARPC_UNIQUE_ID=demo
export SOFARPC_TIMEOUT_MS=3000
export SOFARPC_CONNECT_TIMEOUT_MS=1000
```

启动：

```sh
./bin/sofarpc-mcp
```

Server 使用 stdio MCP 协议。

启动时，`sofarpc-mcp` 会在 `SOFARPC_PROJECT_ROOT` 下扫描 `.java`
源码，并在 pure-Go 路径里构建 describe 所需的 contract information。
隐藏目录、测试目录和常见构建产物目录会被跳过。

如果你的 Agent 宿主支持项目级 MCP 配置，建议把同样的值写进该项目的 MCP
server 条目里，这样每次调用就不用重复传 `directUrl`：

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp",
      "env": {
        "SOFARPC_PROJECT_ROOT": "/abs/path/to/project",
        "SOFARPC_DIRECT_URL": "bolt://host:12200",
        "SOFARPC_PROTOCOL": "bolt",
        "SOFARPC_SERIALIZATION": "hessian2"
      }
    }
  }
}
```

## 典型调用链

1. `sofarpc_open`
2. `sofarpc_target`
3. 如果存在 contract information，则 `sofarpc_describe`
4. `sofarpc_invoke`
5. `sofarpc_replay`
6. invoke 无法继续时调用 `sofarpc_doctor`

## `sofarpc_invoke` 形状

```json
{
  "service": "com.foo.Facade",
  "method": "getUser",
  "types": ["com.foo.GetUserRequest"],
  "args": [{ "userId": 1 }],
  "version": "2.0",
  "targetAppName": "foo-app",
  "directUrl": "bolt://host:12200",
  "dryRun": true
}
```

- `version` 会覆盖本次调用的 SOFA service version。
- `targetAppName` 会设置 direct transport 的 target app header。
- `directUrl` / `registryAddress` 是单次覆盖；否则以 MCP env 为准。
- `dryRun=true` 返回的 plan 可以直接交给 `sofarpc_replay`。

当 contract information 存在时，facade-backed invoke 会在进入 wire 之前自动
归一化常见 Java 形状：

- root / nested DTO 会自动补 `@type`
- `List<DTO>` / `Map<String, V>` 的 value 会递归归一化
- `java.math.BigDecimal` / `BigInteger` 会自动包装成 canonical typed object

例如，dry-run plan 可能会把：

```json
{
  "args": [
    {
      "amount": 1000.5
    }
  ]
}
```

变成：

```json
{
  "args": [
    {
      "@type": "com.foo.GetUserRequest",
      "amount": {
        "@type": "java.math.BigDecimal",
        "value": "1000.5"
      }
    }
  ]
}
```

## Trusted mode

即使没有 contract guidance，`sofarpc_invoke` 也能运行，只要调用方显式提供：

- `service`
- `method`
- `types`
- `args`

这种情况下 plan 会标记为 `contractSource: "trusted"`，不会做重载消歧，也不会
自动做类型归一化或生成 skeleton。远端如果需要 `@type`、`BigDecimal` 等
Java 特定形状，调用方需要自己显式传入。

## 仓库结构

```text
cmd/
  sofarpc-mcp/           MCP 入口
  spike-invoke/          direct transport 验证 CLI
internal/
  boltclient/            纯 Go BOLT client
  sofarpcwire/           SofaRequest / SofaResponse 编解码
  sourcecontract/        Java 源码扫描 -> contract store
  errcode/               稳定错误码 + 恢复提示
  mcp/                   工具注册 + handlers
  core/
    workspace/           项目根目录解析
    target/              优先级链 + TCP 探测
    contract/            重载解析 + skeleton 生成
    invoke/              plan 构建 + 执行
  facadesemantic/        contract metadata shapes
  javatype/              Java 类型分类辅助
docs/
  architecture.md        架构说明
```

## 状态

- 当前运行时主路径已经是 pure-Go。
- `sofarpc_describe` 现在直接依赖项目源码扫描，不需要 Java sidecar，也不需要本地缓存。
- 当前工作树下 `go test ./...` 全部通过。
