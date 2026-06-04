# sofarpc-cli

面向 Agent 的本地 MCP Server，用于执行 SOFARPC 泛化调用。

- 设计文档：[docs/architecture.md](./docs/architecture.md)
- 主线形态：pure-Go `direct + bolt + hessian2`
- 启动入口：`cmd/sofarpc-mcp`

## MCP 工具

| 工具 | 用途 |
| --- | --- |
| `sofarpc_init_project` | 为 Java 项目初始化 `.sofarpc/config*.json`，包含自动发现的 `allowedServices`。 |
| `sofarpc_open` | 打开工作区。返回项目根目录、已解析 target、能力标识和 session id。 |
| `sofarpc_target` | 解析目标并探测可达性。 |
| `sofarpc_describe` | 当存在 contract information 时，解析重载并生成 JSON skeleton。 |
| `sofarpc_invoke` | 构建 plan 并执行调用。`dryRun=true` 只返回 plan。 |
| `sofarpc_replay` | 用 `sessionId` 或完整 `payload` 加 project/session 安全上下文重放 plan。 |
| `sofarpc_doctor` | 对 target、workspace 状态和 invoke 前提做结构化诊断。 |

所有失败都会返回稳定的 `errcode.Code`，并且可能带有结构化的
`nextTool` 提示。Agent 应该直接跟随这个提示，而不是从错误文案里自己推导。

## MCP prompts

Server 也暴露少量用户主动触发的 prompt，用作常见工作流入口。Prompt 只返回
使用 tools 的指令，不会自己执行 SOFARPC 调用。

| Prompt | 用途 |
| --- | --- |
| `sofarpc_bootstrap_project` | 引导首次安全写入 `.sofarpc/config*.json`。 |
| `sofarpc_dry_run_facade_call` | 引导 `open -> describe -> invoke dryRun=true` 的 facade 方法调用计划，包含可选 args 和 `invocationProperties`。 |
| `sofarpc_diagnose_failure` | 引导基于 errcode 的 `target`、`doctor`、`describe`、`replay` 诊断流程，并利用 session/plan resources。 |

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

如果要分发给不想安装 Go 的用户，可以构建 MCPB 一键安装包，默认覆盖
macOS、Linux 和 Windows：

```sh
VERSION=v0.1.0 MCPB_VERSION=0.1.0 bash scripts/build-mcpb.sh
```

默认 target matrix 是：

```text
darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
```

产物位于 `dist/mcpb/*.mcpb`。每个 bundle 只包含一个平台对应的二进制和
`manifest.json`；Windows 产物内是 `server/sofarpc-mcp.exe`。manifest 会暴露
真实调用开关、target override、允许访问的 target hosts、session plan
保留上限和最大响应体大小等安全配置。手动 GitHub Actions `mcpb` workflow
也会执行同一个脚本并上传这些 bundle。

Setup 分成两层：

- 用户级：把 MCP server 注册进 Claude Code 和 Codex，并给当前用户安装
  `sofarpc-invoke` skill。它只保留全局执行 guardrail，并会清掉旧的项目级
  SOFARPC env。
- 项目级：把 target 默认值和 service allowlist 写进 Java 项目的 `.sofarpc/` 目录。

用户级是默认 scope。它是幂等的，并且默认会合并已有 sofarpc env；再次执行
只传一个新参数时，不会把手工加过的 guardrail env 丢掉：

```sh
sofarpc-mcp setup --scope=user                                      # 两个客户端 + skill
sofarpc-mcp setup --claude-code                                     # 只注册 Claude Code
sofarpc-mcp setup --codex                                           # 只注册 Codex
sofarpc-mcp setup --install-skill=false                             # 只写 MCP 配置
sofarpc-mcp setup --allow-invoke --allowed-target-hosts=127.0.0.1   # 全局真实调用 guardrail
sofarpc-mcp setup --dry-run --allow-invoke                          # 预览用户级配置
```

如果用 `go run` 直接跑源码，请传 `--command /abs/path/to/sofarpc-mcp`，
或者先 build/install；setup 会拒绝把 Go 临时 build-cache 里的二进制路径写进
客户端配置。

项目级 setup 写入可以跟仓库走、或者只留在本地 checkout 的 target 配置：

```sh
sofarpc-mcp setup --scope=project --project-root . --local \
  --direct-url=bolt://dev-rpc.example.com:12200 --timeout-ms=3000 \
  --allowed-services=com.foo.UserFacade

sofarpc-mcp setup --scope=project --project-root . --shared \
  --registry-address=zookeeper://zk.example.com:2181 --protocol=bolt \
  --allowed-services=com.foo.UserFacade,com.foo.OrderFacade
```

`--local` 写 `.sofarpc/config.local.json`，并确保这个路径出现在项目
`.gitignore` 里。`--shared` 写 `.sofarpc/config.json`。已有项目配置不会被覆盖，
除非显式传 `--force`。`--allow-invoke`、`--allowed-target-hosts` 这类真实调用
开关和 host guardrail 属于用户级 env，项目级 setup 会拒绝写入。target 字段和
`--allowed-services` 属于项目级，用户级 setup 会拒绝写入。

Agent 也可以通过 MCP 完成同样的首次项目初始化：

```json
{
  "project": "C:\\path\\to\\java-project",
  "config": "local",
  "directUrl": "bolt://dev-rpc.example.com:12200"
}
```

