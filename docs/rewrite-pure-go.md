# Pure-Go 重写计划

> 状态:**决策已定,待 spike 验证**
> 目标分支:`pure-go`(从 `main` 分出,现 `rewrite` 保留为历史)

## 1. 决策

放弃 Go + Java 双语言架构,整项目收敛为**单个 Go 二进制**,直接走 bolt + Hessian2 协议与 SOFARPC 服务端通信。

## 2. 第一性原理

这个工具的**最小职责**是:

> 把用户的 service + method + paramTypes + args → 一次 Hessian2 generic invoke →
> 把结果塞回 Agent。

围绕这个最小职责,现有架构里多处是**实现便利**而非**必要**:

| 现状假设 | 真相 |
| --- | --- |
| "SOFARPC SDK 是 Java,所以必须有 Java worker" | bolt + Hessian2 都是开放协议,Go 原生实现可行 |
| "需要 JVM 反射读 facade jar" | `javap` shell-out 或 trust mode 足够;Hessian2 字段容忍兜底 |
| "需要 JVM 池 + profile 分片" | 单用户单项目,池子永远 N=1 |
| "需要 Spoon 索引" | 用户不用,字段差异 Hessian2 自己兜住 |

单用户 / 单项目 / 每会话 5-20 次 invoke 的场景,**Go+Java 分工带来的全部复杂度都没有支点**。

## 3. 纯 Go 方案组件

```
sofarpc-mcp (单二进制, ~15MB)
├── MCP stdio 服务(internal/mcp,基本保留)
├── target 解析 + 可达性探测(internal/core/target,保留)
├── bolt codec(internal/boltclient,新写 ~500 行 或 port sofa-mosn)
├── Hessian2 codec(github.com/apache/dubbo-go-hessian2)
├── generic invoke 组装层(internal/invoke,用 GenericObject 对齐 SOFARPC Java SDK)
├── describe 层(internal/describe,javap shell-out,可选)
└── trust mode(facade == nil 走这条,已就绪)
```

## 4. 删除清单

Spike 成功后**一次性删**:

- `internal/worker/`(pool + wire + client)
- `internal/indexer/`
- `internal/mcp/workerstore.go` + `workerstore_test.go`
- `internal/facadesemantic/`(只服务 worker 反射)
- `internal/javatype/`(同上)
- `docs/architecture.md` §6 §7 两章 wire 协议
- 配置 env:`SOFARPC_RUNTIME_JAR` / `_JAR_DIGEST` / `_JAVA` / `_JAVA_MAJOR` / `_INDEXER_*` / `_FACADE_CLASSPATH`
- `cmd/sofarpc-mcp/main.go` 里的 worker/indexer 装配代码

## 5. 最大风险:Hessian2 generic invoke 兼容性

SOFARPC Java SDK 的 `GenericService.$invoke` 约定(`@type` 标签、基础类型包装、嵌套 Map 的类型传递、null 编码)和 `dubbogo/hessian` 默认行为**不一定完全对齐**。需要 spike 验证,必要时写适配层。

其他风险(bolt v1/v2、心跳、超时、TLS)都属于工程细节,不是未知量。

## 6. Spike 计划(Step 0)

**目标**:独立小程序,round-trip 一次最简单的 SOFARPC 调用。

**位置**:`cmd/spike-invoke/main.go`,不碰现有代码。

**伪代码**:
```go
func main() {
    // 1. TCP 连 bolt://host:port
    // 2. 组装 bolt request frame(参考 sofa-mosn/pkg/protocol/rpc/sofarpc/codec)
    // 3. Hessian2 编 ["$invoke", [paramTypes...], [args...]] 作为 payload
    // 4. 发送,读响应 frame,Hessian2 decode body
    // 5. 打印 result / exception
}
```

**成功判据**:调用一个公司测试环境的简单服务(比如 `echo(String)`),返回值和直接用 Java SDK 调一致。

**失败时的分支**:
- bolt 帧错 → 对着 sofa-mosn 或抓包修
- Hessian2 编码错 → `dubbogo/hessian` 的 type registry 定制
- `$invoke` payload 形态错 → 对着 SOFARPC Java SDK 源码抄 `com.alipay.sofa.rpc.filter.generic.GenericFilter` 附近
- 三天打不通 → 时间盒触发,撤回 Go+Java 瘦身方案

## 7. 推进步骤

| Step | 工作 | 预估 |
| --- | --- | --- |
| 0 | Spike,独立 `cmd/spike-invoke/` | 1-2 天 |
| 1 | 删 Java 相关代码(worker / indexer / facadesemantic / javatype / workerstore) | 半天 |
| 2 | `internal/boltclient/`:连接复用、超时、requestId、错误码映射 | 2-3 天 |
| 3 | `internal/describe/`:javap shell-out + 解析(或延后) | 1-2 天 |
| 4 | 打通 MCP:`invoke/plan.go` 的 Plan 交 boltclient 执行,清理 Plan 字段 | 1 天 |
| 5 | README / architecture.md 重写,env 文档更新 | 半天 |

总计 **6-10 个工作日**。

## 8. 待确认(明天)

1. **测试目标**:公司测试环境某个简单 SOFARPC 服务的 `bolt://host:port` + `service/method/paramTypes`。
2. **时间盒**:spike 失败回退线 —— 3 天为界,超过则重估方向。
3. **分支策略**:确认从 `main` 新开 `pure-go`,不在 `rewrite` 上继续。
