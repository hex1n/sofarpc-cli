# sofarpc-cli

面向 Agent 的本地 MCP Server，用于执行 SOFARPC 泛化调用。

- 设计文档：[docs/architecture.md](./docs/architecture.md)
- 主线形态：pure-Go `direct + bolt + hessian2`
- 启动入口：`cmd/sofarpc-mcp`

## MCP 工具

| 工具 | 用途 |
| --- | --- |
| `sofarpc_init_project` | 为 Java 项目初始化 `.sofarpc/config*.json` 中的目标配置。 |
| `sofarpc_open` | 打开工作区。返回项目根目录、已解析 target、能力标识和 session id。 |
| `sofarpc_target` | 解析目标并探测可达性。 |
| `sofarpc_describe` | 当存在 contract information 时，解析重载并生成 JSON skeleton。省略 `method` 列出该服务的可调用方法；连 `service` 也省略则列出已知服务。 |
| `sofarpc_invoke` | 构建 plan 并执行调用。`dryRun=true` 返回 plan 和本地预检结果。 |
| `sofarpc_replay` | 用 `sessionId`（可加 `planId` 选择历史中某个 plan）或完整 `payload` 加 project/session 安全上下文重放 plan。 |
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

Go 的 `@latest` 选择最新可用的模块版本。要明确安装 `main` 分支的版本，
使用 `@main`。需要可复现的安装时，固定已发布的标签或提交。

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@main
sofarpc-mcp version -json
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
- 项目级：把 target 默认值 写进 Java 项目的 `.sofarpc/` 目录。

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
  --direct-url=bolt://dev-rpc.example.com:12200 --timeout-ms=3000

sofarpc-mcp setup --scope=project --project-root . --shared \
  --direct-url=bolt://dev-rpc.example.com:12200 --protocol=bolt
```

`--local` 写 `.sofarpc/config.local.json`，并确保这个路径出现在项目
`.gitignore` 里。`--shared` 写 `.sofarpc/config.json`。已有项目配置不会被覆盖，
除非显式传 `--force`。`--allow-invoke`、`--allowed-target-hosts` 这类真实调用
开关和 host guardrail 属于用户级 env，项目级 setup 会拒绝写入。target 字段属于项目级，用户级 setup 会拒绝写入。

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

初始化时至少传一个目标配置字段。要配置 BOLT 地址，请显式传入
`directUrl`。可通过 `sofarpc_describe` 从 Java 源码发现服务。

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

Server 使用 stdio MCP 协议。未指定项目时使用进程的当前目录。
目标配置的 `protocol` 和 `serialization` 默认分别为 `bolt` 和
`hessian2`。

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

项目配置里不要写 `mode`。mode 由解析出的 `directUrl` 推导为 direct，
低优先级 layer 的 endpoint 会被忽略。纯 Go 主线只执行 direct 的
`bolt + hessian2` 目标——不接受 `registryAddress`/`registryProtocol`,
含这两个字段的配置会作为未知字段被拒绝。

调用前会拒绝 `tr://` 地址，即使配置了 `protocol=bolt`。
`sofarpc_target` 的 TCP 探测单独检查端口可达性。

MCP 新调用使用 `profile` 选择目标 Profile，CLI 新调用使用 `-profile`。
MCP 调用省略时先看会话的 Profile。最后回退到项目的 `defaultProfile`。
目标字段按以下顺序解析。

```text
单次输入 > 本地 Profile > 共享 Profile > 本地基础配置 > 共享基础配置 > 默认值
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

安装的 [`sofarpc-invoke` 技能](cmd/sofarpc-mcp/skill/SKILL.md) 帮助 Claude Code
和 Codex 选择 CLI 或 MCP、检查预检结果，并按结构化提示排查错误。

## 单次 CLI 调用

`sofarpc-mcp call` 用与 MCP 工具完全相同的 plan/execute 管道和守护栏跑一次
调用——适合人工冒烟、CI 脚本、以及在 MCP client 之外复现 agent 的调用：

```sh
# 只看 plan，不碰网络
sofarpc-mcp call -project . -service com.foo.UserFacade -method query \
  -types java.lang.String -args '["id-1"]' \
  -direct-url bolt://127.0.0.1:12200 -dry-run

