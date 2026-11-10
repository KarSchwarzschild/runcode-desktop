# Stage 2：真实知识库接口与桌面接入

更新：2026-11-04。Stage 2 本地实现、自动测试和 Bridge 启动验证已完成；真实上游权限与个人知识库联调尚未验收。本轮没有部署线上服务。

## 本阶段效果与边界

Stage 2 将桌面“+ → 知识库”接到 Bridge 的真实目录和逐库授权接口。正常登录后，在已配置并部署本阶段接口的测试环境里，可以搜索、滚动选择最多 5 个可访问库；名称、简介、平台目录路径及已知文档数来自经过授权的摘要。引用仍是输入框内的小号灰色标题，可像普通编辑内容一样删除、剪切、撤销；点击有效标题会再次授权并打开基座对应库目录。

Bridge 检索接口可独立返回已授权文段。桌面带库提问继续明确阻止，直到 Stage 3/4 将资料接入 agentloop 的上下文准备流程。这个阶段不能证明 LLM 已使用知识库回答。

| 仓库 | 本阶段分支 | 范围 |
| --- | --- | --- |
| 智开桌面端 | `feat/knowledge-base-chat` | 产品提交 `7803171`、`b6f5d37`；实际登录身份、目录/详情、选择恢复、标题导航 |
| Bridge | `feat/knowledge-base-api` | 提交 `a91e745`；受授权的目录、详情、检索及部署配置 |
| agentloop | `feat/question-context` | 本阶段不改动，保留 Stage 0 起点 `833be623` |

## 服务端调用与权限

桌面只调用显式租户路由：

```text
GET  /t/{tenantId}/v1/knowledge-bases?query=&page=1&pageSize=10
GET  /t/{tenantId}/v1/knowledge-bases/{libraryId}
POST /t/{tenantId}/v1/knowledge-bases/retrieve
     {"libraryIds":["已授权的库 UUID"],"question":"测试问题"}
```

Bridge 验证 Passport 身份和租户后，从管理 boundary 的 `AllPaging` 读取候选，对每个库调用 `GET /KnowledgeBase/Library/{id}` 的 `ACCESS_RESOURCE` 权限入口。它还检查返回 ID、所选租户、启用、未删除和非应用标注状态。检索前重新验证全部库，再向内部 `/Library/SearchMore` 发出服务端构造的固定请求；不接受客户端指定用户、角色、文档过滤、模型或上游 URL。

过滤分页使用 `hasMore`，未知授权总数为 `total=-1`。单页全部被过滤时仍可继续下一页；文档数未知时不显示“0 篇”。目录 URL 由 Bridge 在授权后按固定 `https://ai.ouchn.edu.cn/knowledge-base/{id}/data-collection` 路由生成，桌面再次验证，显示的路径是平台页面路径。

已检查的 boundary 源码允许同租户成员读取 PRIVATE 库。这是待部署方确认的现有政策，不能解释成“仅本人可读”。报告中租户 API Key 支持的 `__document_id` 过滤只能缩小搜索范围，本身没有提供用户授权的文档清单；本实现不以它替代权限接口。

## 真实联调仍需的环境资料

已知线上 Bridge `https://bridge-aibase.ouc-online.com.cn` 尚未包含本阶段知识库接口。此次修改保留在本地分支，不自动部署。

部署维护者需要提供或在服务端配置：

1. 承载上述逐库权限接口的管理 boundary 基址、实际版本，以及它对桌面 Passport access token 的 issuer、audience、scope 信任配置。
2. Bridge 可访问的内部 KnowledgeBase 服务基址、部署版本及可选内部服务认证。内部密钥只放服务环境。
3. 确认同租户 PRIVATE 的读取政策，提供一个可访问库和一个无权或已禁用库用于联调。
4. 测试 Bridge 的有效 OAuth callback；桌面 Bridge 基址会影响回调，任意本地地址不一定已在 Passport 注册。

无需在聊天中提供密码、Cookie、access token 或 refresh token。用户继续通过客户端正常登录。

## 如何检查每次修改

本次测试 EXE：[智开 Stage 2](C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main/cmd/runcode-desktop/bin/stage2-preview/智开.exe)。构建时间 `2026-11-04 14:33:27 +08:00`，产品版本 `1.0.20`，40,617,472 字节；SHA256：`797FC957A4F3FC27E7EC0A8529527F895368D974FDF380D24EA568B63E0082D9`。源码产品提交 `b6f5d37`，使用 `GOWORK=off` 和固定引擎 `v0.16.0`；不是本地引擎覆盖构建。构建清单为外部日志目录中的 `stage2-desktop-build-manifest.json`。

