# 知识库 Stage 1：实现与验收记录

> 本文保留 Stage 1 初版记录。用户随后要求的紧凑布局、滚动加载与最多 5 库修订，见 [Stage 1 紧凑版记录](knowledge-base-stage1-compact.md)；当前测试 EXE 以新记录为准。

日期：2026-11-04。本阶段代码已实现并打包，自动化检查与独立审查完成；完整鼠标/键盘交互仍待手工验收。

## 已实现的效果

- 对话输入区“+ → 知识库”，选择器展示图标、名称、简介、逻辑目录路径、文档数和可区分的 ID；支持按名称/简介/路径搜索与分页。
- 最多选择 3 个库，输入区保留可移除的名称标签，悬停可读完整名称、路径及简介。
- 选择通过真实 Wails 命令保存到 Go 管理的会话文件，切换会话、重开和进程重启后可恢复；同一会话切换模型不丢失选择。
- 读取/保存中阻止发送；保存失败保留已确认状态；旧异步响应不能覆盖新会话。后端校验 revision，拒绝旧版本覆盖。
- 选中知识库时明确提示“检索尚未接通”，阻止普通发送、追加、图片、重新生成、问答卡片和计划执行，并保留待发送内容。移除所有标签后可进行原有普通聊天。

**当前是测试目录，不是真实个人知识库。** 仅显式 `--test` 构建提供以下合成数据，页面同时标明路径为逻辑目录、文档数为测试值；普通构建显示服务未接入，不自动回退为测试库。

| 名称 | 逻辑路径 | 示例文档数 |
| --- | --- | --- |
| 数字员工01（测试示例） | 测试目录 / 数字员工01 | 3 |
| 制度指南（测试示例） | 测试目录 / 制度指南 | 5 |
| 产品手册（测试示例） | 测试目录 / 产品手册 | 2 |
| 入职培训（测试示例） | 测试目录 / 入职培训 | 4 |

这些条目使用独立 `demo:` ID，不是用户真实库的副本。路径不是网页链接或本地文件地址。本阶段没有真实知识检索、正文注入或知识库回答。

## 分支与产物

| 仓库 | 分支 | 本阶段源码 commit / 状态 |
| --- | --- | --- |
| 智开桌面 | `feat/knowledge-base-chat` | 功能 `d69b321231052b794cd536efefc40995ab782dfe`；协议工具前置修复 `e9f93f486177eb5fa57caff1b561229378361632` |
| Bridge | `feat/knowledge-base-api` | `58e4f82e165ed31bceba1917aecf0203618a097e`，未修改、未部署 |
| agentloop | `feat/question-context` | `833be6238b4211fc6b9c23497cc2ff91298b74f5`（v0.16.0），未修改 |

开发目录：`C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main`。原始桌面目录保留，未在原目录构建。所有提交仅留在本地功能分支，工作区保留供后续阶段使用。

测试 EXE：`C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main/cmd/runcode-desktop/bin/智开.exe`。

- 构建时间：2026-11-04 10:53（Asia/Shanghai），大小 40,338,432 字节。
- 产品：`zhikai`，版本 `1.0.20`，bundleID：`cn.ouconline.ai.zhikai`；构建与 `--build-info` 均退出 0。
- SHA256：`7FB4BBE4D77EDA9F8C523A8E49512840D0BB9D24667AE1615BD44E86AE7CE02A`。
- 使用现有品牌脚本 `--brand zhikai --test --local-engine`。外部 go.work 指向未修改的 agentloop 功能分支；这不是 Stage 5 固定 tag 发布验收。
- 产物对应功能 commit `d69b321`；构建时只有方案/设计文档未提交，产品代码已提交。后续验收文档提交不改变该产物来源。

构建来源清单：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/stage1-desktop-build-manifest.json`。Stage 0 清单已另存，原版 EXE 备份仍保留。

## 便捷检查：5 步

1. 正常退出旧客户端，双击 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/start-desktop.cmd`。也可打开上面的测试 EXE；不要点指向 `C:/Program Files/Ouc/desk_agent/` 的旧快捷方式。本次启动的测试进程仍在运行时，可先从任务栏还原窗口。
2. 新建测试会话 A，点击输入区左下角“+ → 知识库”。确认可看到表中的名称、简介、逻辑路径和文档数，以及明显的测试数据说明；搜索“制度”应只显示匹配项。
3. 选择“数字员工01（测试示例）”，确认出现名称标签和“已保存”；再选择两项后，第四项应不可继续选中。关闭选择器，输入任意测试问题：发送应不可用，文字仍保留。
4. 新建会话 B，确认不会带入 A 的选择；返回 A，确认标签恢复。关闭并重开 A、重启客户端或切换模型，再确认选择仍在。前提是同一账号、知识库服务和租户。
5. 移除全部标签，确认普通发送重新可用。使用原有可用登录和模型进行普通聊天；这一步需要模型服务可达。若历史有问答卡或待审批计划，有选库时应阻止提交，移除后可继续。

