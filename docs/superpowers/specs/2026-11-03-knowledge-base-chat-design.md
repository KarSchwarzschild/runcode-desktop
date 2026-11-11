# 桌面对话知识库接入方案

创建日期：2026-11-03；更新日期：2026-11-04。状态：Stage 0/1 已执行；Stage 2 本地实现与自动测试完成，真实部署联调待验收。测试和人工验收边界见对应报告；Stage 3–5 尚未授权。

## 已确认的目标

- 在截图中聊天输入框左下角“+”菜单增加“知识库”，与技能、智能体、文件、图片附件并列。
- 选择后，知识库必须位于实际文本编辑器的内容流内，仅显示灰色、小字号标题，作为绑定稳定 ID 的原子引用；用普通 Backspace/Delete、选区删除或剪切移除，支持撤销/重做。取消文本框上方的独立 chips，不能只把它们移进卡片边框冒充可编辑内容。
- Stage 1 选择器需要显示知识库缩略信息、名称和路径：采用图标、名称、简介、逻辑目录路径、文档数量；合成目录明确标记测试数据，不伪造真实库链接。
- 用户本轮确认 Stage 1 紧凑交互：去掉右侧复选小框，点击整行选择并以行变色表示选中；标题下一行以灰字合并简介、目录和篇数，单行省略，悬停可查看完整内容。列表首批加载 10 项，滚动触底追加，无上一页/下一页控件；同一会话最多选择 5 个库。合成目录扩为 12 个明确测试示例，用于验证跨首批 10 项的滚动加载。
- 输入框内位置、灰色小字号标题及普通编辑删除属于本次 Stage 1。只有点击打开基座对应知识库目录的导航能力留待 Stage 2 真实目录接入后实施；链接由服务端提供并经可信白名单校验，编辑删除不得触发导航。Stage 1 合成引用无链接，不伪造实际目标。
- 用户已确认：选择在当前会话持续生效，每次提问重新检索，发送成功后标签保留。
- 用户提供的知识库页面为 `https://ai.ouchn.edu.cn/knowledge-base/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa/data-collection`，库 ID 为 `08df2f18-e3db-4ea4-8cdb-6ad67e9185fa`。已从浏览器页面确认当前标题为“数字员工01”；实现时仍从服务端获取规范标题、租户及访问权。
- 设计阶段交付执行顺序、待补资料和测试方法；后续获授权执行的 Stage 0 只准备分支、环境与原版检查，不实施知识库功能。
- 用户明确要求三套产品代码在独立功能分支修改：桌面 `feat/knowledge-base-chat`、Bridge `feat/knowledge-base-api`、agentloop `feat/question-context`；进入开发时先建立正确 Git 基线并创建分支，不直接在主分支或 detached HEAD 上实现。
- 交付按“基线与可运行环境 → 桌面入口和会话选择 → Bridge 真实目录/检索 → 引擎上下文机制 → 三端联调 → 完整回归及发布候选”分阶段；Bridge 与引擎在契约明确后可并行。各阶段提供真实可验证的产物，明确模拟数据与已联通能力，用户不限于检查前端外观。详细阶段、分支起点及验收边界见执行计划。

## 源码已确认的事实

桌面项目根目录：`C:/Users/yyzxc/Desktop/Client-Platform/runcode-desktop-main`。