# 真实调用（与 MCP 工具相同的调用开关和目标限制）
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project . \
  -service com.foo.UserFacade -method query \
  -types java.lang.String -args '["id-1"]'

# 重放捕获的 plan（裸 plan JSON 或 invoke dry-run 输出都行）
sofarpc-mcp call -plan plan.json
```

CLI 输出与 `sofarpc_invoke` 共用 JSON 字段，包括 `ok`、`plan`、
`preflight`、`result`、`diagnostics` 和 `error`，按调用结果返回。
真实调用要求 `SOFARPC_ALLOW_INVOKE=true`。执行有参方法前需要提供明确的
参数向量。

计划生成后，CLI 的 `-dry-run` 和 MCP 调用或回放的 `dryRun: true`
都会返回计划和 `preflight`。输出可同时出现 `ok: true` 和
`preflight.ready: false`。查看 `preflight.policyError` 和
`preflight.executionError` 了解拦截原因。预检只检查本地策略、计划、
协议兼容性和目标地址。TCP 可达性、baggage 环境变量和请求编码另行检查。

## HTTP A/B 对比

在仓库目录运行 [`scripts/http-ab.py`](scripts/http-ab.py)，对比同一条 HTTP
只读请求在两个部署上的结果。JSON 场景描述两条路由、凭证环境变量、
响应字段和预期构建标识。`capture` 发送请求并采集结果。`compare` 离线核对
构建证据，再比较选定的业务数据。详见[配置和命令](scripts/http-ab.md)。

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
- 传输尝试会报告 `diagnostics.stage` 和 `diagnostics.requestState`。
  `request-written` 表示本机 socket 接受了完整请求。传输层生成的 SOFATracer
  context 会写入 `SofaRequest.requestProps["rpc_trace_context"]`，对应的
  `diagnostics.traceId` 可用于关联启用了 SOFATracer 的服务端日志。
  MCP 调用和回放失败时，首个文本块会给出错误码、阶段、可用的传输信息和
  适用的诊断工具提示。
- `directUrl` 指定单次调用的目标地址。省略时由所选 Profile 和项目配置
  解析目标。MCP 进程的真实调用环境变量用于执行策略判断。
- 回放接受 v2 计划、包含 `plan` 的干跑结果，或通过 `sessionId` 和可选
  `planId` 选择会话已捕获的计划。回放字面量 payload 时可传
  `sessionId`、`project` 或 `cwd` 选择执行策略上下文。
- 源码变更后可用 `sofarpc_open` 的 `refreshContract: true` 重新索引当前项目。
- 每个 MCP 进程最多保留 64 MiB 的计划 JSON，超出时优先淘汰最早的计划。
- 真实调用和 replay 都需要 `SOFARPC_ALLOW_INVOKE=true`。旧配置的顶层
  `allowedServices` 仍可读取，执行时会忽略。项目初始化会忽略旧的服务选项
  （CLI 的 `--allowed-services`，以及 MCP 的 `services`、`allowAllServices`
  和 `serviceNameSuffixes`）。
- 单次调用和会话可以选择项目已配置的 Profile。要调用所选 Profile 以外的
  地址，或 replay 的归档地址与当前默认地址不一致时，需要显式设置
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
直接消费这些 fixture，不要求本机 Java。协议及合同相关改动会在 PR 上自动
验证 Java 基线。发布前需要手动运行 GitHub Actions
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
  skills/sofarpc-invoke/SKILL.md 指向 cmd/sofarpc-mcp/skill/SKILL.md 的 symlink
docs/
  architecture.md        架构说明
```

## 状态

- 当前运行时主路径已经是 pure-Go。
- `sofarpc_describe` 现在直接依赖项目源码扫描，不需要 Java sidecar，也不需要本地缓存。
- 当前工作树下 `go test ./...` 全部通过。

## 许可证

Copyright 2026 He Xin (hex1n) and sofarpc-cli contributors.

本项目采用 [Apache License, Version 2.0](LICENSE) 许可。
第三方依赖仍遵循各自的许可证。
