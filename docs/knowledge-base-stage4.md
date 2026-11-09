# Stage 4：桌面、Bridge 与引擎接线

日期：2026-11-05（Asia/Shanghai）。阶段范围对应原计划 Task 3、4、5 及 Task 6 正向场景。产品接线与本地自动化已实现；真实服务及 Windows 交互验收状态单独列出，不能把合成模型通过当作线上问答验收。

## 实现行为

- 当前会话最多选择 5 个知识库，保留 Stage 2 的输入框内引用交互。发送成功后选择保留，每个新问题重新检索。
- Go 在准入时冻结库与可信账号/租户/知识库端点边界，将选择交给 agentloop 的持久化问题记录。原问、准备状态和引用元数据使用引擎日志恢复，不另外缓存检索正文。
- 引擎在实际主模型调用之前准备资料；steering、模型传输重试、失败、取消及恢复复用 Stage 3 生命周期。桌面关闭/换账号后拒绝迟到响应。
- 检索正文仅作为有明确不可信标记的参考材料进入模型预算。界面只接收库摘要、状态与来源标识，不接收正文。只有实际应用的有效来源才显示“已向模型提供参考资料”；合法空结果始终显示未检索到资料。
- 状态按 sessionId/questionId 绑定；事件早于 receipt 或停止操作时不会串到另一题。停止先于首个检索事件到达也不会被迟到事件重新显示为检索中。
- 重新生成默认沿用原题库快照并重新检索；修改重试可显式清空/替换选择，新分支继承该选择，来源会话不改。取消恢复原草稿，接受回执不会抹掉后来输入的新草稿。编辑重试期间不提供无法按来源会话授权的库目录快捷跳转。
- 初期只支持通行证平台模型。有知识来源的历史在清空选择后仍保留目的地约束，切到自定义/Codex 连接会拒绝。辅助调用沿用同一个可信平台 provider，并在调用前重新校验历史库权限。

OpenAI 适配器会合并相邻 user 消息。因此 HTTP wire 中可能是原问后接明确的 `Untrusted reference material` 分隔段；引擎输入、可编辑原问和持久化原问仍保持原文，参考材料不进入用户授权证据。没有在桌面端把正文拼进用户问题。

## 本地来源与服务配置

| 组件 | 来源 |
| --- | --- |
| 桌面 | `feat/knowledge-base-chat`，Stage 4 基线 `46734d1`；工作区 `C:/Users/yyzxc/.codex/worktrees/knowledge-base-stage4/runcode-desktop-main` |
| agentloop | `feat/question-context`，`3ae3dcd`；增加持久化前取消保护，现有 `.codex-stage0/worktrees/agentloop` |
| Bridge | `feat/knowledge-base-api`，`1cde2ab`；按用户指定的 .NET 源码重写直连适配，旧 management 改动已保存在任务备份中 |

构建显式使用私有 `stage4.go.work` 联动上述引擎 checkout，并使用品牌脚本 `--brand zhikai-guokai --test --local-engine`。没有修改正式 go.mod 的 `v0.16.0` 依赖，也没有把本地路径作为发布配置提交。真实固定 tag 和 `GOWORK=off` 发布验证属于 Stage 5。

用户于 2026-11-05 指定测试环境后，本轮不再使用旧 production management 配置。测试路径如下：

| 连接 | 地址与职责 |
| --- | --- |
| 客户端 → 本机 Bridge | `http://127.0.0.1:18199`，模型、租户和知识库请求都走本机 |
| Bridge → .NET 知识库 | `http://123.249.28.107:15003`，`/Library` 目录、详情及 `SearchMore` |
| Bridge → 平台管理 API | `https://aibase-preview.ouc-online.com.cn/api`，携带用户令牌验证租户成员关系 |
| Passport | `https://passport-ai-dev.ouc-online.com.cn`，正常浏览器 OAuth/PKCE 登录 |
| 知识库网页 | `https://aibase-preview.ouc-online.com.cn`，由可信部署配置生成目录链接 |
| 模型服务 | `http://123.249.28.107:15001`，地址依据 Bridge 现有测试部署配置 |