| 事实 | 代码位置（相对项目根） |
| --- | --- |
| “+”菜单已有四项，可扩展入口 | `cmd/runcode-desktop/frontend/src/composer/toolbar.tsx:148` |
| 输入卡片含附件、文本和工具条 | `cmd/runcode-desktop/frontend/src/composer/index.tsx:378` |
| 切会话会重挂载 Composer，不能把选库只放组件局部状态 | `cmd/runcode-desktop/frontend/src/App.tsx:362` |
| 草稿按会话保存，但发送成功会清草稿 | `cmd/runcode-desktop/frontend/src/session/use-chat-drafts.ts:6`；`session/use-question-retry.ts:78` |
| 真实发送使用 SubmitQuestion，支持运行中补充及问题重试 | `internal/protocol/questions.go:25`；`internal/desktop/questions.go:108` |
| 原问题文本直接进入引擎；重试会读回该文本 | `internal/desktop/questions.go:31`、`:172` |
| 已有通行证登录、刷新、租户绑定及账号代际隔离 | `internal/desktop/passport.go:30`；`tokens.go:51`；`app.go:703` |
| 测试版可观察实际发往模型的上下文 | `internal/desktop/contextaudit.go:3`；`testbuild.go:3` |
| agentloop 是独立依赖，现已取得同级 `../agentloop` 的 `v0.16.0` 源码 | `go.work:7`；`go.mod`；引擎 commit `833be6238b4211fc6b9c23497cc2ff91298b74f5` |

.NET 根目录：`C:/Users/yyzxc/Desktop/Client-Platform/OUCOnline.AI.KnowledgeBase`。

| 用途 | 已有服务内部接口 | 证据 |
| --- | --- | --- |
| 列表/分页 | `GET /Library/{tenantId}/List`、`/{tenantId}/Paging` | `OUCOnline.AI.KnowledgeBase.API/Controllers/LibraryController.cs:117`、`:131` |
| 详情及标题 | `GET /Library/{id}`，标题字段 `Name` | 同文件 `:94` |
| 检索文段 | `POST /Library/{id}/Search`、`POST /Library/SearchMore` | 同文件 `:163`、`:175` |
| 已有完整问答 | `POST /Library/{id}/Ask` | 同文件 `:188`；本功能继续使用桌面所选模型生成回答 |
| 检索响应 | `Results[].Partitions[].Text`，含库/文档/分片 ID、来源名、相关度 | `Abstractions/Values/SearchResultValue.cs`、`CitationValue.cs`、`PartitionValue.cs`（目录前缀均为 `OUCOnline.AI.KnowledgeBase.`） |

这里的 `/Library` 是服务内部代码路由；网页 `/knowledge-base` 是前端页面。用户提供的 Network 记录已确认分片分页的公网入口为 `https://ai.ouchn.edu.cn/api/KnowledgeBase/Library/DataCollection/{id}/DataSlicing/Paging`。该证据只固定此接口，不能据此假定所有内部路由都已公开，也不能仅凭兼容路径判断线上运行 .NET 或 Java。

### 五套代码的职责

| 本地目录（均在 `C:/Users/yyzxc/Desktop/Client-Platform/` 下） | 职责及本功能改动边界 |
| --- | --- |
| `runcode-desktop-main` | 实际桌面产品代码：React/TypeScript 输入框、Go/Wails 桌面宿主，另有 CLI/TUI 和服务端骨架；增加菜单、标题标签、会话选库、检索服务接线及引用展示。 |
| `agentloop` | 通用智能体引擎：问题准入、工具循环、模型请求、预算、历史、取消；增加通用的逐题参考资料准备/投影，不引入知识库业务或服务地址。 |
| `Bridge/ouconline-ai-bridge` | 独立 Java 桥接服务；验证 Passport JWT，校验租户，将身份转换后访问 AI.Core，另有 OA 等接口；拟增加面向桌面的知识库适配。它不是桌面前端的 Wails bridge.ts。 |
| `OUCOnline.AI.KnowledgeBase` | .NET 知识库服务：库/文档元数据、分片、向量检索、SearchMore；尽量复用已有检索。 |
| `ai-platform-java` | 平台后端的 Java 实现，包含 boundary、knowledgebase、cloud 等服务；有用户资源授权与 RAG 参考实现，但不能据此认定当前线上已经切换到 Java。 |

Java 核查版本为 `8920a0efc19b6718c1036e4b0769329fecd8d00d`。`ai-boundary/.../controller/KnowledgeBaseLibraryController.java:101` 的 `GET /KnowledgeBase/Library/{tenantId}/Paging` 注入当前用户；`:63` 的详情和 `:117` 的单库 Search 使用 `ACCESS_RESOURCE + LIBRARY` 授权。真实 `/api` 前缀由部署决定。