把这段作为 `sofarpc_init_project` 参数调用即可。Agent 已知 workspace root
时应传 `project` 或 `cwd`；如果省略项目作用域，tool 只会从 MCP 进程 cwd
返回安全 Java 项目自动发现证据，不会写文件。必须在后续调用中显式传
`project`、`cwd` 或 `sessionId` 才会落盘。返回里始终包含
`projectResolution`，说明 `confidence`、`markers`、`scanTruncated` 和
`candidates`。

如果省略 `services`，tool 会按已解析项目加载 source-contract information，
并把有方法的 `*Facade` 接口写入 `allowedServices`。它不会猜 target；需要显式传
`directUrl` 或 `registryAddress`，否则只写 allowlist-only config 并返回下一步。
如果发现不到服务白名单，需要显式传 `services`，或者有意设置
`allowAllServices: true` 写入 `allowedServices: ["*"]`。

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
cd /abs/path/to/project
./bin/sofarpc-mcp
```

Server 使用 stdio MCP 协议。project root 回退到进程 CWD；
`SOFARPC_PROTOCOL` / `SOFARPC_SERIALIZATION` 默认是 `bolt` /
`hessian2`，除非要覆盖默认值，否则都不用设。

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
  "uniqueId": "dev",
  "allowedServices": ["com.foo.UserFacade", "com.foo.OrderFacade"]
}
```

项目配置里不要写 `mode`。mode 会按优先级从第一个 endpoint 原子推导：
`directUrl` 表示 direct，`registryAddress` 表示 registry，并且低优先级
layer 的另一个 endpoint 会被忽略。

target 解析优先级是：

```text
单次 input > .sofarpc/config.local.json > .sofarpc/config.json > 进程 env 兼容兜底 > defaults
```

`sofarpc-mcp` 启动和注册 tools 时不会扫描 `.java` 源码。source-contract
信息会在第一次有 tool 需要 contract store 时按已解析 project root 惰性加载，
随后按项目缓存，所以大项目不会拖慢 MCP startup。`project` / `cwd` 会显式
选择项目；否则 `sessionId` 会复用 `sofarpc_open` 打开的项目。隐藏目录、
测试目录和常见构建产物目录会被跳过。

### 手写客户端配置

如果你不想跑 `sofarpc-mcp setup`，可以把下面这段直接写进客户端的
MCP 配置里（Claude Code：`~/.claude.json` → `mcpServers`；Codex：
`~/.codex/config.toml` 下的 `[mcp_servers.sofarpc]`）：

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp"
    }
  }
}
```

## 典型调用链

1. 新 Java checkout 没有 `.sofarpc/config*.json` 时先调 `sofarpc_init_project`；
   已知项目时传 `project`/`cwd`，否则检查 `projectResolution`
2. `sofarpc_open`
3. `sofarpc_target`（可带 `project` 或 `cwd` 检查另一个项目）
4. 如果存在 contract information，则带 `sessionId` 调 `sofarpc_describe`
5. 带 `sessionId` 调 `sofarpc_invoke`
6. `sofarpc_replay` 使用 `sessionId`；如果传完整 `payload`，也可同时传
   `sessionId` 作为安全上下文
7. invoke 无法继续时调用 `sofarpc_doctor`

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
  "invocationProperties": {
    "tenant": { "value": "dev" },
    "authToken": { "env": "SOFARPC_AUTH_TOKEN" }
  },
  "directUrl": "bolt://host:12200",
  "sessionId": "ws_...",
  "dryRun": true
}
```

- `project` / `cwd` 可以显式选择 project root；否则 `sessionId` 提供
  project-scoped target config 和 contract store。
- `version` 会覆盖本次调用的 SOFA service version。
- `targetAppName` 会设置 direct transport 的 target app header。
- `invocationProperties` 是随网关透传的 SOFARPC request baggage。direct
  invoke 会把它编码成 `SofaRequest.requestProps["rpc_req_baggage"]`，下游 Java
  服务在 provider baggage 启用时可用 `RpcInvokeContext.getRequestBaggage(...)`
  读取。`value` 是字面值；`env` 只在真实 invoke/replay 时解析，并在 plan 中保持
  redacted。
- `directUrl` / `registryAddress` 是单次覆盖。否则先使用项目配置；进程 env
  只作为旧用法/手工运行兜底。用户级 setup 不再写 target 默认值。
- `dryRun=true` 返回的 plan 可以直接交给 `sofarpc_replay`；replay 也接受包含
  `plan` 字段的 dry-run 输出对象。literal payload replay 可以同时传
  `sessionId`、`project` 或 `cwd`，让执行策略使用目标项目的安全上下文。
- 真实 `sofarpc_invoke` 和 `sofarpc_replay` 都需要 `SOFARPC_ALLOW_INVOKE=true`；
  同时项目 `.sofarpc/config*.json` 必须显式配置 `allowedServices`。只有写
  `["*"]` 时才表示有意允许全部 service。
- 非 dry-run direct 调用默认只执行项目配置解析出的 direct target；如果要允许
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

用 `contractMode` 可以显式控制 contract 行为：

- `auto`（默认）：优先使用项目 contract；如果 contract 无法解析方法，且
  已提供完整 `service` / `method` / `types` / `args`，自动退到 trusted plan。
- `strict`：必须使用项目 contract，绝不退到 trusted。
- `trusted`：忽略 contract，完全信任调用方提供的 tuple。

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
