---
name: call-rpc
description: >
  仅把用户 RPC 调用意图映射为一次 `sofarpc call` 命令并执行，返回命令输出，不做项目索引/参数推断/结果解读。
user-invocable: true
allowed-tools: Bash
---

# Call RPC

目标：只做一件事——触发 RPC 调用。

## 触发词

- "call 一下"
- "测一下"
- "调 facade"
- "发个 RPC"
- "call-rpc"

## 行为边界（必须遵守）

1. 只执行 `sofarpc call`。
2. 不读取 `.sofarpc/config.json`、不查 index、不构建索引、不运行 `build-index`、`detect-config`、`run-cases`。
3. 不做 payload 填充、类型推断、cases 落盘、结果分类。
4. 用户没给完整参数时，先追问，不猜测缺失字段。

## 最小工作流

1. 从用户输入提取以下参数：

   - `<method-fqn>`（如 `com.example.UserFacade.getUser`）
   - `--context`（可选）
   - `-data` 或 `-data @file`
   - 可选 `-payload-mode`、`-full-response`、`-timeout-ms`、`-types`

2. 组装命令：

```bash
sofarpc call [flags] <method-fqn>
```

3. 直接执行命令并原样返回标准输出与标准错误。

## 禁止行为

- 不调用 `sofarpc facade where`、`detect-config`、`build-index`、`run-cases`。
- 不改写、生成、保存任何 `cases` 文件。
- 不在响应里解读业务含义，只返回原始执行结果。