需要特别确认实际库可见性政策：`ai-boundary/.../authz/TenantAccessAuthorizer.java:61` 当前允许同租户成员读取资源，也允许公开分享/指定用户等路径，不能描述成“私有库必定只有 owner 可读”。已有授权服务可复用，但 `Enabled`、所选租户归属和每个库的权限仍要按确认后的契约检查。内部 Java SearchMore 也不是用户授权入口。

Java `ai-cloud` 的 `CloudKnowledgeSearcher` 与 `ChatEnricher` 已实现检索和上下文组装，但公开检索路由使用 AppSecret/AppSharing 和应用配置的库集合，不等价于桌面用户自由选库。此次不改用该问答链替换桌面引擎。

Bridge 根目录为 `C:/Users/yyzxc/Desktop/Client-Platform/Bridge/ouconline-ai-bridge`，技术栈为 Java 21 / Spring Boot 4.0.1。其 `src/main/java/cn/ouconline/ai/bridge/` 下已核实：

- `config/SecurityConfig.java:27` 保护登录请求，`:55` 配置 issuer/JWKS、有效期及 token typ 校验；本地代码未见显式 audience/scope 校验，不能声称与 Java boundary 的 manageapi 校验已完全一致，接入时核对两侧真实认证契约。
- `proxy/ProxyController.java:159` 的租户路径验证用户成员关系；`me/TenantAccessService.java:58` 从 Core 管理租户和成员表取得可用租户，按用户缓存 60 秒。此权限只到租户层，不能代替库授权。
- `proxy/AiCoreAuthorizationBuilder.java:23` 将 Passport 身份转换为 Core 的受信 JSON Bearer；它不是知识库服务的通用鉴权协议，不能不经核对就复制到 SearchMore。
- 当前没有知识库目录、SearchMore 或 library ACL 接口。建议参考 `oa/OaController.java`、`OaClient.java` 的独立模块组织，新增 knowledge Controller/Service/Client/DTO；不直接使用 `ProxyService` 无条件透传内部检索结果。
- 新知识库端点只提供显式 `/t/{tenantId}/...` 形式，先校验租户，再通过权威库权限服务逐库授权；授权查询失败时拒绝检索。不要沿用现有非作用域 `/v1/**` 的默认租户逻辑，也不要把模型目录的 tenant-only 缓存当作用户库 ACL。

### 本次分片样例的准确含义

用户提供的两个 JSON 是同一分页数据的内层对象和带 `authorization/stateCode/state/message/data` 的外层包装，并不能证明一个来自 Bridge、另一个来自 agentloop。