1. 保存草稿并正常退出旧智开实例，包括托盘中的进程，再打开本次报告列出的独立测试包。同品牌单实例会让新启动命令唤醒旧包，必须核对产物路径和版本。
2. 默认真实模式：正常登录并选择租户，打开“+ → 知识库”。测试 Bridge 就绪后应看到授权的真实库；接口未部署或配置未启用时应看到明确错误，不出现伪装成真实库的示例。
3. 在真实测试环境选择库，检查紧凑摘要、超过 10 项继续滚动和最多 5 项；在正文中删除、剪切和撤销引用。点击有效标题应打开对应库目录；拖选或删除不应打开网页。
4. 切换会话、重开会话、退出登录再换账号；确认引用不串账号/租户。由维护者撤权后刷新，旧引用应可删除但不可继续导航。服务暂时不可用时仍可清空旧选择。
5. Bridge 可独立测试检索，检查正常命中、合法空结果、无权、禁用、跨租户、超时。带库桌面发送仍会提示上下文尚未接入；移除并保存全部引用后可测原有聊天。

仅看界面时，可在测试构建启动前设置 `RUNCODE_KNOWLEDGE_MODE=demo` 使用隔离的合成目录；它不证明真实账号、权限或检索可用。正常真实模式与 demo 的保存范围分开。

## 验证记录

| 检查 | 结果与限制 |
| --- | --- |
| 前端完整测试 | 46 个文件、569 项通过；最终日志 `stage2-frontend-delivery.log` |
| 前端类型/lint | 通过；最后的标题缓存与重新确认修订后也重新执行 |
| Go 根模块 | `GOWORK=off go test -race ./...`，19 个测试包通过；日志 `stage2-desktop-go-test.log` |
| Go 修订后专项 | `go test -race ./internal/desktop -run TestKnowledge -count=1` 通过，13.918 秒；覆盖最终移除修复与分页大小调整 |
| 桌面 Go module | `GOWORK=off go test ./...` 通过，含实际 icongen 执行 |
| 协议与编译 | `GOWORK=off go run ./tools/protogen --check`、`go build ./...` 通过；协议注释更新后重新生成，没有手改生成文件 |
| Windows 独立包 | 品牌构建与 `--build-info` 退出 0；名称智开、bundleID `cn.ouconline.ai.zhikai`、版本 1.0.20 核对通过。日志 `stage2-desktop-build-delivery.log`；既有大 chunk 和重复 Windows manifest 提示仍存在 |
| Go lint | 全量退出 1，剩余 334 个未改动 CRLF 文件的 gofmt 报告，与前阶段基线一致；本轮新增的 11 项诊断已修复，不宣称全量 lint 通过 |
| Bridge 完整测试 | Maven 146 项，0 失败、0 错误、0 跳过；`target/stage2-final-full-test.log` |
| Bridge 打包与本地启动 | JAR 打包成功；在 `127.0.0.1:18199` 启动，`/actuator/health` 返回 200/UP，匿名知识库请求 401；验证后停止本次测试进程 |
| 独立审查 | 修复过期令牌下移除被阻断、分页不可用被当作结束、租户元数据无大小上限，以及移除后名称/重试入口缺失；复审无遗留问题 |
| 真实上游联调 | 未执行；线上匿名 401 仅证明入口要求认证，不能证明新端点已部署或个人库可见 |
| 真实桌面交互 | 未通过验收。Windows 自动化使用测试 EXE 路径启动后，窗口与进程实际指向 `C:/Program Files/Ouc/desk_agent/智开.exe`；没有将旧版计入验证。正常关闭操作及一次重新激活后重试均报 `window is minimized`，未强制结束旧实例。需手动正常退出旧版，再直接双击上述独立包检查鼠标、系统中文 IME 和真实目录跳转 |

Bridge 构建包：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/worktrees/ouconline-ai-bridge/target/ouconline-ai-bridge-0.1.0-SNAPSHOT.jar`，SHA256：`FE07F72B1C63FE3E00E3B2BD11BCE0DC0EAD091B7927F4EB983B48DC5DD4B18A`。具体服务配置与 curl 示例见 [Bridge 配置说明](C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/worktrees/ouconline-ai-bridge/docs/knowledge-base.md)。默认知识库开关关闭；服务端地址、真实 JWT 信任和 ACL 政策确认后再开启。

所有桌面日志位于 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/`。测试使用本地合成身份和受控上游；没有复制聊天中的账号凭据，没有修改 .NET、ai-platform-java 或 agentloop，也没有推送、合并或发布。用户自己的 `docs/start.bat` 保持未跟踪且未修改。
