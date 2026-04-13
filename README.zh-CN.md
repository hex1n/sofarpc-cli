# sofarpc-cli

用于调用 SOFARPC 服务的 CLI。

结构：

- Go CLI 控制面
- Java SOFARPC worker runtime
- 本地 runtime cache 和 daemon 池

入口文档：

- 使用说明和命令参考：[docs/usage.md](./docs/usage.md)
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

完整用法、manifest 格式、runtime source 管理和诊断命令，请看
[docs/usage.md](./docs/usage.md)。
