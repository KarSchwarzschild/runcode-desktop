# 知识库接入 Stage 0：基线与运行环境

日期：2026-11-04。用户只授权执行 Stage 0。知识库菜单、检索、上下文注入均未实施；未合并、推送或部署服务。

执行结果：三个功能分支、用户工具链和可启动的原版测试 EXE 已准备完成；基线检查已执行，但不是全部通过。协议生成检查失败、一个图标工具测试被系统策略阻止、Windows manifest 打包问题及真实登录/租户/聊天未验收项均列在下文，不能据此宣称可发布。

## Git 基线

| 仓库 | 功能分支 | 起点 commit | 实际开发目录 |
| --- | --- | --- | --- |
| 智开桌面端 | `feat/knowledge-base-chat` | `1a7d819c0e4bfc58c7a32ed98a58b6070adc506b` | `C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main` |
| Bridge | `feat/knowledge-base-api` | `58e4f82e165ed31bceba1917aecf0203618a097e` | `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/worktrees/ouconline-ai-bridge` |
| agentloop | `feat/question-context` | `833be6238b4211fc6b9c23497cc2ff91298b74f5`（`v0.16.0`） | `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/worktrees/agentloop` |

桌面原目录 `C:/Users/yyzxc/Desktop/Client-Platform/runcode-desktop-main` 已关联 `https://github.com/mwt06/runcode-desktop.git`。其原本没有 HEAD；完整备份后获取 `origin/main`，使用 mixed reset 建立上游 Git 历史，没有覆盖产品文件。原目录留在跟踪 `origin/main` 的 `main`；原有方案文档保留并复制到开发工作区。

备份：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/backups/runcode-desktop-before-baseline-20261104`。桌面使用 Codex 托管 worktree；原生工具不接受当前聊天工作区外的两个兄弟仓库，因此 Bridge、agentloop 使用普通 Git linked worktree。三者都是独立分支，不在原仓库主分支或 detached HEAD 上开发。

三个桌面 `go.mod` 仍固定依赖 agentloop `v0.16.0`。本地开发通过外部 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/knowledge-base.go.work` 显式联动四个模块；绝对路径没有写入仓库的 `go.work`。这次构建属于本地引擎测试构建，不能替代 Stage 5 的 `GOWORK=off` 固定 tag 发布验收。

## 快速检查客户端

所有本机快捷脚本在 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/`：

1. `build-desktop.cmd`：关闭正在运行的智开后双击，使用现有品牌脚本重新构建测试版。检查三个分支，保存构建日志与来源清单；失败时保留错误，不启动旧包冒充新成果。
2. `start-desktop.cmd`：打开这个工作区的测试 EXE；发现另一目录的同名客户端在运行会提示先正常关闭它。
3. `check-desktop.cmd`：显示三个仓库的当前分支、commit、工作区状态，以及 EXE 的品牌、版本、SHA256。

实际执行入口是 `desktop.ps1`，`env.ps1` 只设置当前进程的工具 PATH、CGO 和开发 workspace。可直接运行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\Users\yyzxc\Desktop\Client-Platform\.codex-stage0\desktop.ps1' -Action Build
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\Users\yyzxc\Desktop\Client-Platform\.codex-stage0\desktop.ps1' -Action Run
```

以上命令的执行策略只作用于本次进程；没有更改全局 PATH、代理或系统策略。工具装在 `C:/Users/yyzxc/AppData/Local/Codex/tools/knowledge-base-stage0/`，无需管理员权限。

| 工具 | 验证的版本 |
| --- | --- |
| Go | `go1.26.10 windows/amd64`，官方 ZIP 大小与 SHA256 校验通过 |
| Wails CLI | `v3.0.0-beta.9`，与桌面模块一致；`go version -m` 和 `wails3 version` 已核验 |
| Windows CGO | WinLibs MinGW-w64 GCC `16.2.0`，UCRT / POSIX / SEH；归档 SHA256 与发行方及 GitHub asset digest 一致 |
| Node / npm | `v24.20.0` / `11.19.0`（已有安装） |
| Java | Microsoft OpenJDK `21.0.13.0.2`，官方 SHA256 校验通过 |
| Maven | `3.9.16`，官方 SHA512 校验通过 |
| WebView2 | 已安装 `156.0.4314.58` |