日常改完后先退出客户端，再双击同目录下 `build-desktop.cmd` 重建，随后用 `start-desktop.cmd` 启动；`check-desktop.cmd` 显示当前分支、commit、EXE 版本和摘要。只改源码不会自动更新已有 EXE。

## 自动化与人工检查结果

| 检查 | 实际结果 |
| --- | --- |
| 前端 typecheck / eslint / Vitest / Vite | 全部 exit 0；42 文件、528 测试通过；最终补丁后执行。Vite 仍有既有大 chunk 提示。 |
| 根模块 `go test -race ./...` | 最终后端修复后 exit 0，19 个测试包通过；包括 desktop 和 protogen，无 race 报告。`stage1-go-root-race.log` 保存原始输出。 |
| 桌面主模块 `go -C cmd/runcode-desktop test .` | exit 0。本阶段未重复未修改的 icongen 包测试，Stage 0 的系统阻止记录仍保留。 |
| Go 定向测试 / vet | desktop/protocol 测试和 race 通过，vet 通过；知识库定向 race 通过。后端摘要明确区分各轮修改时序。 |
| 协议检查 | `go run ./tools/protogen --check` exit 0。补全 PrepareExit 排除项后按 Go DTO 生成真实 TypeScript，保持原有协议类型检查。早期 protogen 测试受系统策略阻止；最终根模块 race 已正常执行该包并通过，未修改系统策略。 |
| Go lint | 使用 CI 指定 v2.1.6，由 Go 1.26.10 源码构建。完整 run exit 1：334 个 gofmt 问题，全在未修改的基线 CRLF 文件；本次新增/修改 Go 文件 0 个问题，直接 gofmt 检查也无输出。没有批量格式化原仓库。 |
| 独立审查 | 审查发现的模型切换丢选择、问答卡提前“已回复”、计划审批提前变状态均已修复并复审，无剩余阻断问题。 |
| 真正 Windows EXE | 品牌构建和 build-info 成功；进程/窗口路径确认是本次 worktree 的 EXE，界面树确有包含“知识库”的加号入口。 |
| 手工交互 | 尚未完成。点击工具报告窗口最小化，重新绑定/恢复仍失败，因此没有把“展开菜单、选库、窄窗口、IME、重启恢复”的人工检查记为通过。按上面 5 步验收。 |

前端原始日志：工作区 `.codex-stage0/logs/stage1-frontend-{typecheck,lint,test,build}.log`；Go、lint、构建日志：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/`。临时日志未提交入库。

后端测试覆盖持久化、重启恢复、会话/账号/租户/Bridge 隔离、CAS 并发、损坏/超大记录、占用目标文件导致写入失败、Windows junction 越界、删除清理、各发送入口与模型切换。真实 HTTP 测试模型验证拒绝的内容不会进入模型/历史，空选的原始问题不被改写。普通 symlink 测试因 Windows 权限跳过，等价的 junction 测试实际通过。

存储位于会话工作区 `.runcode/knowledge/<sessionId>.json`，只保存测试库 ID、修订号及身份域指纹，不保存令牌或知识正文。关闭会话保留，删除会话清理。知识库 Bridge 身份独立于生成模型的 API 地址。

## 保留边界

Stage 2 才接通真实个人/共享知识库目录、权限和检索；Stage 3 增加引擎上下文能力，Stage 4 完成真实问答联调。现有账号登录流程保持原样，本阶段没有用聊天中的密码或令牌做测试，也未验证线上租户/真实库权限。

Windows 链接器仍输出 Stage 0 已有的 `.rsrc merge failure: multiple non-default manifests`。本机产物可查询身份并创建窗口，但发布前仍需解决重复 manifest。当前交付是本地功能测试包，不是发布候选。