`RUNCODE_BRIDGE_BASE_URL` 指向本机；`RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL` 默认继承该地址。`RUNCODE_PASSPORT_REDIRECT_URI` 可单独指定测试 Passport 已注册的 OAuth relay，使 API 地址与回调注册互不混淆。现有 launcher 使用 `https://bridge-aibase.ouc-online.com.cn/oauth/callback` 中转，本轮测试环境的注册兼容性仍需正常登录成功确认。`RUNCODE_KNOWLEDGE_PORTAL_BASE_URL` 只接受部署配置的 HTTPS origin；实际库目录 URL 仍须逐字符符合该 origin、库 GUID 与固定路径。

测试客户端使用独立 `test-profile`，与日常配置隔离；登录凭据仍由客户端 DPAPI 加密保存。普通 Go 测试在进程级临时配置/缓存目录运行，防止异步测试刷新在单项测试清理后写入日常登录文件。

## .NET 契约与身份边界

参考源码为 `C:/Users/yyzxc/Desktop/Client-Platform/OUCOnline.AI.KnowledgeBase`。该目录不是 Git checkout，因此按文件与行号记录契约，不编造源码 commit。

- `LibraryController.cs` 路由前缀是 `/Library`；`FindLibraryModel.UserId` 是查询参数，由验证后的 Passport `UserId` 填写。租户与用户 ID 是字符串，知识库与数据集 ID 是 GUID。
- `LibraryDomainService.LibraryAllPaging` 的用户、共享和启用状态过滤目前被注释。目录结果只能作为候选，不能当作用户已获授权。Bridge 统一检查所属租户、启用、未删除、非应用标注及所有者/共享规则，并在检索前重新检查全部所选库。
- .NET 枚举含义：Private=0 仅所有者；TenantShared=1 与 PublicShared=2 在本期所选授权租户内可读；SpecifiedUsersShared=3 检查用户白名单。所有者保留对自己库的访问。本期不推断跨租户公共访问或管理员提升权限。此规则替换旧管理接口“同租户即可读他人私有库”的假设。
- 检索仅调用 `POST /Library/SearchMore`，服务端构造 `libraryIds`、原问题、`maxMatchesCount=5`、`minRelevance=0`。不接收前端 userId、数据集选择器或临时文档选择器。
- 来源中的文档 ID 对应 `CitationValue.DataCollectionId`；切片 ID 对应 `PartitionValue.DataSlicingId`，形如 `d=<documentId>//p=<filePart>`，不能当作 GUID 或用序号替代。返回库、文档与 tag 须一致。
- 直接服务使用 `X-Internal-Service-Key`，只保存在 Bridge 配置并发送给该服务。它不代表用户库权限。成功空结果与业务错误区分，内部服务认证失败不能误报成客户端登录失效；对外不透传上游异常正文。

## 验证记录

| 检查 | 结果 |
| --- | --- |
| 前端全量测试 | 49 文件 / 593 项通过，包含独立审查后的回归 |
| 前端 typecheck / lint / build | 通过；保留既有大 chunk 提示 |
| 根模块 `go test -race ./...` | 通过；桌面包 48.384 秒 |
| 服务端模块 `go test -race ./...` | 通过 |
| 桌面模块 `go test ./...` | 通过 |
| 三模块 `go build ./...` | 通过 |
| 协议 `go run ./tools/protogen --check` | 通过，DTO 与事件均由生成器输出 |
| Bridge 原 management 版本 | 156 项通过；属于旧实现证据，不用于证明本轮 .NET 重写已通过 |
| Bridge .NET 重写 | `mvn clean verify` 160 项通过，0 失败/错误/跳过；其中知识库 50 项，JAR 打包成功 |
| 本地真实引擎 + 合成 HTTP Bridge/模型 | 覆盖文段进入实际模型请求、原问、逐题检索、快照/重试、空结果、403/越界响应、取消/关闭/账号切换、历史目的地限制与来源回放 |
| Windows 品牌测试包 | 按测试环境最终重建通过，`--build-info` 为智开（国开版）1.0.20；客户端进程已启动 |
| 准入与退出取消修复 | 修复后根模块全量测试通过，桌面包 53.134 秒；最终重点 race 回归 3.230 秒通过 |
| 引擎取消修复 | 全量测试、重点 race 与 vet 通过；实际写入失败仍保留持久化不确定状态 |
| 独立审查 | Bridge 重写及桌面/引擎取消修复均无剩余确认的 Critical/Important/Minor 问题 |
| 本机测试 Bridge | PID 21268，仅监听 `127.0.0.1:18199`；健康接口 200/UP，未认证知识库请求 401 |
| Go lint | 存在已记录的 CRLF/gofmt 基线问题；新增诊断单独修复验证，不宣称全库 lint 清零 |
| 测试端点可达性 | Passport discovery 和平台首页 200；知识库 `/health` 200，未带内部服务密钥的业务请求 401 |
| 正常测试 OAuth | 已运行客户端正常登录流程；本轮等待 300 秒未取得回调，测试未通过，没有保存测试登录态 |
| 真实 Stage 4 服务/模型 | 尚未通过；缺少已配置的测试知识库内部服务密钥及正常测试账号登录 |
| Windows 原生鼠标/中文 IME | 尚未通过。Computer Use native pipe 在初始化、重试、重置后均不可连接 |