- 已知分片详情前端页面为 `https://ai.ouchn.edu.cn/knowledge-base/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa/data-collection/08df2f18-f135-42dd-8590-c21f82a1dfca/slice-details`；其实际分片 API 已由后续 Network 记录确认，检索 API 尚待确认。
- 本轮补充请求中的 `grant_type=refresh_token`、`client_id=aimanage` 表示网页登录续期，不能作为分片/检索接口契约；不记录或复用其中的任何凭据。
- 用户已提供实际请求：`GET https://ai.ouchn.edu.cn/api/KnowledgeBase/Library/DataCollection/08df2f18-f135-42dd-8590-c21f82a1dfca/DataSlicing/Paging?pageNum=1&pageSize=8`，浏览器状态为 `200 OK`；结合已提供的成功业务包装和两段正文，分片分页契约已得到样例证据。此处是用户提供的观测，并非本轮主动调用 API 的测试结果。
- 对应 .NET 代码形状为 `GET /Library/DataCollection/{id}/DataSlicing/Paging`（Controller `:656`，DomainService `:2151`）。方法按文档枚举分片，没有问题参数，也未调用语义检索；不能作为每题按相关性检索的替代。
- Network 的 `Remote address=127.0.0.1:10808` 是浏览器连接的本机端点，通常来自本地代理/转发；不代表知识库服务器地址，也不应写成 Bridge 或桌面知识库 API 基址。业务地址以 Request URL 的 HTTPS 域名为准。
- 知识库 ID 是 `08df2f18-e3db-4ea4-8cdb-6ad67e9185fa`；样例 `dataCollectionId` 为 `08df2f18-f135-42dd-8590-c21f82a1dfca`，属于文档/数据集 ID，不能代替库 ID。
- 两段 `text` 证明已经能读取文档分片。`relevance:0` 是此 DTO 的默认值，不能据此判断向量召回质量。
- 该分页方法没有填充 `Tags`，而 `PartitionValue.DataSlicingId` 根据 tags 计算，因此出现 `d=//p=`。此样例应使用已填充且验证过的 `id` 标识分片，不能把占位 `dataSlicingId` 当唯一来源。真实 SearchMore 分片还需验证其 tags/ID；无有效平台分片 ID 时可用已验证的库、文档、分片序号和内容摘要生成本次引用键，不能伪造平台分片链接。
- 样例没有库标题，`file` 是生成的文件名。库标签仍从库详情 `Name` 读取；文档标题优先使用原始元数据/SourceName，缺失则如实回退文件名。
- `/download?...` 是相对地址，需要确认可信服务基址及授权方式；不能直接以桌面 localhost 拼接或当成匿名公开链接。
- 此文档中的工单办理等内容是待检索的知识材料，不扩大本次功能范围。此次不开发工单系统。

## 必须补齐的接入条件

1. **可信的用户授权入口。** 单库/多库检索方法按传入 ID 取库，方法内未见当前用户、租户成员、共享规则和 Enabled 的完整校验；`AllPaging` 的相关权限过滤有被注释的代码（`DomainService/LibraryDomainService.cs:371`、`:499`、`:555`）。生产全局认证过滤器来自外部包，不能据此断言线上完全无鉴权，也不能假设它已完成每个库的业务授权。
2. **确认线上路由和响应包装。** 需要网站管理 API/网关或 Bridge 的接口契约。`APIReturnInfo` 来自外部包，HTTP 200 可能仍是业务失败；应以真实脱敏响应固定解析规则。单库 Search 把“未匹配到结果”当业务错误返回，适配层需规范为空结果而非网络故障。
3. **补充引擎的每题上下文接口。** 已核查 agentloop `v0.16.0`：现有 UserPromptSubmit 只覆盖新回合，遗漏 steering，命令 hook 失败会继续运行；LLMRequestObserver 是只读且在预算准入后。因此本方案需要独立引擎改动，加入通用的受管上下文扩展、失败题投影语义、测试和真实版本 tag，而非继续等待源码。
4. **Git 开发基线已于 Stage 0 恢复。** 原先无 HEAD 的桌面目录经备份、fetch 和 mixed reset 建立上游 `1a7d819c` 基线，产品文件保留；已在隔离工作区创建 `feat/knowledge-base-chat`。Bridge、agentloop 分别为 `feat/knowledge-base-api`、`feat/question-context`，实际路径与起点见 [Stage 0 报告](../../knowledge-base-stage0.md)。

## 建议的产品行为

会话持续生效、Stage 1 紧凑选择器、最多 5 库、真实编辑器内原子引用及 Stage 2 才实现的标题导航已由用户确认；其余后续行为仍按设计建议与对应阶段契约核实。