Go 外部 workspace 通过 `go work use` 生成原生 Windows 路径。手写的正斜杠绝对路径曾导致 Go 报“module 不在 workspace”，已修正；失败记录保留在 `logs/go-workspace-attempt1/`，不计为通过。公共 Go 依赖在开发脚本中使用进程级 `https://goproxy.cn,direct`，私有 GitLab 按 `GOPRIVATE` 处理，仍启用正常 Go checksum 校验。

测试版和已安装同品牌客户端共用账号配置及历史。使用新测试会话；窗口关闭即退出。改完代码需要重新构建再打开，直接运行旧 EXE 不会自动加载新代码。仅前端开发时可另用 Vite 预览，但这不能替代 Wails 与 Go 的真实交互检查。

## 验证记录

日志根目录：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/`。

| 项目 | 结果 / 证据 |
| --- | --- |
| 前端依赖、typecheck、lint、test、build | 全部退出码 0；39 个测试文件、512 项通过。`frontend-summary.log` 及各 `frontend-*.log`；lockfile 前后 SHA256 一致，产品文件未改。 |
| Bridge | `mvn test` 通过，110 项测试、0 失败、0 错误、0 跳过；`bridge-test.log`。首次因本地代理瞬断未能下载 Surefire 依赖，已保留 `bridge-test-attempt1-proxy.log`，使用隔离的无代理设置及同一缓存重跑通过。 |
| .NET 环境检查 | 现有 .NET / WindowsDesktop runtime `8.0.11`；`dotnet --list-sdks` 为空。Stage 0 不修改或构建 .NET 服务；若 Stage 2 必须修改该服务，再准备 .NET 8 SDK。 |
| 根模块 Go 测试 / 编译 | `go test -race ./...` 退出 0，19 个测试包通过；`go build ./...` 退出 0。见 `go-root-test.log`、`go-root-build.log`。 |
| server Go 测试 / 编译 | `go -C cmd/runcode-server test -race ./...` 退出 0，1 个测试包通过；build 退出 0。见 `go-server-test.log`、`go-server-build.log`。 |
| agentloop | 最终完整 `go test -race ./...` 退出 0，51 个测试包通过（49 个缓存）、5 包无测试，无 race 报告。首轮 sessions / tools/bash 超时；两包以 `-race -count=1 -p=1` 隔离复跑通过，再跑完整命令通过。三次日志保留为 `engine-test.log`、`engine-test-rerun.log`、`engine-test-final.log`。首次并行负载与固定 5 秒等待是可能因素，未将其断言为唯一原因。 |
| desktop 模块测试 | 桌面主包通过；`tools/icongen` 的测试程序被 Windows Code Integrity 策略阻止执行，整体 `go -C cmd/runcode-desktop test ./...` 退出 1。不是已通过或跳过。见 `go-desktop-test.log`、`desktop-test-code-integrity.log`（事件 3077）；没有关闭或绕过安全策略。 |
| 协议生成检查 | **未通过**，`go run ./tools/protogen --check` 退出 1：`PrepareExit: parameter limit: non-protocol type time.Duration`。见 `protocol-check.log`；上游 HEAD 已有该问题。 |
| Windows 测试 EXE | 品牌脚本及修正后的本机构建包装器退出 0；`--build-info` 退出 0。名称智开、版本 `1.0.20`、product `zhikai`、bundleID `cn.ouconline.ai.zhikai`。见 `desktop-build-manifest.json`、`desktop-verification-summary.json`。 |
| 窗口启动 | 本次 worktree 的 `bin/智开.exe` 确实创建窗口，窗口树显示首页、已有账号显示信息、qwen3.8-flash、空白新对话和原“+”入口。只验证界面加载与本地身份状态恢复，未验证凭据续期。 |
| 真实登录 / 租户选择 / 普通聊天 | **尚未完成**。图形操作工具报窗口坐标/前台进程错误，不能可靠操作测试窗口；后续桌面启动器打开的实例路径是已安装版本，未将其当成本次成果。需按下面步骤在本次测试 EXE 手工验收。 |

测试 EXE：`C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main/cmd/runcode-desktop/bin/智开.exe`，40,285,696 字节，SHA256：

```text
EE02F96902EAAEAD42C4343EE5AF505795216E5BACE9AD37D8460C64B958F928
```

Windows 打包另有一项基线问题：链接器输出 `.rsrc merge failure: multiple non-default manifests`，PE 核查确有两个相同资源 ID 的 manifest，一份包含尚未替换的 Wails 名称占位符，另一份来自 Go。它没有阻止这台机器上的 EXE 身份查询和窗口创建，但不能视为合格发布包；后续发布前需修正并验证 DPI、longPathAware 和版本元数据。见 `desktop-pe-manifest.json`。直接 DLL 导入未发现额外的 libgcc/libwinpthread 依赖，没有复制外部 DLL。

协议生成问题的根因位于 `tools/protogen/extract.go`：原生接线方法排除表漏了 `internal/desktop/exit.go` 的 `PrepareExit`。它不属于前端协议命令；后续修复应将它加入排除表并补回归检查，保持对非协议类型的拒绝规则。本阶段保留原版产品代码，所以未绕过检查或重新生成协议文件。

外部包装器已修正 Windows GUI EXE 的 stdout 捕获：使用隐藏的 `--build-info` 子进程、显式等待与重定向，再核对退出码/JSON；直接 PowerShell `&` 调用曾取到空输出而误报身份。CMD 包装器也已验证保留原退出码。最初的 transcript 对 Bash 输出捕获不完整，后来已改为显式写原生命令日志；本次验证证据还包括 `desktop-verification-summary.json` 和构建来源清单。

可复跑 Bridge 测试（不需要服务部署）：

```powershell
. 'C:\Users\yyzxc\Desktop\Client-Platform\.codex-stage0\env.ps1'
Set-Location -LiteralPath $Stage0Bridge
$javaTools = Join-Path $Stage0Tools 'java'
mvn.cmd -B -ntp -s (Join-Path $javaTools 'settings-stage0-direct.xml') -gs (Join-Path $javaTools 'global-settings-stage0.xml') ('-Dmaven.repo.local=' + (Join-Path $javaTools 'm2-repository')) test
```

## 待手工完成的原版客户端验收

1. 正常关闭已安装的智开客户端，然后双击 `.codex-stage0/start-desktop.cmd`。不要使用指向 `C:/Program Files/Ouc/desk_agent/` 的旧快捷方式。运行 `check-desktop.cmd` 核对本报告中的路径、版本与摘要。
2. 在设置中确认存在测试版专属“上下文审核”入口；检查账号状态。若登录已失效，使用现有通行证登录流程；查看租户列表并选择自己获授权的租户、平台模型。
3. 在新测试会话中发送“这是 Stage 0 普通聊天检查，请只回复 Stage 0 OK，不调用工具。”，检查正常收到回答。这个操作只证明普通聊天，不证明知识库检索或上下文注入。

本阶段没有启动或部署测试 Bridge 服务；这项普通聊天检查仍使用客户端原来配置的服务。Stage 2 才建立知识库相关测试服务配置。

现有前端依赖检查报告 15 个漏洞（14 moderate、1 high），并有 uuid 弃用、esbuild 安装脚本及 Vite 大 chunk 提示；作为上游基线记录，本阶段不擅自升级依赖。

下载缓存保留在用户工具目录；一次临时缓存清理被自动审批拒绝（仅返回 `blocked by policy`），未重试清理，不影响已校验工具的使用。

## 后续阶段边界

Stage 1 才新增“+ → 知识库”和当前会话标签。Stage 2 才对接 Bridge 的真实个人/共享知识库权限与检索；Stage 3 实施引擎逐题上下文能力；Stage 4 完整联调；Stage 5 异常回归与固定 tag 发布候选验收。

三个功能分支已为这些工作保留。Stage 0 不证明当前客户端已能访问个人知识库，也不证明服务端的新接口已部署。账号继续使用客户端原有通行证流程；聊天中提供的密码和令牌没有写入脚本、文档、仓库或测试夹具。
