# Agent-First 实现计划

> **状态变更（rewrite 分支）**：原计划是从已有代码迁移到 agent-first 设计；后改为清空重建。语义随之转变：
>
> - **阶段 0**：地基包（`errcode` + `javatype` + `SemanticClassInfo.Interfaces`）依旧最先做，已完成并恢复在工作树。
> - **阶段 1 / 2 / 4 / 7**：原本是替换/收敛旧实现，现在变成"一开始就按目标态写"。表格中的"删除旧 X"=" 不要写出 X"。
> - **阶段 3**：MCP env 默认值注入，新建 entry point 时直接支持。
> - **阶段 5**：worker daemon key 不含 classpath hash —— Java 重写时遵守。
> - **阶段 6**：indexer 增量（`--since <mtime>`）—— Java 重写时一上来就实现。
>
> 阶段标号保留是为了让后续 commit / PR 引用一致。
>
> **2026-04 补充**：`sofarpc.manifest.json` 与 context 概念已从目标态中完全移除。
> 配置层精简为 `agent input → MCP env → 默认值` 三层；多环境切换通过在
> `mcp.json` 里注册多个 MCP server 条目实现，每条携带各自的 `SOFARPC_*` env。
> 下文阶段 3 / 阶段 7 与 Delta 表中涉及 context / manifest 的描述请视为历史背景。

按 **风险 × 收益** 排序。每个阶段独立可发布、可回滚。

目标：第一性原理下的 agent-first SOFARPC 调用工具——3 进程（Go 控制面 + Java indexer + Java invoke worker）、6 个 MCP 工具、3 个类型 Role、15 条错误码、单一 payload 模式。

---

## 阶段 0：地基（1-2 天，纯内部，零破坏）

目的：抽出后续阶段都依赖的共享词汇，避免改一个地方漏一个地方。

| # | 动作 | 文件 | 风险 |
|---|---|---|---|
| 0.1 | 新建 `internal/javatype` 包，定义 `Role`（UserType / Container / Passthrough）+ 6 条 hint 表 | 新建 | 无（无调用方） |
| 0.2 | 新建 `internal/errcode` 包，定义错误码 + `Hint{NextTool, NextArgs}` 结构，Marshal 进 `model.RuntimeError` | 新建 + 扩 `internal/model/model.go` | 低（仅追加字段） |
| 0.3 | 给 `facadesemantic.SemanticClassInfo` 补 `Interfaces []string` 字段，indexer 同步输出 | `internal/facadesemantic/semantic.go` + `spoon-indexer-java/.../SpoonSemanticAnalyzer.java` | 中（需 indexer rebuild） |

完成标志：`go build ./...` 通过，没有任何调用方，但下面阶段可以直接 import。

---

## 阶段 1：类型角色统一（3-5 天，最高收益）

目的：消除 `internal/contract/generic.go` 与 `internal/facadeschema/schema.go` 双份漂移的类型表。

依赖：阶段 0.1、0.3。

1. 在 `internal/javatype` 实现 `Classify(typeFqn string, registry facadesemantic.Registry) Role`。走 `Superclass + Interfaces` 链：
   - 命中 `java.util.Collection / Map` → Container
   - 命中 `java.lang.Number / CharSequence / Date / Temporal / Enum / 基本类型` → Passthrough
   - 其它 → UserType
2. `internal/contract/generic.go::compileValue` 改为：`if Classify == UserType { 注入 @type } else { 透传 }`，删除 7 张 `*Types` map。
3. `internal/facadeschema/schema.go::renderSkeleton` 改为：`switch Role { ... }` + 6 条 hint 表渲染占位符，删除 7 张 `*Like` map。
4. 跑完整 e2e（call + describe + facade schema），对比金值。

完成标志：grep `numberTypes\|stringLike\|decimalTypes` 在仓库里为零；新增类型只需扩 hint 表（一行）或 indexer 更出 interface（自动归类）。

---

## 阶段 2：错误码贯通（2-3 天，Agent 体验最大改善）

目的：每个失败路径都给 Agent 下一步动作。

依赖：阶段 0.2。

按调用栈倒推填错误码，按工具分批推进：

| 工具 | 主要错误源 | NextTool / NextArgs |
|---|---|---|
| `invoke_rpc` | `target.Mode == ""` | `nextTool=resolve_target`, `nextArgs={service}` |
| `invoke_rpc` | 反序列化失败 | `nextTool=describe_method`, `nextArgs={service,method}` |
| `describe_method` | overload ambiguity | `nextTool=describe_method`, `nextArgs={service,method,types:[...]}` |
| `resolve_target` | probe failed | `nextTool=doctor`, `nextArgs={}` |
| `open_workspace_session` | facade 未初始化 | `nextTool=facade init`, `nextArgs={}` |

落点：`internal/cli/resolution.go:79`、`internal/app/invoke/*`、`internal/adapters/mcpserver/server.go` 各 handler 出错处。

完成标志：随机扔 5 个常见错误给 Agent，它能仅凭 `nextTool / nextArgs` 自愈。

---

## 阶段 3：MCP 配置注入（1 天，用户当前痛点）

目的：通过 `mcp.json` 的 env 配置 direct-url 等默认值，无需每次传参。

