---
name: call-rpc
description: >
  自主完成任意 SOFABoot 项目的 SOFARPC facade 实际调用与结果验证。触发词：
  "测一下 xxx 接口"、"调一下 facade"、"call 一下 xxx"、"发个 RPC"、
  "test facade / invoke / call RPC"、"跑一下 xxx 接口看返回"。支持复杂
  request：嵌套 DTO、泛型集合、继承父类、枚举、BigDecimal、日期类型、
  多参数方法。流程是索引优先：先查项目里的 JSON skeleton，再套用户真实值
  发 RPC，最后解读返回。不用于：只是读代码理解接口，或接口还没实现时纸面讨论。
user-invocable: true
allowed-tools: Read Grep Glob Bash Write Agent
---

# Call RPC

目标：当用户要“实际打一条 SOFARPC facade”时，独立完成这条链路：

1. 找到项目和当前生效的 facade 调用 state
2. 定位 facade / method
3. 从索引拿 skeleton，补值
4. 发 RPC
5. 判断是成功、业务错还是 RPC 错
6. 能复用时沉淀到 cases

## 何时用

用这个 skill：
- 用户要实际调 facade / 测 RPC / 看返回
- 用户要验证链路是否通
- 用户要批量回放已有 case

不要用这个 skill：
- 用户只是想看源码理解接口
- 用户在讨论接口设计，还没要实际调用

## 先做什么

先跑：

```bash
sofarpc rpc-test where [--project <path>]
```

这一步用来确认：
- tools / python 是否可用
- 当前 project root 是什么
- 当前生效的 state layout 是 `.sofarpc` 还是 legacy
- `config.json` / `index/` / `cases/` 实际落在哪

如果项目还没接入，再跑：

```bash
sofarpc rpc-test init [--project <path>]
```

## 默认工作流

### 1. 判断是 autonomous 还是 needs-input

- 用户只想看“链路通不通”或“先随便打一条”：
  autonomous，可以用占位值；业务错也算链路成功
- 用户要验证真实业务逻辑：
  needs-input，缺业务主键时先问用户，不要编

### 2. 定位 method

- 用户已给 `<FQN>.<method>`：直接用
- 用户只给中文描述：先查索引，再必要时搜源码
- 索引没有或明显过期：重建索引后再继续

### 3. 先查索引，再补值

- 从 `<state>/index/<FQN>.json` 拿 `paramsSkeleton`
- 重点看 `paramsFieldInfo`
- 优先填：
  - 用户给的业务主键
  - `required: true` 字段
  - 有明确语义注释的字段

不要在没有依据时硬编码项目路径、jar 名、context 名、业务常量，尤其不要复用之前项目里的值。

### 4. 发调用

默认先试：

```bash
sofarpc call -context <ctx> -data '[...]' <FQN>.<method>
```

规则：
- flag 放在位置参数之前
- payload 外层永远是数组
- 默认优先 `raw`
- 只有遇到明确边界再切 `generic` 或 `schema`

### 5. 解读结果

- `success=true`：成功
- `success=false`：业务错，链路通常已经通了
- 进程非 0 / `INVOKE_FAILED` / `SERIALIZE_FAILED` / `TIMEOUT`：RPC 层故障

先分清“业务错”还是“RPC 错”，再决定下一步。

### 6. 沉淀 case

如果 payload 可复用，把它存到 `<state>/cases/<Service>_<method>.json`。

## 必须守住的准则

1. 配置从项目 `config.json` 读，不硬编码历史项目信息。
2. 先查索引；索引缺失或陈旧先重建。
3. 缺业务主键时停下来问用户，不要编。
4. 占位值必须在汇报里点名，避免把“链路通”说成“业务正确”。
5. 默认优先 `raw`，不要看到一点序列化风险就直接切 `generic`。
6. 结束前优先给用户一个可执行结论：成功、业务错、还是 RPC 错。

## 需要细节时读什么

- 项目接入、state layout、`config.json` 字段、常用命令：
  读 [references/bootstrap.md](./references/bootstrap.md)
- 补值规则、payloadMode 选择、返回解读、失败排查、cases：
  读 [references/invoke-playbook.md](./references/invoke-playbook.md)
