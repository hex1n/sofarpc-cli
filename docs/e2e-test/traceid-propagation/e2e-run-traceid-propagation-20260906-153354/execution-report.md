# SOFATracer traceId 透出 —— 端到端执行报告

**结论：8/8 通过。** 真实 SOFARPC provider（SOFATracer 默认开启）确认采用了 sofarpc-cli 客户端生成的 traceId，并把它写进 `rpc-server-digest.log`；该 traceId 同时出现在工具返回的 `diagnostics.traceId`。这条业务成果——「调用完拿 traceId 去 provider 日志里定位这次调用」——端到端成立。

| 状态 | 计数 |
|---|---|
| `passed` | 8 |
| `failed` | 0 |
| `blocked` | 0 |
| `unverified` | 0 |
| `skipped` | 0 |

选中场景：S0, S1, S2, S3, S4, S5, S6, S7（全部）。无排除。

## 1. 运行上下文

| 项 | 值 |
|---|---|
| 计划 | `docs/e2e-test/traceid-propagation/2026-09-06-traceid-propagation-e2e-test-plan.md` |
| 计划 sha256 | `871d96b612c0585d4d8510cf08aa5d3bcf9db4a8a902afc6c10fc68bee899722`（交付前复核一致） |
| 被测构建 | `go build ./cmd/sofarpc-mcp`，sha256 `81700734d95335b8e767554535051362dbf0162bb8b2ab1ffe68162bad93427b` |
| 源码身份 | git HEAD `2d5021d4e9fee677f51da62f6edc9f124ea27bbd` **+ 未提交的 trace 改动**（`git-status.txt`）——被测的就是本次改动本体，非已提交版本 |
| 目标 | `bolt://127.0.0.1:21200`，本会话自建的一次性 provider；**未接触任何业务环境** |
| provider 运行时 | sofa-rpc-all 5.8.0 + tracer-core 3.0.8 + bolt 1.5.9 + hessian 3.3.13，JDK 1.8.0_452 |
| 真实 vs 替身 | provider 是**真实** SOFARPC 运行时（真 tracer / 真 bolt / 真 hessian2），仅业务 facade 为专门写的 echo；S3 的 deaf 监听与 S6 的空端口是传输失败替身 |
| 时间 | 2026-09-06 15:33–15:44 |
| 入口 | `sofarpc-mcp call`（与 MCP 工具同一 plan/execute 管道与执行护栏） |

## 2. 场景结果

| ID | 状态 | Oracle | 期望 | 实际 | 证据 |
|---|---|---|---|---|---|
| S0 | `passed` | derived | provider 就绪且 SOFATracer 生效 | `PROVIDER_READY port=21200`；`tracelog/` 生成 | `evidence/S0-provider-ready.txt` |
| **S1** | **`passed`** | **specified** | `diagnostics.traceId` 等于 provider 端 server span 的 traceId | 两端同为 `c0a812de17886801157011001850`；业务结果 `echo:hello` | `evidence/S1-cli-output.json`、`evidence/S1-provider-stdout.txt` |
| **S2** | **`passed`** | derived | 该 traceId 出现在 provider digest 日志 | `rpc-server-digest.log` 命中 1 行，`"tracerId":"c0a812de…1850"`、`"span.kind":"server"`、`"result.code":"00"` | `evidence/S2-digest-hit.txt` |
| S3 | `passed` | specified | 超时返回 `runtime.timeout` 且 traceId 非空 | `code=runtime.timeout`，`traceId=c0a812de178868020413210012409` | `evidence/S3-cli-output.json` |
| S4 | `passed` | specified | 连续两次执行 traceId 不同且各自可追 | `…6153810011763` / `…6158110011765`，provider 两条 `SERVER_TRACE` 分别匹配 | `evidence/S4-*.json`、`evidence/S4-provider-stdout.txt` |
| S5 | `passed` | derived | baggage 与 trace 共存互不影响 | provider 读到 `baggage={route=blue, tenant=e2e-dev}` 且 traceId 与客户端一致 | `evidence/S5-provider-stdout.txt` |
| S6 | `passed` | specified | connect-failed 返回 traceId 但服务端确无记录 | `code=target.connect-failed`，traceId 非空，digest 日志 `in_provider_digest=False` | `evidence/S6-not-searchable.txt` |
| S7 | `passed` | specified | provider 侧能认出 agent 发起的调用 | 4 条 digest 行的 `remote.app` 全为 `sofarpc-cli` | `evidence/S7-digest-remote-app.txt` |

### S1 期望修正（同一权威内的自纠）

