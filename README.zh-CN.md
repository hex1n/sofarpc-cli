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

仓库内辅助脚本：

```sh
./scripts/install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

Setup 分成两层：

- 用户级：把 MCP server 注册进 Claude Code 和 Codex，并给当前用户安装
  `sofarpc-invoke` skill。
- 项目级：把 target 默认值写进 Java 项目的 `.sofarpc/` 目录。

用户级是默认 scope。它是幂等的，并且默认会合并已有 sofarpc env；再次执行
只传一个新参数时，不会把手工加过的 guardrail env 丢掉：

```sh
sofarpc-mcp setup --scope=user                                      # 两个客户端 + skill
sofarpc-mcp setup --claude-code                                     # 只注册 Claude Code
sofarpc-mcp setup --codex                                           # 只注册 Codex
sofarpc-mcp setup --install-skill=false                             # 只写 MCP 配置
sofarpc-mcp setup --replace-env --direct-url=bolt://host:12200      # 替换 sofarpc env
sofarpc-mcp setup --dry-run --allow-invoke --allowed-services='*'   # 预览
```

如果用 `go run` 直接跑源码，请传 `--command /abs/path/to/sofarpc-mcp`，
或者先 build/install；setup 会拒绝把 Go 临时 build-cache 里的二进制路径写进
客户端配置。

项目级 setup 写入可以跟仓库走、或者只留在本地 checkout 的 target 配置：

```sh
sofarpc-mcp setup --scope=project --project-root . --local \
  --direct-url=bolt://dev-rpc.example.com:12200 --timeout-ms=3000

sofarpc-mcp setup --scope=project --project-root . --shared \
  --registry-address=zookeeper://zk.example.com:2181 --protocol=bolt
```

`--local` 写 `.sofarpc/config.local.json`，并确保这个路径出现在项目
`.gitignore` 里。`--shared` 写 `.sofarpc/config.json`。已有项目配置不会被覆盖，
除非显式传 `--force`。`--allow-invoke` 这类真实调用 guardrail 属于用户级 env，
项目级 setup 会拒绝写入。

Skill 是通过 `//go:embed` 烤进二进制的，所以一个全新的 `go install`
就自带它 —— 不需要 clone 仓库。canonical 源文件位于
`cmd/sofarpc-mcp/skill/SKILL.md`；仓库里的
`.claude/skills/sofarpc-invoke/SKILL.md` 是指向它的 symlink，这样在本
checkout 里用 Claude Code 也能直接被自动发现。

## 快速开始

大多数用户只需要上面的 setup 流程。下面的章节覆盖手动路径 —— 源码构建、
不依赖 `setup` 直接跑 server、或者手工编辑客户端配置。

### 源码构建

```sh
go build -o bin/sofarpc-mcp ./cmd/sofarpc-mcp
```

### 不依赖客户端配置直接运行

```sh
export SOFARPC_PROJECT_ROOT=/abs/path/to/project
export SOFARPC_DIRECT_URL=bolt://host:12200

./bin/sofarpc-mcp
```

Server 使用 stdio MCP 协议。`SOFARPC_PROJECT_ROOT` 回退到进程 CWD；
`SOFARPC_PROTOCOL` / `SOFARPC_SERIALIZATION` 默认是 `bolt` /
`hessian2`，除非要覆盖默认值，否则都不用设。

可选的 per-target 调优：

```sh
export SOFARPC_REGISTRY_ADDRESS=zookeeper://host:2181
export SOFARPC_UNIQUE_ID=demo
export SOFARPC_TIMEOUT_MS=3000
export SOFARPC_CONNECT_TIMEOUT_MS=1000
```

项目级 target 配置可以放在 Java 项目目录里。可提交的共享默认值放在
`.sofarpc/config.json`；个人机器或环境相关的 `directUrl` 建议放在
`.sofarpc/config.local.json`：

```json
{
  "directUrl": "bolt://dev-rpc.example.com:12200",
  "protocol": "bolt",
  "serialization": "hessian2",
  "timeoutMs": 3000,
  "connectTimeoutMs": 1000,
  "uniqueId": "dev"
}
```

项目配置里不要写 `mode`。mode 会按优先级从第一个 endpoint 原子推导：
`directUrl` 表示 direct，`registryAddress` 表示 registry，并且低优先级
layer 的另一个 endpoint 会被忽略。

target 解析优先级是：

```text
单次 input > .sofarpc/config.local.json > .sofarpc/config.json > MCP env > defaults
```

