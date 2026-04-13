# sofa-rpcctl

这个仓库现在以 `greenfield/` 下的新实现为主。

当前结构：

- Go CLI 控制面
- Java SOFARPC worker runtime
- 本地 runtime cache 和 daemon 池

入口文档：

- 使用说明和命令参考：[greenfield/README.md](./greenfield/README.md)
- 设计文档：[docs/greenfield-sofarpc-cli-design.md](./docs/greenfield-sofarpc-cli-design.md)

## 快速开始

构建：

```powershell
mvn -f greenfield/runtime-worker-java/pom.xml package
go build -o greenfield/bin/rpc ./greenfield/cmd/rpc
```

运行：

```powershell
cd greenfield
go run ./cmd/rpc help
```

也可以在仓库根目录直接执行：

```powershell
go run ./greenfield/cmd/rpc help
```

完整用法、manifest 格式、runtime source 管理和诊断命令，请看
[greenfield/README.md](./greenfield/README.md)。
