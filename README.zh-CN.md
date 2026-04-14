# sofarpc-cli

用于调用 SOFARPC 服务的 CLI。

结构（有意多语言，各自做自己最擅长的事）：

- **Go** —— CLI 控制面、daemon 生命周期、runtime 缓存。冷启动快、
  Windows 子进程语义干净、单文件二进制分发。
- **Java** —— 随目标版本对齐的 SOFARPC worker runtime，以及基于 Spoon 的 facade 语义索引器。

入口文档：

- 使用说明和命令参考：[docs/usage.zh-CN.md](./docs/usage.zh-CN.md)
- 设计文档：[docs/sofarpc-cli-design.md](./docs/sofarpc-cli-design.md)

## 快速开始

构建：

```powershell
mvn -f runtime-worker-java/pom.xml package
go build -o bin/sofarpc ./cmd/sofarpc
```

运行：

```powershell
go run ./cmd/sofarpc help
```

## Claude Code skill

仓库内置 `call-rpc` skill，安装后就是“触发 `sofarpc call` 的薄入口”。
用户级安装一次即可：

```powershell
sofarpc skills install          # 将 skills/call-rpc 复制到 ~/.claude/skills/
sofarpc skills where            # 查看源路径 / 目标路径
```

该 skill 不负责：
- 接入项目、构建索引、管理 cases
- 结果验证和业务语义解读

它只负责把一次调用映射到 `sofarpc call` 命令并透传执行结果。

完整用法、manifest 格式、runtime source 管理和诊断命令，请看
[docs/usage.zh-CN.md](./docs/usage.zh-CN.md)。