依赖：无。

1. `cmd/sofarpc-mcp/main.go` 从一组 `SOFARPC_DIRECT_URL / SOFARPC_REGISTRY_ADDRESS / ...` env 读入 `target.Config`。
2. `mcp.Options.TargetSources.Env` 承载该默认值，`core/target.Resolve` 的三层优先级链为：
   ```
   agent input > MCP env > 内置默认
   ```
3. 多环境切换：在 `mcp.json` 内注册多个 MCP server 条目（`sofarpc-dev` / `sofarpc-stg` / ...），各自带独立 env。

完成标志：`mcp.json` 设 `SOFARPC_DIRECT_URL=bolt://...`，Agent 调 `sofarpc_invoke` 不传 target 即可工作。

---

## 阶段 4：MCP 工具收敛 8 → 6（2-3 天，Agent 选择负担）

依赖：阶段 2（错误码内含状态提示后，`inspect_session / resume_context` 才能安全合并）。

| 旧 | 新 | 动作 |
|---|---|---|
| `open_workspace_session` + `inspect_session` + `resume_context` | `sofarpc_open` | `open` 总返回完整 session 状态；删另两个 handler |
| `plan_invocation` | 合进 `sofarpc_invoke` 的 `dryRun:true` | `plan_invocation` 删除，`invoke_rpc` 加 dryRun 分支 |
| `list_facade_services` | `sofarpc_list` | 重命名 + 文档 |

`internal/adapters/mcpserver/server.go` 1175 行同步瘦身（目标 < 700 行）。

完成标志：`mcp.tools` 列表 6 个，Agent 选工具不再犹豫。

---

## 阶段 5：Worker daemon key 简化（3-4 天，冷启动开销）

依赖：阶段 1（类型解析不再依赖具体 jar 集合）。

当前：`daemonKey = sha(profile + classpathHash)`，换 stub 集合就重启。

目标：`daemonKey = sha(sofaRpcVersion + runtimeJarDigest + javaMajor)`，stub 在 invoke 调用里临时 `URLClassLoader` 隔离。

1. 改 `runtime-worker-java/.../WorkerMain.java` 的 invoke 入口：每次请求构造 child classloader（按 stub paths 哈希缓存复用，TTL 5 min）。
2. 改 `internal/runtime/manager.go` 的 `daemonKey`，去掉 classpathHash。
3. 删 `retireProfileDaemons` 里按 classpath 触发的换出逻辑。
4. 压测：相同 profile 跨多个 service，确认 daemon 不再翻滚。

完成标志：连续 invoke 10 个不同 facade jar 的服务，Java 进程数稳定为 1。

---

## 阶段 6：Indexer 增量化（5-7 天，大项目性能）

依赖：无（独立子系统）。

1. `spoon-indexer-java` 接受 `--since <mtime>`，只重扫修改过的 `.java`，merge 进旧 registry（落 `_index.json` + `_index.shards/*.json`）。
2. `internal/facadeindex.RefreshIndex` 默认走增量，`--full` 强制全扫。
3. 监听文件系统变更（可选，第二步）。

完成标志：500+ 类项目首次 30s，二次 < 2s。

---

## 阶段 7：删除（1-2 天，减负）

阶段 1-6 落地后做一次集中清理：

- 删 `payload-mode` 三选一（`raw / generic / schema`）→ 全程 generic + 自动 @type，flag 保留兼容但 hidden。
- 彻底移除 `sofarpc.manifest.json` / context 概念（已完成）：`internal/core/workspace` 只负责 project root 解析，不再读任何项目级配置文件。
- 评估 `metadata` daemon：阶段 6 后 indexer 增量够快则可删。

---

## 依赖图

```
0.1 ─┐
0.3 ─┴─→ 阶段 1 ─→ 阶段 5
0.2 ───→ 阶段 2 ─→ 阶段 4
              阶段 3 (独立)
              阶段 6 (独立)
              阶段 7 (最后)
```

并行机会：阶段 1 / 2 / 3 / 6 可同时由 4 条分支推进。

---

## 推荐起点

阶段 0.2 + 阶段 2 的第一个错误码（`invoke_rpc` 缺 target 的 hint）。

理由：单点改动可演示给 Agent，立刻拿到反馈；阶段 1 的类型重构虽然收益最大，但需要先验证 indexer 输出 interface 链路（阶段 0.3 要等 Java 侧 rebuild）。错误码先行可以让你边做阶段 1，边验证 Agent 行为变化。

---

## Delta 汇总（当前 → 目标）

| 维度 | 当前 | 目标 |
|---|---|---|
| payload modes | 3（raw/generic/schema） | 1（隐式 generic） |
| 类型分类 | ~12 张 map，两处漂移 | 3 roles + 6 hints，单一来源 |
| 配置层 | 4（flag / context / manifest / defaults） | 3（agent input / MCP env / 默认值） |
| MCP 工具数 | 8 | 6 |
| worker daemon key | profile + classpathHash | profile（classpath 移到请求级 classloader） |
| indexer | 全量重扫 | mtime 增量 |
| 错误返回 | message + code | message + code + nextTool + nextArgs |
