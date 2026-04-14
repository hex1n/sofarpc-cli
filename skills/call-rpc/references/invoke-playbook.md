# Invoke Playbook

## 什么时候读这份

需要下面任一内容时再读：
- 如何从 skeleton 补值
- `raw/generic/schema` 该怎么选
- 返回结果怎么判断
- RPC 层失败怎么排
- cases 怎么保存和回放

## 1. 补值顺序

### needs-input

优先级：
1. 用户给的真实值
2. `required: true` 且属于业务主键的字段
3. 注释里语义明确的字段
4. 其他辅助字段

规则：
- 缺业务主键先问用户
- 不要自己编“看起来像真的”业务值

### autonomous

目标只是打通链路时：
- 可以用明显占位值
- 但汇报里必须明确说明哪些值是占位

## 2. skeleton 占位值解释

常见占位：
- `String` → `""`
- 数字 → `0`
- `Boolean` → `false`
- `BigDecimal` → `"0"`
- 日期类型 → `null`
- `List/Set` → 单元素示例
- `Map` → 示例键值
- `enum` → 第一个枚举名

规则：
- 日期类型的 `null` 一般要换成真实格式
- `BigDecimal` 保持字符串
- enum 用 name，不用 ordinal
- 不需要列表元素时，直接发 `[]`

## 3. payloadMode 选择

默认顺序：
1. `raw`
2. `schema`
3. `generic`

### `raw`

优先使用场景：
- stub jar 齐全
- 顶层参数和嵌套 DTO 类都能加载
- 返回壳即使有 `Optional/helper getter`，也优先先试它

### `schema`

适合：
- 嵌套 `List<CustomDTO>`
- `Map<String, List<DTO>>`
- 多态 `Object` 字段
- 父子类混用

前提：
- 相关类能从 stubPaths 加载
- CLI 能通过 `describe` / 索引拿到类型签名

### `generic`

只在下面场景用：
- 某些 DTO 类根本拿不到
- provider 本身接受 map-ish payload
- 只是做轻量 smoke，不追求强类型还原

副作用：
- 返回更像 Map
- 嵌套自定义对象集合容易丢元素类型

## 4. 发调用

规则：
- flag 永远放在位置参数之前
- payload 外层永远是数组
- payload 长时写临时文件，用 `@file`

示例：

```bash
sofarpc call -context <ctx> -data '[{...}]' <FQN>.<method>
sofarpc call -context <ctx> -payload-mode schema -data @.rpc-tmp-demo.json <FQN>.<method>
```

常用 flag：
- `-full-response`
- `-context <name>`
- `-payload-mode <raw|generic|schema>`
- `-types <csv>`
- `-timeout-ms 30000`

## 5. 返回解读

三类结论：

- `OK`
  - `success=true`
- `BIZ_FAIL`
  - `success=false`
  - 链路通常是通的
- `RPC_FAIL`
  - CLI 退出非 0
  - 常见于超时、序列化失败、服务未找到

先做分类，再给用户结论。

## 6. 常见 RPC 故障

| 现象 | 常见原因 | 首选动作 |
|------|---------|---------|
| `RPC-020100010 未找到业务服务` | provider 没起、服务名错、uniqueId 错 | 检查 target、服务暴露、context |
| `SerializationException` / `ClassCastException` | payload 类型不对 | 先减 payload，再检查 `payloadMode` / `types` |
| `TIMEOUT` | 服务慢或不可达 | 提高超时，顺便看 `doctor` / daemon |
| `NoClassDefFoundError` | stubPaths 不全 | 补 facade jar 或依赖 jar |
| 服务端字段拿到 null | 字段名不对或 DTO setter 问题 | 回源码确认字段名和类型 |

## 7. 失败迭代顺序

1. 先把 payload 缩到最小
2. 只保留业务主键 + required 字段
3. 必要时二分排查问题字段
4. 用 `-full-response` 看完整诊断
5. 需要 Java 栈时看 `sofarpc daemon show <key>`

## 8. 保存和回放 cases

成功或有价值的业务失败，都可以沉淀成 case：

- 文件：`<state>/cases/<Service>_<method>.json`
- 一个文件多个 case
- 每个 case 至少带：
  - `name`
  - `notes`
  - `params`

批量回放：

```bash
sofarpc facade run-cases
sofarpc facade run-cases --filter <substr>
sofarpc facade run-cases --dry-run
sofarpc facade run-cases --save
```