- **旧期望（计划 §5 S1 第 4 条）**：「`SERVER_TRACE rpcId` 为服务端派生的子 span id（非空）」
- **新期望**：服务端**原样采用**客户端的 `sofaRpcId`，本次为 `0`
- **来源**：同一份已 pin 的权威 `RpcSofaTracer#saveSpanContextAndTags` —— `new SofaTracerSpanContext(traceId, rpcId)` 直接用上游 rpcId 构造，不派生。旧期望是我对该权威的误读，不是产品行为差异。
- **影响范围**：仅 S1 第 4 条断言；S1 的主判据（traceId 相等）不受影响。

## 3. 证据与发现

### 主判据原始输出（S1）

```
CLI      : "traceId": "c0a812de17886801157011001850"   "result": "echo:hello"   "ok": true
provider : SERVER_TRACE traceId=c0a812de17886801157011001850 rpcId=0 baggage={} message=hello
digest   : {"tracerId":"c0a812de17886801157011001850","spanId":"0","span.kind":"server",
            "remote.app":"sofarpc-cli","service":"com.example.demo.EchoFacade:1.0",
            "method":"echo","result.code":"00", ...}
```

id 结构与 `TraceIdGenerator` 一致：`c0a812de`(=192.168.18.222) + `1788680115701`(millis) + `1001`(seq) + `850`(客户端 pid)。

### 根因记录

| # | 现象 | 分类 | 处置 |
|---|---|---|---|
| 1 | S1 首次执行返回 `runtime.rejected: java.lang.NoClassDefFoundError: io/grpc/Context` | `environment`（本次搭建的 provider classpath 缺 `io.grpc:grpc-context:1.28.0`，SOFATracer 3.x 依赖它做上下文传播） | 补 jar 后重启 provider 重跑，S1 通过。**首次失败保留**：该次输出同时证明请求已抵达 provider（`responseContentLength=178`）且失败路径也回带了 traceId |
| 2 | S5 的 digest 行 `"baggage":""` 为空，但业务方法确实读到了 `{route=blue, tenant=e2e-dev}` | 观察，非缺陷 | digest 的 `baggage` 字段记录的是穿透属性（`sofaPenAttrs`），不是 `rpc_req_baggage`；服务端读取路径已由 `SERVER_TRACE` 行证实 |
| 3 | digest 行 `"remote.ip":"127.0.0.1"` 与我们发送的 `sofaCallerIp`（192.168.18.222）不同 | 观察，非缺陷 | 服务端以实际 socket 对端地址覆盖该 tag；`remote.app` 仍来自我们发送的 `sofaCallerApp`，调用方识别不受影响 |

## 4. 续跑与状态

### 复跑步骤（手工顺序执行，逐步检查结果后再进行下一步）

```bash
# 1. 编译一次性 provider（jar 全部取自本机 ~/.m2，无需联网）
javac -encoding UTF-8 -cp "$(cat harness/classpath.txt)" -d <classes> harness/*.java

# 2. 启动 provider（SOFATracer 由 sofa-rpc-all 内置 rpc-config.json 默认开启）
java -cp "$(cat harness/classpath.txt):<classes>" \
     -Dlogging.path=<logdir> -Dprovider.port=21200 \
     -Dprovider.registry.file=<tmp>/registry.local \
     com.example.demo.ProviderMain          # 等待 PROVIDER_READY

# 3. 真实调用（项目配置见 evidence/e2eproj-config.json）
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project <proj> \
  -service com.example.demo.EchoFacade -method echo \
  -types java.lang.String -args '["hello"]'

# 4. 比对
grep "<diagnostics.traceId>" <logdir>/tracelog/rpc-server-digest.log
```

`harness/classpath.txt` 中的 `${M2}` 替换为 `~/.m2/repository`，`${SCRATCH}` 替换为本次工作目录。

### 清理与留存

- **业务数据变更：无。** 全程 read-only，目标为本会话自建的一次性本地进程，未触碰任何业务环境或数据库。
- provider 进程、deaf 监听进程、`registry.local`、临时 project 目录、provider 日志目录：位于 `<tmp>`，运行结束后已终止/随会话销毁（生命周期 `delete`）。
- 留存在仓库内的仅为文本证据：本目录 `evidence/`（CLI 输出、provider stdout、digest 日志副本）与 `harness/`（可复现的 provider 源码与 classpath 模板）。

### 后续可做

- G1（计划 §8）：构造一个 `default.tracer` 置空的 provider，验证「未接 SOFATracer 时静默降级」这条已写进文档的行为。
- G2：在真实业务测试环境上重跑 S1+S2（本次只覆盖本地 5.8.0 provider）。