`sofarpc-mcp` 启动和注册 tools 时不会扫描 `.java` 源码。source-contract
信息会在第一次有 tool 需要 contract store 时惰性加载，所以大项目不会拖慢
MCP startup。隐藏目录、测试目录和常见构建产物目录会被跳过。

### 手写客户端配置

如果你不想跑 `sofarpc-mcp setup`，可以把下面这段直接写进客户端的
MCP 配置里（Claude Code：`~/.claude.json` → `mcpServers`；Codex：
`~/.codex/config.toml` 下的 `[mcp_servers.sofarpc]`）：

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp",
      "env": {
        "SOFARPC_PROJECT_ROOT": "/abs/path/to/project",
        "SOFARPC_DIRECT_URL": "bolt://host:12200"
      }
    }
  }
}
```

## 典型调用链

1. `sofarpc_open`
2. `sofarpc_target`（可带 `project` 或 `cwd` 检查另一个项目）
3. 如果存在 contract information，则 `sofarpc_describe`
4. `sofarpc_invoke`
5. `sofarpc_replay`
6. invoke 无法继续时调用 `sofarpc_doctor`

已安装的 `sofarpc-invoke` skill 把这条链条变成机器可读的 playbook，
包含 errcode 恢复协议。源文件在 `cmd/sofarpc-mcp/skill/SKILL.md`。

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
- `directUrl` / `registryAddress` 是单次覆盖；否则先使用项目配置，再使用 MCP env。
- `dryRun=true` 返回的 plan 可以直接交给 `sofarpc_replay`；replay 也接受包含
  `plan` 字段的 dry-run 输出对象。
- 真实 `sofarpc_invoke` 和 `sofarpc_replay` 都需要 `SOFARPC_ALLOW_INVOKE=true`；
  可用 `SOFARPC_ALLOWED_SERVICES` 限制允许调用的 service FQN。
- 非 dry-run direct 调用默认只执行项目配置或 MCP env 解析出的 direct target；如果要允许
  单次 `directUrl` 覆盖或 literal replay payload 里的 target，需要显式设置
  `SOFARPC_ALLOW_TARGET_OVERRIDE=true`。可用 `SOFARPC_ALLOWED_TARGET_HOSTS` 继续限制
  允许访问的 host 或 host:port。项目 target 配置无效时，`sofarpc_target` /
  `sofarpc_doctor` 会报告错误，真实 invoke 会被拒绝直到配置修好。
- direct BOLT response body 在分配和解码前会受
  `SOFARPC_MAX_RESPONSE_BYTES` 限制，默认 16 MiB。
- `sofarpc_doctor` 会通过 `invoke-policy` 检查报告真实调用 guardrail。

当 contract information 存在时，facade-backed invoke 会在进入 wire 之前自动
归一化常见 Java 形状：

- root / nested DTO 会自动补 `@type`
- `List<DTO>` / `Map<String, V>` 的 value 会递归归一化
- `java.math.BigDecimal` / `BigInteger` 会自动包装成 canonical typed object
- enum constant 会自动包装成 SOFA canonical enum object：
  `{"@type":"com.foo.Status","name":"ACTIVE"}`

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

## 测试与发布

默认验证仍然保持 Go-only：

```sh
go vet ./...
go test -race ./...
go build ./...
```

SOFARPC/Hessian wire 兼容性由
`internal/sofarpcwire/testdata/golden` 下提交的 fixture 兜底。默认 Go 测试
直接消费这些 fixture，不要求本机 Java。发布前需要手动运行 GitHub Actions
里的 `wire-fixtures` workflow；它会重新生成基线 Java fixture，并验证声明的
SOFARPC 版本矩阵。

## 仓库结构

```text
cmd/
  sofarpc-mcp/
    skill/               内嵌的 sofarpc-invoke SKILL.md (go:embed 源)
  spike-invoke/          direct transport 验证 CLI（build tag: spike）
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
  javamodel/             Java class / field / method value types
  javatype/              Java 类型分类辅助
.claude/
  skills/sofarpc-invoke/ 指向 cmd/sofarpc-mcp/skill/ 的 symlink，方便 in-repo 发现
docs/
  architecture.md        架构说明
```

## 状态

- 当前运行时主路径已经是 pure-Go。
- `sofarpc_describe` 现在直接依赖项目源码扫描，不需要 Java sidecar，也不需要本地缓存。
- 当前工作树下 `go test ./...` 全部通过。
