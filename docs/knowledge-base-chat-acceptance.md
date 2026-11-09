# 知识库宿主问答验收记录（2026-11-05）

Task 2–6 的实现和自动化验证已完成；Task 7 的固定依赖、构建及本地模拟验证已完成，但**真实业务链路与 Windows 窗口交互验收尚未通过**。不将本记录作为上线、发布或旧完整智能体方案的验收证明。

## 版本和改动边界

- 桌面分支：`feat/knowledge-base-chat`，本次产品实现提交 `c72bdae`。本记录的后续提交仅补充验收说明。
- Bridge：`feat/knowledge-api`，`163b265ca9463a638d20c58055307ce281f693dd`，本轮未修改。
- 三个 Go 模块和生成的 Windows EXE 均实际检查为 `gitlab.ouc-online.com.cn/aibase/agentloop v0.16.0`，没有引擎 Replace；构建和检查统一 `GOWORK=off`。
- 引擎工作区仍为 `833be6238b4211fc6b9c23497cc2ff91298b74f5`，工作区干净。未新建分支、未修改模块缓存、未使用 `--local-engine`。
- 新增逻辑集中于 `internal/knowledgechat`、桌面知识库宿主路由及前端知识库模式。原有入口只加模式分流与隔离检查；没有整库格式化、普通聊天重写或新的引擎接口。新增功能的状态、权限、存储、取消和事件边界均有中文注释。

## 已实现行为

普通会话选库后创建独立知识库会话；有历史时明确确认，不把原智能体历史自动搬入。迁移纯文字未发送草稿，附件和重试附件会被拒绝并保留。清空所有标签仍保持知识库模式。

原问、选择版本和 submissionId 在返回 receipt 前原子保存。同一提交重发返回同一问题，内容冲突拒绝；receipt 前的停止也会取消后续工作。检索和权限准备共享 30 秒，成功后由已发布 Provider 单轮生成，最多 120 秒；不进入引擎 Session、工具、自动标题或摘要。

资料以独立 JSON 数据消息传入，原问不被拼接修改。预算先裁完整旧问答，再裁尾部整片；来源与传递依赖从实际保留内容派生。每轮重新检索，并复核保留历史依赖的库权限与平台模型权限。401 最多共享一次强制刷新，刷新后再次检查选库（含零命中库）和历史依赖，成功检索不重复调用。

流式文字、thinking 合计受 64 KiB UTF-8 限制；工具调用失败且不执行；max_tokens 显示截断。终态先保存再通知界面，保存失败不宣告 completed。有输出后失败不自动重开流。正式路径拒绝原始 SSE 抓包配置；完整上下文审计只在测试构建显式开启时使用。

会话绑定账号、租户、workspace、知识库 Bridge 和模型 Bridge。恢复只读取本地记录，未完成题标为 interrupted，不自动执行。相同账号可离线查看和复制导出历史；发送仍重新授权。重试创建新分支、只复制目标题之前完成的问答，显式空 override 可清空本次检索。旧扩展格式不能交给旧引擎解码；无法验证身份归属时隐藏正文和标题。

目录首次只取第一页，搜索防抖 300ms，后续由用户点“加载更多”。关闭选择器丢弃迟到结果。来源仅来自实际发送集合，空 URL 不伪造链接。侧栏知识库菜单提供重命名及复制导出。

## 自动化证据

| 检查 | 结果 |
| --- | --- |
| 根模块 `go test -race ./...` | 退出 0，包含知识库、普通聊天与其他根模块包；最新日志 `no-engine-task27-root-final.log` |
| server 模块 `go test -race ./...` | 退出 0 |
| desktop 模块 `go test ./...` | 退出 0 |
| 三模块 `go build ./...` | 均退出 0 |
| `go run ./tools/protogen --check` | 退出 0 |
| 前端 `npm test` | 52 个文件、616 项通过 |
| 前端 typecheck / lint / build | 均退出 0 |
| `git diff --check` | 退出 0 |
| golangci-lint 对本次改动 | 0 issues |
| 完整 golangci-lint | 原 Windows 工作区有既有 CRLF 格式提示；复制 763 个源码文件到隔离目录，仅将 Go CRLF 转为 LF 后运行全量 lint，0 issues；未为此修改无关源码 |
| 准确 Bridge JAR 的 Maven clean verify | Task 0 已实跑 175 tests，0 failures/errors/skipped；Task 7 复核同一准确构建报告，未冒充重新执行 |

Windows 上 `TestKnowledgeStoreCannotEscapeWorkspace` 的符号链接子场景因系统无创建符号链接权限跳过，不宣称该场景在此机器验证通过。

真实 v0.16.0 Provider + 本地 HTTP/SSE fixture 覆盖：检索资料实际进入模型、原问保持不变、没有 tools、一次刷新、刷新时撤权、断流不重试、工具请求拒绝、输出截断/超限、流中取消及终态保存失败。独立复核发现的选库摘要、旧账号列表、终态丢失、receipt 前停止和普通预览引用问题均观察失败后修复并通过回归。

