# Bootstrap

## 什么时候读这份

需要下面任一信息时再读：
- 项目还没接入 call-rpc
- `where` 显示的 layout / state dir 看不懂
- 要确认 `config.json` 应该放哪
- 要找命令入口和常用参数

## state layout

技能代码装在用户目录：
- `~/.claude/skills/call-rpc/`
- `~/.agents/skills/call-rpc/`

项目状态目录按优先级解析：
1. `<project>/.sofarpc/`
2. `<project>/.claude/rpc-test/`
3. `<project>/.claude/skills/rpc-test/`

`where` 会直接打印当前项目实际生效的是哪一套。

注意：
- `detect-config --write` / `init` 写主位置 `.sofarpc/`
- `build-index` / `run-cases` 跟随当前生效的 layout
- legacy 项目不会被静默迁移

## 最短接入路径

```bash
cd <project>
pip install javalang
sofarpc rpc-test init
```

如果不是在项目根跑：

```bash
sofarpc rpc-test init --project <path>
```

## 常用命令

```bash
sofarpc rpc-test where [--project <path>]
sofarpc rpc-test init [--project <path>] [--skip-index]
sofarpc rpc-test detect-config --write [--project <path>]
sofarpc rpc-test build-index [--project <path>]
sofarpc rpc-test run-cases [--project <path>] [--filter <sub>] [--dry-run] [--save] [--context <ctx>]
```

## `where` 该怎么看

重点看这几行：
- `project root`
- `state layout`
- `config path`
- `index dir`
- `cases dir`
- `sofarpcBin`
- `defaultContext`
- `manifestPath`

如果 `config/index/cases` 指到意料之外的项目，优先检查：
- 当前 shell 是否带了旧的 `SOFARPC_PROJECT_ROOT`
- 是否应该显式加 `--project`

## `config.json` 里最关键的字段

- `facadeModules[]`
  - `sourceRoot`
  - `mavenModulePath`
  - `jarGlob`
  - `depsDir`
- `mvnCommand`
- `sofarpcBin`
- `defaultContext`
- `manifestPath`

`defaultContext` 最好只在项目内填真实默认值，不要把某个项目里的环境名当成通用默认。

## facade 产物预检

至少确认：
- `jarGlob` 能匹配到 facade jar
- `depsDir` 下有依赖 jar

缺了就按 `config.json` 里的模块信息补构建，例如：

```bash
${mvnCommand} -pl ${mavenModulePath} -am install -DskipTests
${mvnCommand} -pl ${mavenModulePath} dependency:copy-dependencies -DincludeScope=runtime -DoutputDirectory=target/facade-deps
```