- 首期支持同一会话选择 1–5 个当前租户下有权使用的库；新会话为空。指定库只作为初始验收对象，不写死在全局配置中。前端、Go 和后续服务端校验均使用 5 库上限，重复 ID 去重后计数，第 6 个拒绝且保留原已确认状态。
- 选择器支持按名称、简介、目录搜索；底层保留 page/pageSize 分页 API，界面首批 10 项、触底追加下一批，无上下页按钮。整行点击切换选择、选中行变色，不显示右侧复选小框；标题下一行以单行灰字显示简介、目录、篇数，溢出省略并可悬停查看全称。加载、无库、无结果、失败重试分别显示。
- Stage 1 引用直接放在真实文本编辑器中，仅显示灰色小字号标题；引用本身是原子对象，光标可位于其前后，也可与普通文字一同进入选区。Backspace/Delete、选区删除及剪切移除引用并保存会话选择，撤销/重做同步恢复或移除引用。重名库按 ID 区分，标题由后端返回，按文本安全渲染；引用标题和 ID 不进入用户正文 payload。
- 用 Lexical 替换 textarea 输入叶子，保留 Composer 工具栏、附件、选择器和原业务流程。编辑器提供明确的聚焦/选区 imperative handle，另有真实 DOM ref 供尺寸测量；正文文本与引用节点分开投影，保持 IME、换行、mention、场景占位符选区和修改重试。选库仍独立于正文草稿，不能因替换文本或清空发送草稿而隐式丢失。
- 异步 CAS 选择队列合并快速连续删除、撤销或重选的最新意图；未确认/保存期间阻止误发。失败回读并恢复后端权威引用，同时保留普通正文编辑，显示可理解的失败与重试状态；A→B→A、换账号和卸载使迟到结果失效。前端不成为实际权限或发送准入的权威。
- Stage 2 接入真实目录后才为上述内联标题增加导航，点击目标为服务端提供并校验的该库目录页；编辑删除与导航分开，不能因删除引用而打开页面。
- 会话选择由 Go 后端保存；前端是按 sessionId 的镜像。文字/附件发送后清空，选库不清空；重开同一会话时重新校验后恢复。
- 每次接受问题冻结选库快照。检索中更改选择只影响后续问题；停止、关闭会话、登出、重建会话会取消在途工作，迟到响应不能启动回答。
- 普通发送、运行中补充、重新生成、修改后重试均需覆盖。重新生成沿用原问题的选库快照并重新检索；新分支继承本次选择，来源会话不变；取消编辑恢复原草稿。
- 没选库时保持现有聊天。选库但没有文字问题时不发起检索。
- 有有效命中才显示“已参考知识库”及来源。无命中显示“本次未检索到相关资料”，允许普通回答但明确缺乏知识库依据；鉴权失败、库禁用/删除、检索超时则停止本次回答。准入前失败保留原草稿；问题已接受但准备失败时，保留失败题的原文/选库快照并显示“重试/修改后重试”，不覆盖用户新写的草稿、不自动重复提交。用户可移除库后明确重试。
- 移除标签停止后续检索，不抹除历史回答里已经出现的信息。要让模型完全不沿用此前知识库信息，应开新会话；界面不能承诺“移除就遗忘”。
- 首期默认仅用于已有通行证的平台模型；自定义第三方模型、Codex 连接及 OA 锁定会话的组合暂不开放，待明确允许的数据目的地及现有锁策略后再扩展。该范围是建议，需要业务方确认。

## 推荐链路

```mermaid
flowchart LR
  A[加号 → 知识库 → 输入框内原子引用] --> B[按会话保存选择]
  B --> C[提交原始问题并冻结选库]
  C --> D[引擎接收问题后的可取消上下文准备]
  D --> E[Go 使用现有通行证调用 Bridge]
  E --> F[Bridge 校验用户 租户 每个库权限]
  F --> G[知识库 SearchMore 检索分段]
  G --> H[过滤 去重 限额 引用编号]
  H --> I[独立参考资料进入模型请求预算]
  I --> J[原有 LLM 流式回答及来源]
```

网页管理地址只用于“打开知识库管理/查看来源”，不抓取网页全文作为检索，不在输入正文中仅写标题来替代库 ID。

### 鉴权与服务适配