Bridge 已有计数测试覆盖总量 500 时默认仅检查本页 10 条、最大页 50 条、pageSize=500 拒绝、上游返回超页长时在逐项详情前拒绝。未向真实服务制造 500 次详情调用。

## Windows 产物及界面限制

- 命令：`GOWORK=off bash ./scripts/build-desktop.sh --brand zhikai-guokai --test`，退出 0。
- 路径：`C:/Users/yyzxc/.codex/worktrees/knowledge-base-stage4/runcode-desktop-main/cmd/runcode-desktop/bin/智开（国开版）.exe`。
- 文件大小：40,834,048 字节；生成时间：2026-11-05 19:44:59（Asia/Shanghai）。
- SHA256：`30E9BC6A0A353E58052686A76DFBA9344280B4E7D774855DC6259E1BAB62B632`。
- `go version -m` 实证 windows/amd64、agentloop v0.16.0。
- 构建保留两项工具提示：前端 chunk 超过 500kB；Windows 链接器输出 `.rsrc merge failure: multiple non-default manifests`。命令实际退出 0，产物可启动；尚未完成窗口资源外观检查，不能凭退出码宣称所有 Windows 资源正确。
- 已用隔离 APPDATA/LOCALAPPDATA 和合成模型服务启动该准确 EXE，进程存活且模拟服务记录 me、租户和 models 请求。没有真实模型或真实知识库数据参与。
- Computer Use 的 Windows native pipe 在首次调用、重试及重建会话后均报系统找不到文件；未取得可操作窗口，因此中文输入法、键盘、长标题、真实流式来源、重开与 A/B 窗口验收仍待人工执行。

## 真实环境结果与阻塞

本地 Bridge `http://127.0.0.1:18207` 运行的是上述 `163b265` JAR，健康检查 200/UP；原有实例未停止或替换。

JAR：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/no-engine-task0-bridge/ouconline-ai-bridge-0.1.0-SNAPSHOT.jar`。

SHA256：`8C43232B3C906DBED30D89989AC2D3A9C1E7CBA4ACDBB4840CF2A5B0914E9A39`。

| 服务 | 配置 |
| --- | --- |
| Passport issuer | `https://passport-ai-dev.ouc-online.com.cn` |
| 平台成员 API 根地址 | `https://aibase-preview.ouc-online.com.cn` |
| .NET 知识库根地址 | `http://123.249.28.107:15003` |
| Core 根地址 | `http://123.249.28.107:15001` |

用户提供的服务密钥仅保存在本机用户绑定密文中，注入隔离 Bridge 进程；未进入源代码或报告。不存在的非空库 GUID 在不带 Knowledge key 时为 401，带配置 key 后返回 200、authorization=true、state=false、data=null 的“未找到”封套，确认该服务密钥有效。Core key 已注入，业务链仍未验证。

本机正常 dev 登录的 access token 已过期，且 aud/scope 缺少 `manageapi`。真实目录、详情、retrieve 请求均实际返回 401，尚未进入业务 ACL。相同 desktop OAuth 请求不带 manageapi 可进入登录页，加入 manageapi 后先进入 `/home/error`；未取得具体协议错误码，不宣称已确定 invalid_scope。

**继续真实联调所需条件：** 管理员核对 dev Passport 的 `runcode-desktop` 注册与授权日志，保留已注册 callback `http://localhost:8199/oauth/callback`，允许 PKCE authorization_code，以及 manageapi scope 和对应 access-token audience。随后在隔离客户端正常重新登录，申请 `openid profile offline_access passportapi manageapi`；刷新旧登录不会追加 scope。无需把密码或完整 token 发到聊天。

完成上述条件后再核对实际账号与租户，验证“数字员工01”的目录→详情→retrieve、文档/分片 ID、真实 SearchMore 和模型依据。本人私有、他人私有、共享、白名单、跨租户、停用及撤权分别均为**真实环境未实测**；本地 ACL 测试不能替代这些结果。

## 可复用的隔离测试入口

本机目录 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/no-engine-task7/` 保留脱敏 HTTP 结果、真实配置简报与启动脚本：

- `Start-Task7-Bridge.ps1`：准确 JAR 的真实服务配置，本地端口 18207。
- `Start-Task7-GuiFixture.ps1`、`Initialize-Task7-GuiProfile.ps1`、`Start-Task7-GuiClient.ps1`：仅模拟、本地端口 18208、独立 gui-profile/gui-workspace；启动客户端时核对 EXE SHA256。人工操作可显式传 `-ShowWindow`。
- `gui-fixture-readme.md` 与 `gui-fixture-contract-results.json`：8 项模拟 HTTP 契约检查通过；KB-TEST-7421 为合成非敏感事实，不代表真实知识库已验证。
- `task7-environment-evidence.md`、`read-only-results.json`、`service-configuration-results.json`：真实环境可复查证据，不含完整令牌或私有正文。

Wails 目录 IPC 没有 AbortSignal；关闭后取消尚未发出的防抖调用并丢弃在途返回，已发 HTTP 依赖宿主 10 秒超时。冻结库标签恢复时显示 ID，来源文档标题保留。这些限制不应表述成尚未实现的工具循环能力。