测试日志与 RED/GREEN/审查记录位于工作区 `.superpowers/sdd/2026-11-03-knowledge-base-chat/`。检索响应和原问不写入新增日志；可选真实验证只记录请求摘要、题号和校验结果。

## 可执行的真实验证

`internal/desktop/knowledge_live_test.go` 默认跳过。真实验证要求 dev Passport、本机模型与知识库 Bridge、带标记的独立测试配置目录。不能在打开 live 标志时执行整个单元测试套件。已有本地 launcher：

- `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Start-Stage4-Bridge.ps1`
- `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Start-Stage4-Client.cmd`
- `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Verify-Stage4.ps1`

先在本机配置 `BRIDGE_KNOWLEDGE_INTERNAL_SERVICE_KEY`，不要写入源码或聊天；再启动已验证 JAR。正常登录可通过客户端，或运行同一客户端 OAuth 方法的 opt-in harness：

```powershell
& 'C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Verify-Stage4.ps1' -Mode Login
& 'C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Verify-Stage4.ps1' -Mode Verify -TenantId '<测试账号真实租户ID>'
```

它从测试账号实际可访问目录解析“数字员工01”的 GUID，不沿用其他环境的固定 ID；同名库可用 `-LibraryId` 明确选择。测试在临时工作区验证两个已知问题、实际主请求中的原问和文段、有效来源、第二问重新检索与选库保留；输出仅含请求哈希和状态。自动语义检查只是已知事实的关键词核对，不能替代人工判断回答质量。

用户桌面检查步骤：

1. 启动本地测试 Bridge，再打开指定 Stage 4 测试客户端；核对窗口为“智开（国开版）”。
2. 正常测试通行证登录，选择该账号真实可访问的测试租户，新建测试会话。
3. 从“+ → 知识库”选“数字员工01”，问“创建工单前需要谁确认？”，检查来源和回答依据。
4. 继续问“资料缺失或制度冲突时应怎么处理？”，确认标签保留、重新检索；修改重试并检查新分支、来源会话与取消草稿。
5. 关闭并恢复测试会话，检查来源；新开无选库会话验证隔离。仅对合成资料启用完整上下文审核。

## 当前环境限制

用户已明确授权本机真实联调。旧 production 配置的 Bridge 启动曾两次被自动审批拒绝，工具只返回 `blocked by policy`；该旧配置已经停用。本轮使用新的测试配置成功启动本机 Bridge 与客户端，不再受该旧启动阻塞。测试知识库需要的内部服务密钥仍未配置，已向用户询问其本机配置位置；正常 OAuth 等待超时，客户端需重新正常登录。完成配置和登录后才能运行真实检索与模型验收，不能用健康检查、旧 Stage 2 报告或合成测试代替。

最终本机产物 SHA256：

- Bridge JAR：`2950348B0E724B11DFDB3F3722792BFE83E9613692AAFECF43231B2535807951`
- 智开（国开版）EXE：`C21B764C499DFA40A8BE7EA94D3649A4C7EE75F283318FF5907D5EC8CCBFF4CF`