- 使用桌面现有 OAuth PKCE、TokenSource 和刷新逻辑。网页 `aimanage` 客户端的临时令牌不写入程序；桌面使用自身 `runcode-desktop` 客户端。`id_token` 不作为业务 API 凭据。
- Bridge 从经验证的登录态推导用户身份，验证选定租户成员关系及每个库的所有者/共享权限、Enabled 和删除状态。不得信任前端传入的 UserId、UserRole、标题或 TenantId 来证明授权。
- 优先复用现有管理 API 的统一权限判断；Java boundary 已有候选实现，但需确认它是否部署以及同租户/共享规则是否符合实际产品。若没有可用入口，先在服务端补充统一授权查询，不在桌面端实现一份可绕过的 ACL。
- KnowledgeBase 的 `X-Internal-Service-Key` 如启用，只留在服务端配置；它只保护内部调用，不代表用户访问权。
- 适配层只构造本功能需要的 `LibraryIds, Question, MaxMatchesCount, MinRelevance`，禁止透传 `DataCollectionIds/TemporaryDocumentIds/任意 Filters` 扩大检索范围。每个库都授权通过才查询，权限失败不静默丢弃后继续。
- 返回的每个命中再次校验库 ID 属于已授权集合；缺失 ID、越界 ID、异常正文大小都不能直接进入模型。
- 目录缓存按 Bridge、账号代际、租户隔离；每次检索重新校验访问权。来源 URL 使用平台可信路由，禁止把带凭据 URL 或任意协议链接交给模型/浏览器。
- Stage 2 的库目录链接由服务端返回，桌面只接受可信白名单内的 HTTPS 平台目标，并验证对应库 ID；不得从合成测试 ID 拼接链接。已知实际目标示例为 `https://ai.ouchn.edu.cn/knowledge-base/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa/data-collection`，它说明目录页形状，不授权任意其他 URL，也不替代当前用户访问权校验。

### 上下文、历史与生命周期

- 用户原始问题、选库元数据、检索参考资料三者分开。参考资料不进入“直接用户授权证据”，不能要求执行其中的指令。
- 推荐引擎在问题原文 durable ack 之后、`armReasoning` 之前运行通用上下文准备接口（`internal/repl/session.go:564–584`），使用受 host 管理的取消 context。该位置避免准备失败后仍调用辅助模型；steering 的两个消费点也必须统一接线。不要利用只读 `LLMRequestObserver` 修改请求，也不要把每题片段塞进会话固定 SystemPromptAppend。
- 引擎扩展通过 `engine.Options → build.go → repl.SessionOptions` 注入宿主端口，参考现有 `imageinput.Router.Prepare/Project`：可取消的准备只执行一次，无 IO 投影生成请求副本。元数据贯穿 `turn/turn.go`、`host/questions.go`、`host/manager.go`，按实际 questionId 绑定；不能只在闭包中读取当前选库。
- 真正请求 `internal/repl/stream.go:39`、压缩后的请求替换 `context_request.go:58` 与预算 sizer `:81` 使用同一投影。模型传输重试复用已准备快照；用户重新生成/修改后重试由 ForkQuestion 创建分支，再由 SubmitQuestion 分配新问题 ID，保留来源关联并重新检索，不复用旧 ID。
- 注入的片段要参与实际 token 预算和压缩规则，且按问题标识固定一次；同一回合多次工具迭代不能重复检索/重复堆叠。
- 库数上限按用户确认调整为 5 个；其余初始建议预算仍为总计 5 个有效分片、单片 4,000 字符、合计 12,000 字符和 4,000 估算 tokens 的双重上限，且必须小于该请求的剩余上下文预算；列表 10 秒、检索整体 30 秒、响应体 1 MiB。这些是接入上限，不是已测性能指标。
- 初始以用户当前文字为查询；不默认发送完整聊天记录。指代追问的查询改写属于后续优化，测试时应使用能独立理解的问题。
- `MinRelevance` 先沿用服务默认 0 做样本观察，生产值由至少 10 条相关/不相关问题校准后固定；不以未校准分数宣称召回准确率。
- 默认仅持久化选择、问题原文及引用元数据，不另建长期原文缓存；检索片段用于当前问题的受管上下文。恢复/重试重新校验权限并检索。主模型已生成的历史回答仍按原有历史机制保存。
- 历史、分支和删除需支持附加元数据；真正旧格式的会话字段缺失按无知识库处理。采用新上下文格式的记录如缺少快照/状态则拒绝执行，不能按旧格式降级。若实际引擎将附加上下文存入 journal，应有明确的来源标记、重新授权和新请求投影规则，不能退化成普通用户消息。
- 取消要覆盖准备期和生成期；服务端应继续传递 RequestAborted/CancellationToken。运行中补充按各自 accepted questionId 绑定资料，在实际进入迭代边界时注入，不共用“当前检索结果”全局变量。
- 生命周期区分 accepted、retrieving、ready/empty、generating、failed/cancelled。receipt 仅表示问题已接受，不代表检索成功；准备失败也必须结束 busy。事件按 questionId 关联，早于 receipt 的事件暂存后再绑定；失败重试走既有 ForkQuestion/GetQuestion 路径建立新尝试。
- durable receipt 必须覆盖原问、冻结的上下文引用/选库快照以及待准备标记，或提供等价的可恢复事务协议。不能先只确认原问落盘，再单独写 sidecar：中间崩溃会让待检索题失去标记。需测试元数据写失败、确认前后崩溃和 raw log 恢复；未完成准备的记录只能作为待恢复/失败题，不能隐式进入普通模型输入。
- 准备失败/取消的题保留在可见历史中，但不进入后续模型请求的普通输入投影。运行中补充的检索失败时，停止该回合剩余生成并明确提示；先前已显示的内容保留。“失败时不调用模型”指不发送包含失败题及其资料的新请求，不否认补充前的模型调用。
- 带参考资料的摘要/子代理/标题等辅助模型同样受数据目的地策略限制；不能只检查主模型。默认不向自动标题、审批裁判附加知识文段。
- 一旦知识片段或据此生成的回答进入会话历史，数据目的地约束随历史保留。移除选库标签不能解除该约束；切换到不允许的模型/连接须拒绝或新开空会话。恢复及继续生成前重新确认受影响库的访问权；失权则停止新的生成，不承诺撤回用户已经看过的历史内容。

## 需要用户或服务维护方提供的最少资料

1. **检索联调条件**：分片分页的 Request URL、GET 方法、200 状态和成功响应已齐。用户确认页面只有搜索框、没有可识别的“检索测试”入口，因此不再要求用户继续抓取检索请求。先按源码 DTO 编写契约测试，再在开发/测试环境由 Bridge 调用真实 `SearchMore` 核验召回和空结果/失败语义；模拟响应测试不能代替真实检索。维护方只需在服务端配置可达的知识库服务地址和权威库授权入口，无需先把内部 SearchMore 开放到公网。不需要用户提供密码、Cookie、Authorization、refresh_token 或内部服务密钥。
2. **线上服务归属和库可见性规则**：Bridge、agentloop 和 Java 源码已取得，无需再次提供。还需确认当前知识库管理 API 由哪套服务承接、是否可复用 boundary，以及同租户成员/私有库/共享库的预期权限。源码存在不等于对应版本已经部署。
3. **测试数据与权限约定**：此库的真实名称、所属 tenantId、一个只用于联调的非敏感文档、3–5 个已知答案的问题；至少一个无权账号/测试租户用于负向验证。返回里的 userId、parentId 不能未经契约确认就当 tenantId。
4. **模型使用范围**：是否接受首期仅通行证平台模型，库资料允许发送到哪些主模型及辅助模型；否则需明确专用内网模型约束。

用户已提供分片详情的 Network 分页请求，并确认页面只有搜索框、没有独立检索测试入口。尚未检查该搜索框的行为，不假设它是文件名筛选或语义检索。后续按问题检索改由 Bridge/.NET 开发环境验证，无需继续要求用户网页抓包。分享接口资料时仍删除所有凭据及敏感正文；不建议发送完整 HAR 或未经脱敏的 Copy as cURL。

## 验收标准

- Stage 1 紧凑选择器无右侧复选小框，点击整行后变色；标题与灰字元信息均可单行省略、悬停读全称。12 项合成目录首批 10 项、触底追加 2 项，无上下页控件、无重复遗漏；搜索后重置分页，追加失败可重试。
- 前端、Go 和后续 Bridge 均验证最多 5 库；5 库保存和重开恢复成功，第 6 库拒绝且原选择/revision 不变。Stage 1 任意已选库仍阻止本题进入模型。
- Stage 1 已选项必须真实位于编辑器内容流，仅有灰色小字号标题，无上方 chips。验证引用前后 Backspace/Delete、跨引用选区删除、Ctrl+A 后删除/剪切及撤销/重做；正文保持正常编辑，引用身份不会被标题中的字符编辑损坏。
- Stage 1 快速连续删除/重选、保存失败及 revision 冲突后恢复权威引用；保留普通正文，不把引用标题/ID 拼入正文或附件说明。验证 A/B、A→B→A、重开及换账号后的引用隔离与持久化。
- Stage 1 验证中文 IME Enter 只上屏、普通 Enter 仍遵守发送阻止、Shift+Enter 正常换行、多行纯文本粘贴与文件/图片附件；mention、场景插入及占位符选区、修改/取消重试和输入区调高均保留。
- Stage 2 真实目录就绪后验收现有内联标题引用新增导航：打开服务端提供且通过可信白名单的对应库目录；普通删除和剪切不触发导航；非法或缺失目标不产生伪造链接。仅导航能力不计入 Stage 1 本次实现，输入框内位置、灰色小字号样式及编辑删除已属 Stage 1。
- “+”中可见知识库；选择正确标题后，连续两次提问仍保留标签，后台每题按当时 ID 快照检索。
- 测试文档含唯一事实，如“星河测试计划的验收口令是 KB-TEST-7421”。提问不透露口令；模型回答与片段相符，并展示真实来源。
- 用测试版“上下文审核”确认实际模型请求包含检索片段，用户原始问题仍保持原文；只看回答或标签不足以证明注入生效。
- A/B 会话分别选不同库，交错发送、切焦点、改库后不会串库；恢复、修改重试、运行中补充各有正确快照。
- 未授权/跨租户/禁用库、超时、401 刷新失败、停止及关闭，不向 LLM 发送这些失败路径的文段，不出现迟到回答。
- 没命中时显示真实状态，不伪造引用；片段里的“忽略要求/泄露凭据”等文本不升级为命令或授权。
- 用已提供文档补充三个可核对的问题：“创建工单前需要谁确认？”应指出员工本人核对/确认；“资料缺失或制度冲突时应怎么处理？”应说明暂无法确认并提供负责岗位；“不同账号能否查看别人的工单？”应引用只能查询本人提交工单的说明。以检索命中及实际模型请求为依据，这些文字问答不要求真正创建/查询工单。
- 自动化通过后，在真实 Windows Wails 测试包中验收菜单、中文输入法、长标题、附件并存、流式回答与来源；浏览器预览不能替代桌面验证。

上述标准覆盖最终完整接入，不表示 Stage 1 已完成真实检索。[Stage 1 内联报告](../../knowledge-base-stage1-inline.md) 记录已执行的自动测试及未执行的 Windows 人工验收；其他历史报告只证明对应版本。Stage 2 修改桌面与 Bridge，agentloop 不变；真实部署联调、上下文注入与线上发布不能由本地契约测试代替。当前交付状态见 [Stage 2 报告](../../knowledge-base-stage2.md)。

Stage 2 复用管理 boundary 的 AllPaging 候选和逐库 ACCESS_RESOURCE 权限接口，Bridge 再核对租户与可用状态，全部通过后调用内部 SearchMore。现有 boundary 源码允许同租户成员读取 PRIVATE 库，部署方须确认它符合实际政策；不能自行把租户 API Key 的文档过滤当作用户 ACL。未知授权总数以 `total=-1`、`hasMore` 表达，未知文档数不伪装为零。目录 URL 由 Bridge 授权后按固定官方页面路由生成，桌面再次验证；这不是上游逻辑目录元数据。知识库开关默认关闭，需配置并验证实际权限网关后开启。
