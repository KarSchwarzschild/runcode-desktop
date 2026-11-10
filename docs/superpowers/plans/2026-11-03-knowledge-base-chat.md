# 桌面对话知识库接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** “+ → 知识库”选择输入框内标题引用，当前会话持续生效；后续完整阶段每题检索已授权文段并供原有 LLM 回答使用。

**Architecture:** React 显示选择及来源，Go 管理会话、身份与检索服务，Bridge 负责用户/租户/库授权并调用 .NET SearchMore。通用的每题上下文准备接在引擎准入与请求构造之间，原始问题与检索参考资料分离。

**Tech Stack:** React 18 / TypeScript / Lexical / Vitest、Go 1.26+ / Wails v3、agentloop、ASP.NET Core 8 / xUnit / KernelMemory；Bridge 为 Java 21 / Spring Boot 4.0.1 / Maven。

**Spec:** [设计与调研证据](../specs/2026-11-03-knowledge-base-chat-design.md)。2026-11-04 用户在 Stage 0 后授权执行 Stage 1，并明确要求展示知识库缩略信息、名称、路径等。Stage 2 已于 2026-11-04 获用户授权；Stage 3–5 尚未授权实施；任务 0 的真实服务契约未补齐前不得将后续草案当作已存在 API。

**更新：2026-11-04。** 本版明确三个仓库的功能分支、分阶段交付及各阶段可验证的能力。以下阶段是实际执行顺序；后面的 Task 0–6 是按组件拆分的实施清单，通过阶段表映射使用，不代表必须把全部服务做完才开始展示桌面。

## Global Constraints

- 用户已确认“当前会话持续生效”，每次提问重新检索；发送后保留选库。
- 本轮执行 Stage 2：Bridge 受权限约束的目录、详情和检索接口；桌面真实目录、选择恢复与内联标题导航。保留 Stage 1 的紧凑选择器和普通编辑删除；上下文接线前仍阻止带库提问。agentloop 留到 Stage 3；不合并、不推送、不部署。
- 用户要求所有产品改动在功能分支完成：桌面 `feat/knowledge-base-chat`、Bridge `feat/knowledge-base-api`、agentloop `feat/question-context`。阶段 0 创建/复用并核验这些分支，后续阶段持续在对应分支提交，不直接改 main，不在引擎 detached HEAD 上提交。
- 桌面 DTO 放 `internal/protocol`，TypeScript 协议由 protogen 生成至 `frontend/src/core/protocol/`。
- 引擎业务边界：知识库服务适配在桌面/Bridge；通用输入、准入、历史投影和上下文机制在 agentloop。
- 库数上限按用户本轮确认改为最多 5 个，前端、Go 与后续服务端同步校验；其他建议预算保持总计 5 个分片，单片 4,000 字符、合计 12,000 字符且 4,000 估算 tokens，并受请求剩余预算限制；列表 10 秒、检索整体 30 秒、正文 1 MiB。
- 只向经验证的服务端发送通行证凭据；内部服务密钥不进入桌面、前端或仓库。借助现有登录刷新，不使用聊天里粘贴的令牌。
- 目录/检索严格绑定 sessionId、会话代际、账号代际、Bridge 和租户；所有命中限定在本次已授权 ID 集合。
- 准备资料不构成用户授权，不污染问题原文；必须进入引擎预算和取消流程。
- 首期建议只开放通行证平台模型；外部自定义模型/Codex/OA 锁定组合待业务政策确认。历史中的资料衍生内容继续受模型目的地约束。
- 方案中的新接口/文件名均为拟议；Bridge、agentloop 和 Java 平台源码现已核查，具体改动位置如下；线上 API 基址、部署版本及权限规则仍需固定。

## Review Focus

- 用户在检索中停止或关闭：迟到结果不能进入模型；任务 2、3、6 覆盖。
- A→B→A 切会话及换账号：旧返回不能污染新会话/身份；任务 3、4 覆盖。
- 已接受但准备失败：原题可见且可重试，不覆盖新草稿，也不能被下一题隐式执行；任务 2、4、5 覆盖。
- 撤权、跨租户 ID、删除/禁用库：请求及返回双向校验，不能继续发正文；任务 1、3、6 覆盖。
- 移除标签、切模型、压缩/恢复：停止新检索不等于清除历史资料，目的地约束和引用来源不能丢；任务 2、3、5 覆盖。

---

## 仓库与分支安排（Stage 0 已创建）

| 仓库及本地根目录 | 功能分支 | 起点与改动职责 |
| --- | --- | --- |
| 桌面：`C:/Users/yyzxc/Desktop/Client-Platform/runcode-desktop-main` | `feat/knowledge-base-chat` | 已取得 `origin/main`，起点 `1a7d819c0e4bfc58c7a32ed98a58b6070adc506b`；开发目录为 `C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main`。后续负责菜单、会话选择、Go 检索适配、Wails 协议、引用及恢复。 |
| Bridge：`C:/Users/yyzxc/Desktop/Client-Platform/Bridge/ouconline-ai-bridge` | `feat/knowledge-base-api` | 从阶段 0 核验的 `origin/main` 建立；知识库目录/详情/检索 API、用户与库权限适配及服务配置。当前本地 main 与已缓存 origin/main 均为 `58e4f82e165ed31bceba1917aecf0203618a097e`，此事实不代替实施时获取远端状态。 |
| 引擎：`C:/Users/yyzxc/Desktop/Client-Platform/agentloop` | `feat/question-context` | 当前桌面固定依赖为 `v0.16.0`，基线 commit `833be6238b4211fc6b9c23497cc2ff91298b74f5`；通用逐题上下文准备、投影、预算、取消及持久化。桌面取得上游历史后重新核对三个 go.mod；若依赖 tag 已变，先按实际依赖更新基线记录与接口调查。 |

- 同名分支存在时先核对其起点和所属工作，不重置、不覆盖他人的提交；每个仓库独立提交并分别形成可审阅的变更。
- 每阶段记录三个仓库的分支、实际 commit、未提交状态、桌面产物 SHA256、Bridge 构建版本与测试结果；涉及某仓库不代表其他仓库也必须产生空提交。
- 如使用隔离 worktree，核对开发 workspace 实际解析到本功能的 agentloop checkout。开发专用 workspace 可以使用正确的本地路径，不把机器绝对路径或临时依赖覆盖提交为正式配置。
- .NET 和 Java 平台优先只复用。若确认必须补权限或取消传递，先在实施记录列出必要改动，在实际涉及的仓库另建 `feat/knowledge-base-access`，同样不直接修改其主分支；不会默认同时重写两套知识库服务。
- 分支用于代码隔离，不等于服务部署隔离或账号/历史数据隔离。测试 Bridge 单独运行或部署到测试环境；桌面测试包使用新测试会话，避免误把已安装旧客户端当作本次成果。

## 分阶段开发与验收

| 阶段 | 开发内容与对应清单 | 交付物及用户可以检查的内容 | 通过标准和边界 |
| --- | --- | --- | --- |
| 0：基线与可运行环境 | Task 0 的 Git/工具链部分；创建上述三个分支，核对 Go、Wails v3、CGO、Node、JDK/Maven | 可启动的原版桌面开发/测试构建、实际路径、三个分支起点、原版检查记录。可以检查已有登录、租户选择和普通聊天 | 本地构建可以运行且能识别构建来源；正常登录/聊天如依赖环境未通则单独记录。此阶段还没有知识库新功能 |
| 1：知识库入口与会话选择 | 桌面分支；Task 3 中选库 DTO/存储的子集及 Task 4 的菜单/选择器/内联原子引用 | 真实 Wails 测试 EXE；用显式标记的合成测试目录验证“+ → 知识库”、输入框内普通删除/剪切/撤销、A/B 会话切换、重开恢复；选库经真实 Go 命令保存 | 前端编辑器/状态测试、Go 选库持久化测试通过；界面清楚标注测试数据。正文与库 ID/标题分离。有选库但上下文链路未就绪时明确阻止该题生成，不把普通模型回答冒充知识库回答。未选库的原聊天保持可测 |
| 2：Bridge 接入真实知识库 | Bridge 分支；Task 1，并把 Task 3 的目录/详情客户端接到阶段 1 界面 | 接口测试报告和已启动的本地/测试 Bridge；使用正常 Passport 登录验证真实个人库/共享库列表；接口可独立返回真实命中文段，无须等待 LLM | 分别验证用户/租户/库授权、检索、空结果、401/403、禁用库和超时。界面能选择真实库，后台可检索；上下文接线完成前仍不能宣称桌面知识库问答已通 |
| 3：agentloop 上下文能力 | 引擎分支；Task 2 | 可复跑的引擎测试结果及脱敏请求记录；通过合成参考文段和受控模型响应验证原问、准备状态、预算、取消、steering、恢复 | 原问与资料分离；每题准备一次；中断、失败和崩溃恢复不漏过准备；模型传输重试复用快照。此阶段证明引擎机制，不等同真实平台模型回答验收 |
| 4：三端完整联调 | 三个功能分支；完成 Task 3、4、5，执行 Task 6 的正向场景 | 可双击的完整测试客户端、对应 Bridge 版本、上下文审核记录。正常登录后选择“数字员工01”，提问、继续提问、查看来源、修改重试和恢复会话 | 真实检索片段出现在实际主模型请求中，回答与文档依据相符；每题重新检索、标签保留、会话隔离。显式用 go.work 联动引擎功能分支，并记录实际来源；尚不是固定依赖发布验收 |
| 5：异常回归与发布候选 | 三个分支及 Task 6 全量验收；按需完成上游服务修补 | 通过自动化与 Windows 实测的发布候选包、完整验收记录、兼容与回退说明 | 验证无权账号、同租户他人私有库、撤权、过期登录、停止、失败重试、重启/压缩及模型切换；使用真实固定引擎 tag 更新三个模块，在 `GOWORK=off` 下重验。服务端测试通过与生产已部署分别记录 |

阶段 2 与阶段 3 在 DTO、错误语义、输入快照及取消契约明确后可并行推进；阶段 4 依赖两者通过。Task 2 的原问持久化、状态恢复等正确性要求不能推迟到阶段 5，阶段 5 是完整回归，不是首次补齐这些机制。

阶段 1 的合成目录仅限显式启用的测试构建/测试适配器，使用独立的测试 ID，不混入真实目录或账号数据；正式模式遇到服务不可用必须报错，不能自动退回模拟数据。阶段 2 接通真实授权和目录后复用同一 UI/Go 状态接口，移除测试选择再进行真实库验收。

Stage 1 补充及本轮内联修订（用户 2026-11-04 确认）：选择器沿用紧凑交互：知识库图标、名称，以及标题下一行灰字合并简介、目录和篇数，单行省略，悬停查看完整内容。去掉右侧复选小框，仅通过整行点击与选中变色表达选择。界面无上下页控件，首批 10 项，触底追加下一页；底层继续使用 page/pageSize。测试目录为 12 项明确测试示例，保留原测试 ID；最多选择 5 个库。已选库必须进入实际文本编辑器，显示为灰色、小字号、仅标题的原子引用；取消上方独立 chips，通过 Backspace、Delete、选区删除及剪切移除，撤销/重做与选库状态同步。正文草稿与引用身份分离，标题/ID 不拼入正文 payload。会话选择持久化与发送后保留规则不变；Stage 1 有库时仍阻止发送。

实现采用 Lexical 替换 textarea 输入叶子，保留 Composer 工具栏、附件与业务流程；对外提供明确的聚焦/选区 imperative handle，DOM ref 单独用于尺寸测量。引用删除经异步 CAS 选择队列保存，连续快速编辑合并到最新意图；保存失败重新读取并恢复后端权威选择，不丢普通正文。路径仍是示例分类路径，不伪造真实网页链接或本地文件路径；普通构建显示未接入原因。前端状态不作为实际发送准入的权威。当前修订的实施与验证记录见 [Stage 1 内联报告](../../knowledge-base-stage1-inline.md)，结果已记录。

Stage 2 为输入框内灰色小字号标题提供打开对应知识库目录的操作，删除引用不得触发导航。实际目标示例为 `https://ai.ouchn.edu.cn/knowledge-base/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa/data-collection`。Bridge 逐库授权后按照固定官方路由生成目标，桌面再次验证 HTTPS、域名、路由及对应库 ID；不得伪造合成测试链接。该路径是平台页面路由，不代表上游提供了逻辑目录字段。

### Stage 2 实施契约（2026-11-04）

- 用户确认现有线上 Bridge 为 `https://bridge-aibase.ouc-online.com.cn`，尚无知识库接口。采用扩展 Bridge；桌面复用 Passport 登录，不管理租户 API Key。
- 目录候选使用管理 boundary 的 `GET /KnowledgeBase/Library/{tenantId}/AllPaging`，每条候选及每次检索前调用 `GET /KnowledgeBase/Library/{id}` 的 `ACCESS_RESOURCE` 权限入口。Bridge 额外核对库 ID、所选租户、启用、未删除及非应用标注状态。所有库通过后才调用内部 `POST /Library/SearchMore`，请求只含固定库集合、问题和服务端检索参数。
- 已检查的 boundary 对同租户成员允许读取 PRIVATE 库。因此不能宣称它已经实现“同租户仅本人私有库可读”；部署前须确认实际权限政策及版本。文档 `Filters` 能力本身不提供用户授权，缺权威入口时不得改用租户 Key 放行。
- 对外仅提供 `/t/{tenantId}/v1/knowledge-bases` 的列表、`/{id}` 详情和 `/retrieve` 检索。过滤后的列表以 `hasMore` 驱动滚动；未知授权总数返回 `total=-1`，不暴露候选总数，不因某页全部被过滤而提前结束。
- 摘要增加 `documentCountKnown`、可选 `directoryUrl`；未知文档数不显示“0 篇”。恢复后已无权限的选择保留可删除占位项和原因，不保留可点击链接。普通构建和测试构建默认真实模式；只有测试构建显式设置 `RUNCODE_KNOWLEDGE_MODE=demo` 才显示隔离测试目录。
- Bridge 知识库功能默认关闭，待服务端配置权限 boundary、内部 KB 地址及信任关系后开启。本地接口/权限契约测试与真实部署联调分别记录；本轮不部署线上服务。具体交付和验收见 [Stage 2 报告](../../knowledge-base-stage2.md)。

Stage 2 本轮交付状态：

- [x] Bridge 目录、详情、逐库权限闸和有界检索；所有知识库调用链上游响应含租户元数据均限制 1 MiB，整体目录 10 秒/检索 30 秒；146 项 Maven 测试通过。
- [x] 桌面真实模式、显式 demo 隔离、会话账号/租户绑定、401 单次刷新、恢复撤权与离线移除、目录导航；标题仅作为显示缓存，重新确认才恢复导航资格。
- [x] 过滤分页空页继续加载、失败保留游标重试、未知文档数、编辑删除和重新确认；前端 569 项测试通过，Go race 与协议检查通过。
- [x] 本地 JAR 打包及健康检查、独立 Windows 测试包、分支提交与审查记录；没有推送或部署。
- [ ] 配置并部署测试 Bridge 的知识库能力，确认实际 boundary ACL 和 JWT 信任，再验证真实个人库/共享库与检索。此项不由本地 fixtures 或匿名 401 替代。
- [ ] Windows 真机完整鼠标、系统中文 IME、真实登录/目录跳转验收；执行结果以 Stage 2 报告为准。

本轮及后续验收清单（需求记录，实际通过情况另见阶段报告）：

- [ ] Stage 1 检查整行选择变色、没有右侧复选小框；长标题和灰字元信息单行省略，悬停可读全称。
- [ ] Stage 1 使用 12 项合成目录验证首批 10 项、触底追加 2 项、无重复遗漏及无上下页控件；搜索切换清理旧分页结果，追加失败可重试。
- [ ] Stage 1 验证 5 库保存、重开恢复及会话隔离；第 6 库拒绝且原 revision/选择不变，带库消息仍由 Go 阻止进入模型。
- [ ] Stage 1 验证已选库真实处于文本编辑器内，仅显示灰色小字号标题；无上方独立 chips。Backspace/Delete、跨引用选区删除、Ctrl+A 后删除或剪切、撤销/重做均同步选库，正文不会被误当成引用或丢失。
- [ ] Stage 1 验证快速连续删除/重新选择的 CAS 队列最终收敛；保存失败恢复权威引用，保留编辑后的正文并提示重试。引用标题和 ID 不进入正文 payload。
- [ ] Stage 1 验证 A→B→A/重开/换账号后的正文与选择隔离；验证 IME 上屏 Enter、普通 Enter、Shift+Enter、多行粘贴、附件、mention、场景占位符选区和修改重试。
- [ ] Stage 2 验证现有内联标题引用新增导航后可打开该库目录；普通编辑删除和剪切不触发导航，保持 Stage 1 的灰色小字号标题样式。
- [ ] Stage 2 验证链接由服务端提供并通过可信白名单，拒绝非 HTTPS、非可信域、异常路由、库 ID 不匹配及带凭据目标；没有合法链接时不能伪造可点击地址。

### Stage 4 测试环境修订（2026-11-05）

用户要求依据本地 `OUCOnline.AI.KnowledgeBase` 源码重写 Bridge 并完成本地联调。本修订替换本轮集成中的旧 management 适配假设，Stage 2 记录保留为历史证据。

- 客户端全部业务 API 指向本机 `http://127.0.0.1:18199`；Bridge 直连知识库 `http://123.249.28.107:15003`，平台与 Passport 分别使用 `https://aibase-preview.ouc-online.com.cn`、`https://passport-ai-dev.ouc-online.com.cn`。
- .NET `/Library` 查询使用真实 `FindLibraryModel.UserId`，用户 ID 只能来自有效 JWT。AllPaging 实际忽略用户过滤，Bridge 对候选、详情和全部待检索库执行同一个所有者/共享/白名单及租户状态判据，再构造 SearchMore。内部服务密钥与用户令牌分别发送至对应服务。
- 库/数据集 GUID、切片复合字符串按源码保留语义；正常空结果、资源不存在、权限拒绝、上游凭据问题和服务故障分别处理。
- 本地测试代码、客户端包与 Bridge 已构建并启动；160 项 Bridge 测试和桌面/引擎回归通过。真实检索、主模型回答和 Windows 交互仍需测试密钥与正常账号登录后的证据，当前不勾选 Stage 4 全部验收完成。详情见 [Stage 4 验收记录](../../knowledge-base-stage4.md)。

Stage 1 本轮清单（Task 3/4 的子集，不代表后续完整任务完成）：

- [x] 知识库 DTO、三条 Wails 命令与生成协议；仅测试构建可见的合成目录。
- [x] 名称/简介/逻辑路径/文档数、12 项紧凑目录、首批 10 项触底追加和最多 5 库；此前上方标签实现仅作为历史记录。
- [x] 用 Lexical 内联原子引用取代上方标签，完成普通删除/剪切/撤销、正文分离、异步 CAS 队列和失败恢复；验证结果见 Stage 1 内联报告。
- [x] Go 持久化、revision 校验、账号/租户/会话隔离、模型切换与重开恢复。
- [x] 带库消息在全部发送/重试路径中拒绝；问答卡与计划执行在副作用之前阻止。
- [x] 初版前端 528 测试、类型/lint/build、Go 根模块 race、协议检查与独立审查；初版 Windows 测试 EXE 已生成。[紧凑版修订记录](../../knowledge-base-stage1-compact.md) 及其测试包属于历史版本，不能证明当前内联修订通过；当前结果见 [内联修订报告](../../knowledge-base-stage1-inline.md)。
- [ ] 真实 Windows 鼠标/键盘全流程、窄窗口和 IME 手工验收；历史自动点击被窗口状态错误阻止，当前修订需重新记录结果。Go lint 基线格式问题及重复 manifest 仍有历史记录。当前检查见 [内联报告](../../knowledge-base-stage1-inline.md)，历史见 [Stage 1 初版报告](../../knowledge-base-stage1.md)。

### 各阶段如何验收，不限于前端

- 阶段 0 可运行真实桌面和现有登录；阶段 1 可测前端到 Go 的真实状态保存；阶段 2 可测真实接口、权限与召回；阶段 3 可测引擎行为；阶段 4 起可直接在桌面验证完整知识库问答。
- 阶段 2 如果测试网络、内部知识库地址或权威库权限入口未就绪，可继续完成本地契约和引擎测试，但将“真实接口联调”保持为未通过，不用模拟数据替代真实通过证据，也不阻塞独立的阶段 1/3 工作。
- 真实登录测试要同时检查 Passport issuer/client 配置、租户返回及 Bridge OAuth callback。桌面默认由 Bridge 基址生成 redirect URI；只改 `RUNCODE_BRIDGE_BASE_URL` 并不保证新地址已经注册。使用维护方已有可用的测试回调或在正常授权流程中配置，不能承诺任意本地 Bridge 地址直接复用线上登录。
- 每阶段提供固定格式的验收记录：`阶段 / 三仓库分支与commit / 桌面路径与SHA256 / Bridge版本 / 模拟或真实数据 / 测试命令及结果 / 用户3–5步操作 / 仍未接通部分`。新客户端产物生成后可建立指向该产物的测试快捷方式；每次启动前关闭旧实例。
- 登录状态、租户身份、每题检索结果、模型实际输入分别验证。用户可通过真实桌面交互和测试版“上下文审核”核对；API 和引擎阶段由开发者执行可复跑测试并给结果，不要求用户自己手写 token 请求。

### 最终集成顺序

先完成三个分支的本地/测试环境审阅及联调；引擎按仓库发布流程生成新的真实版本 tag，桌面三个模块统一升级并以固定 tag 构建；Bridge 对应版本在目标测试环境可用后完成整体验收。生产发布时先提供兼容旧客户端的 Bridge 接口，再发布新版桌面。agentloop 是编入桌面的库，不需要在用户电脑另起一个“agentloop 服务”。

在用户已授权阶段内连续完成实现、检查和交付；进入尚未授权阶段、代码合并、推送版本标签或生产部署均以届时授权为准。本轮止于 Stage 2；暂不实现引擎上下文注入。

## 接口草案（任务间统一口径）

### 用户服务接口：拟新增或映射到现有 Bridge

- `GET /t/{tenantId}/v1/knowledge-bases?query=&page=1&pageSize=10`：只列当前用户可用的库，返回 `items[{id,title,description,path,documentCount,directoryUrl}], page, pageSize, total`；`directoryUrl` 为 Stage 2 的可选可信库目录链接，Stage 1 合成目录不提供伪造链接。界面触底按 page/pageSize 追加。
- `GET /t/{tenantId}/v1/knowledge-bases/{id}`：重新授权并返回规范标题，用于恢复/选中校验。
- `POST /t/{tenantId}/v1/knowledge-bases/retrieve`：正文 `{libraryIds,question}`。服务端控制 TopK、阈值和限额，不开放任意文档 ID、模型、上游 URL 或用户角色字段。
- 检索返回 `{status:"matched"|"empty", libraries:[{id,title}], hits:[{libraryId,documentId,chunkId,title,text,sourceUrl,relevance}], requestId}`。HTTP/业务失败归一为稳定错误码；401、403、404、超时和空结果有不同语义。`sourceUrl` 可以为空。
- 上述地址是推荐的桌面契约，**本地 Bridge 当前尚无这些端点**。如果现有用户网关已有等价能力，复用它并保持桌面适配层对外 DTO 一致。
- 内部可复用 `POST /Library/SearchMore`，请求限定 `LibraryIds,Question,MaxMatchesCount=5,MinRelevance`。用户确认页面没有独立检索测试入口，因此不再等待网页抓包；先按源码 DTO 建立契约测试，再调用测试环境真实服务核验成功、空结果和失败行为，阈值按任务 6 校准。Bridge 可访问内部服务即可，无需现成的公网 SearchMore 路由。
- 已收到的 `data.pageListInfos` 是文档分片分页，不能作为检索 fixture。真实检索应核对 `data.query/results[].partitions[]` 及元数据；字段大小写以部署契约为准。占位 `dataSlicingId=d=//p=` 不作为有效来源 ID。
- 已确认的公网分片分页：`GET https://ai.ouchn.edu.cn/api/KnowledgeBase/Library/DataCollection/08df2f18-f135-42dd-8590-c21f82a1dfca/DataSlicing/Paging?pageNum=1&pageSize=8`，用户 Network 记录为 `200 OK`。可用来固定分页样例与路由映射，不能替代检索契约，也不能假定公网已有 SearchMore。`127.0.0.1:10808` 为浏览器所见本机连接端点，不能配置为业务 API 地址。

### Wails 命令及桌面元数据：拟新增

- `ListKnowledgeBases(req KnowledgeListRequest) (KnowledgePage, error)`：req 含 sessionId/query/page/pageSize，租户和账号由后端会话确定，分类 Query。
- `GetSessionKnowledge(sessionID string) (SessionKnowledgeView, error)`：返回 `{sessionId, revision, libraries:[{id,title}]}`，分类 Query。
- `SetSessionKnowledge(req SetSessionKnowledgeRequest) (SessionKnowledgeView, error)`：req 含 sessionId/libraryIds/expectedRevision，校验成功后原子保存并递增 revision，分类 IdempotentSet；重复同一已生效 ID 集合不再递增。
- `SubmitQuestionRequest` 增加可选 `knowledgeRevision` 和 `knowledgeOverride`。普通发送携带已确认 revision；nil override 表示采用会话选择，显式 `{libraryIds:[]}` 表示本次不使用知识库。override 仅用于已验证的重试来源分支，不能绕过授权。
- QuestionDraft、ResumedBlock 和用户气泡记录每题库 ID/标题快照、准备状态、引用元数据。标题由后端规范化，业务身份只认 ID。
- 新的 `KnowledgeContextStatus` 事件按 sessionId/questionId 关联，阶段为 retrieving/ready/empty/failed/cancelled，含片段数、有限引用元数据或可展示错误；不用它把原始文段发回前端。
- 引擎“通用补充上下文”的方法名称待任务 2 的 API 设计固定：现有 v0.16.0 hook/observer 不足以覆盖这些语义，需增加公开端口，不能在桌面里绕过引擎生命周期。

## Task 0：取得可开发基线，固定真实接口

**涉及：** origin/main；Bridge/管理 API 与 agentloop 源码；本方案文档。

**Produces:** 已确认的服务路径、用户权限语义、响应 fixtures、引擎接口映射及可构建分支。

- [x] 获取脱敏的分片分页请求及响应；用户确认页面只有搜索框、没有独立检索测试入口。按问题检索不再要求用户抓包。
- [ ] 按源码 DTO 固定目录/详情/检索的开发契约，配置测试环境内部服务与权威库授权入口，再通过真实调用验证；核对 `tenantId` 实际来源，不能用租户页面的 userId/parentId 猜测。
- [x] 已取得分片详情前端页面及对应公网 GET 分页请求、200 状态、成功响应样例；页面显示库名“数字员工01”。已区分页面路由、分片分页 API 和网页登录续期请求，任何凭据均不记录或复用。按问题检索及目录/详情的实际 API 契约仍需固定。
- [x] 已取得并只读核查 Bridge `Bridge/ouconline-ai-bridge`、agentloop `v0.16.0`（commit `833be6238b4211fc6b9c23497cc2ff91298b74f5`）和 ai-platform-java main（commit `8920a0efc19b6718c1036e4b0769329fecd8d00d`）。已确认 Bridge 缺知识库接口；引擎缺可靠逐题上下文端口，必须完成任务 2。各目录位于 `C:/Users/yyzxc/Desktop/Client-Platform/` 下。
- [ ] 确认现有用户网关的库授权接口；缺失则由统一服务端补齐。确认允许承载资料的主模型及辅助模型范围，未确认前只做合成数据测试。
- [x] 解决 GitHub 网络访问并取得 origin/main；保护现有文件后建立远程提交基线，再创建 `feat/knowledge-base-chat`。基线为上游 commit `1a7d819c`；原目录完整备份后只建立 Git index/HEAD，未覆盖产品文件。
- [x] Bridge 从核验的 origin/main 创建 `feat/knowledge-base-api`（`58e4f82e`）；agentloop 从三个模块实际依赖的 `v0.16.0` 创建 `feat/question-context`（`833be623`）。隔离目录与证据见 [Stage 0 记录](../../knowledge-base-stage0.md)。
- [x] 检查并准备 Go 1.26.10、Node/npm、Wails v3 beta.9、GCC、Java 21/Maven；.NET 8.0.11 只有运行时、没有 SDK，本阶段不构建 .NET。工具使用用户目录及进程级环境，不保存开发凭据。
- [x] 在新分支记录基线前端/后端检查结果；原版 Windows 测试包已构建并创建窗口。协议生成检查失败、icongen 测试被系统策略阻止、manifest 问题和登录/租户/聊天未验收项均如实记录于 Stage 0 报告，不等同所有检查通过。

**验收：** 阶段 0 先满足三个仓库可解析的 HEAD、明确分支及可运行基线；源码契约可支持阶段 1/3 的本地实现与测试。真实接口、正确权限来源及测试环境条件在阶段 2 完成验证，未完成前不宣称可端到端使用。

## Task 1：服务端受授权的目录与检索适配

**Bridge 根目录：** `C:/Users/yyzxc/Desktop/Client-Platform/Bridge/ouconline-ai-bridge`。

**Create（拟新增）：** `src/main/java/cn/ouconline/ai/bridge/knowledge/KnowledgeController.java`、`KnowledgeService.java`、`KnowledgeClient.java`、`LibraryAccessService.java` 与 DTO；`config/KnowledgeProperties.java`；对应 `src/test/java/cn/ouconline/ai/bridge/knowledge/` 的契约与权限测试。

**Reuse/Modify：** `config/SecurityConfig.java`、`auth/PassportClaims.java`、`me/TenantAccessService.java`、`ApiExceptionHandler.java`，按需要提取现有租户校验为共用能力。HTTP 保护参考 `oa/OaClient.java`；不直接复用面向 Core 的 JSON Bearer 或无条件透传代理。

**权限上游候选：** Java 平台 `ai-boundary/.../authz/TenantAccessAuthorizer.java`、`authz/support/LibraryResourceResolver.java` 及 `controller/KnowledgeBaseLibraryController.java`。确认运行环境再决定调用已部署授权入口或补充其批量授权契约；Bridge 的 LibraryAccessService 是该契约适配器，不维护第二套独立 ACL。当前同租户成员可读的规则需业务确认，不能假定非 owner 一定无权。

**必要时修改 .NET：** `OUCOnline.AI.KnowledgeBase.API/Controllers/LibraryController.cs` 和 `OUCOnline.AI.KnowledgeBase.DomainService/LibraryDomainService.cs`；新增同仓 `OUCOnline.AI.KnowledgeBase.API.Tests/LibraryAccessTests.cs`、`LibraryRetrievalTests.cs`。若权威库授权接口缺失，应先明确由哪一服务提供，再决定实际授权测试落点。

**Consumes:** 验证的用户/租户身份、统一库 ACL、.NET SearchMore。
**Produces:** 上述三个 Bridge 接口和稳定错误/引用结构。

- [ ] 写失败测试：非成员、按确认规则无权的库、指定用户共享、禁用/删除、伪造/跨租户 ID、部分授权多库、空 ID、超 5 库（第 6 库拒绝）、异常命中 ID、HTTP 200 业务失败、空结果、过大正文、取消；另测同租户非 owner 在实际政策下的正确行为。增加 5 库成功请求及库目录链接可信白名单/库 ID 校验测试。
- [ ] 运行对应服务测试，确认测试失败于缺失接入契约；沿用仓库测试框架，不访问生产数据库。
- [ ] 实现列表/详情/检索适配：服务端推导身份与授权，输入只接受显式库集合，返回命中再次验归属；统一包装，并传递取消。
- [ ] 知识库接口仅使用显式租户路由，库授权查询失败拒绝检索。核对实际 issuer/audience/scope 契约与 boundary 的兼容性；新能力不能只以现有 Bridge“JWT 可验签”推断拥有知识库访问权。
- [ ] 增加分页/检索响应区分与来源测试：拒绝占位分片 ID，分页样例使用验证后的 `id`；SearchMore 使用有效分片标识，缺失时生成本次稳定引用键而不伪造平台链接。相对下载地址只按已确认可信基址解析，库标题与生成文件名分别处理。
- [ ] 核验 .NET Controller 向 DomainService 传递 RequestAborted；当前方法虽有 CancellationToken 可选参数，Controller 调用未显式传入，不能假设浏览器断开就能停止服务计算。
- [ ] 通过成功、失败、空结果三类契约测试；提交该服务独立 commit，保持旧调用方兼容，不直接部署生产。

## Task 2：可靠的每题上下文生命周期

**引擎根目录：** `C:/Users/yyzxc/Desktop/Client-Platform/agentloop`。

**Modify：** `engine.go`、`build.go`、`turn/turn.go`、`host/questions.go`、`host/manager.go`；`internal/repl/session.go`、`context_request.go`、`stream.go`、`turn_journal.go`、`partial_commit.go`，以及 `sessions/questions.go`、`checkpoint.go` 中必要的状态恢复/投影代码和对应测试。原问授权证据边界以 `internal/repl/task_evidence.go` 为回归依据。参考 `imageinput/router.go` 的 Prepare/Project，不把知识库业务放进 imageinput。桌面仅接线，不复制引擎实现。

**Consumes:** 原始 UserInput 与冻结的附加资料准备请求。
**Produces:** 受准入、取消和预算管理的上下文准备；失败题状态及问题元数据生命周期。

- [ ] 写失败测试：检索先于该题第一次模型请求；同题多次迭代只准备一次；原问题不变；资料不进入用户授权证据；超预算裁剪；中断/关闭后不发送；准备错误不发送该题。
- [ ] 增加状态测试：receipt 已返回但准备失败时结束回合；失败题可见但从后续普通输入投影排除；下一题不能隐式执行失败题；运行中补充准备失败时中止剩余回合，保留此前输出。
- [ ] 增加持久化一致性测试：元数据落盘失败、原问/快照写入间崩溃、receipt 前后崩溃、raw log 回退恢复。确认前必须可靠保存原问、冻结上下文引用/选库快照和 pending 标记；可用同一日志批次或等价恢复事务，不能先 ack 原问再独立写 sidecar。新格式记录缺元数据/状态时拒绝执行，仅真正 legacy 记录按无知识库兼容。
- [ ] 验证失败测试后实现公开通用端口，由 `engine.Options → build.go → repl.SessionOptions` 接线。原问 durable ack 之后、`armReasoning` 之前准备，持有正确 turnCtx 与 questionId；元数据贯穿 host 拆装 UserInput 链，原问题和来源类别独立。
- [ ] steering 在两处 drain 消费点使用同一准备流程；准备结果不可变且按题绑定。请求构造、压缩后请求替换、预算 sizer 与估算共用无 IO 投影。模型传输重试复用已准备结果；用户重新生成创建新 questionId，保留来源关联并重新检索。
- [ ] 覆盖 steering、摘要、重试分支和历史恢复：重试重新准备，源历史不改；附加资料不默默落为普通用户输入；同一历史的数据目的地约束可持续查询。
- [ ] 运行引擎针对性测试、预算/权限/历史回归，在 `feat/question-context` 提交。阶段 4 显式使用本地 go.work；阶段 5 发布真实 tag 后同时升级桌面三个 go.mod/go.sum，并验证固定 tag。

## Task 3：Go 知识库服务与会话状态

**Create:** `internal/knowledge/client.go`、`service.go`、`context.go` 及各自 `_test.go`；`internal/desktop/knowledge.go`、`knowledge_store.go`、`knowledge_test.go`、`knowledge_flow_test.go`；`internal/protocol/knowledge.go`。

**Modify:** `internal/protocol/questions.go`、`session.go`、`commands.go`、事件映射；`internal/desktop/protocol_aliases.go`、`app.go`、`questions.go`、`turn.go`、`sessions.go`、`session_settings.go` 及关闭/删除实际入口。

**Consumes:** 任务 1 服务契约、任务 2 引擎契约、已有 TokenSource/账号代际和 sessionEntry。
**Produces:** 三个 Wails 命令、每题 selection snapshot、引用/状态事件、可恢复会话选库。

- [ ] 写服务测试：401 只刷新一次、失败登出、跨源重定向拒绝、限额/超时、按库 ID 去重及引用编号、账号代际前后双查、受控模型目的地。检索返回的正文不作为日志和 UI payload。
- [ ] 写集成测试：5 库保存恢复、6 库拒绝且旧选择/revision 不变、目录分页 10+2 无重复；A/B 并行、后台会话发送、revision 过期拒绝、选择未保存不能发送、准备中换账号/改库/关闭、停止后无迟到回答、恢复撤权、移除后切换第三方模型被历史约束阻止。
- [ ] 运行失败测试后实现服务；App 只做装配和命令门面。绑定明确 sessionId，避免使用全局当前租户；知识存储按账号/会话隔离并随会话删除清理。
- [ ] 在主 SubmitQuestion 接入冻结与引擎 hook；旧 SendMessage/InjectMessage 系列要委托同一准备策略或在有选库时明确拒绝，不能绕过检索。修改重试分支校验后才接受 override。
- [ ] 将已接受失败题原文、库快照和状态保存；桌面 sidecar 与引擎 durable receipt 的一致性遵循任务 2 的恢复协议，避免崩溃后漏检索。不保存长期片段缓存。检索错误返回可理解说明，不显示内部异常、凭据或敏感 URL。
- [ ] 完成 schema/event 注册并运行 protogen 生成；通过 Go 相关测试与协议检查后提交。

## Task 4：加号入口、选择器和输入框内原子引用

**Create/retain:** `cmd/runcode-desktop/frontend/src/composer/knowledge-picker.tsx`；`session/use-knowledge.ts`、`session/knowledge-state.ts`、`session/knowledge-state.test.ts`。增加 Lexical 编辑器叶子、知识库引用节点及编辑回归测试；具体文件名由实施者按职责确定，不再创建独立的上方 knowledge-chips 组件。

**Modify:** `composer/toolbar.tsx`、`composer/index.tsx`、`composer/use-composer-resize.ts`、`composer/keymap.ts`（需要时）、`App.tsx`、`core/bridge.ts`、`session/use-question-retry.ts`、`session/use-scenario-actions.ts`；按需调整会话草稿引用位置元数据，正文与后端选库仍分开保存。

**Consumes:** 任务 3 Wails DTO 和命令；已有 ui token、Popover、feedback、action-scope。
**Produces:** 真正可在输入框内编辑删除的知识库原子引用，以及独立持久化的会话选择；Stage 1 带库发送继续阻止，后续完整链路提交带确认 revision。

- [ ] 写状态测试：发送清草稿但选库不变；空文本不发；重复 ID 去重、同名异库可区分；5 库保存恢复、第 6 库拒绝且旧状态不变；首批 10 项触底追加、无重复遗漏及搜索切换清分页；移除仅作用后续；A→B→A 迟到结果失效；保存失败与 revision 冲突不误发。
- [ ] 写准备事件测试：事件早于 receipt 正确绑定；accepted 后失败能重试，不覆盖用户新草稿；停止/关闭取消加载，busy 结束与回合状态一致。
- [ ] 先编写并观察编辑回归失败：引用两侧 Backspace/Delete、跨引用选区删除、Ctrl+A/剪切、撤销/重做、重复 ID、同名不同 ID、正文保持及 payload 无标题/ID；快速删除/重选队列、CAS 冲突、保存失败恢复与迟到结果隔离。
- [ ] 运行失败测试后用 Lexical 替换 textarea 输入叶子并删除上方 chips。引用位于真正可编辑内容流，仅显示灰色小字号标题，作为一个原子对象参与选区；普通文字和附件流程保持。明确 imperative handle 的 focus/选区语义及独立 DOM ref；保留 IME、mention、场景占位符、重试恢复和可调整高度。
- [ ] 保留紧凑选择器：整行选择变色，无右侧复选框；标题下一行灰字显示简介/目录/篇数，单行省略并可悬停查看完整内容；无上下页控件，首批 10 项触底追加。常驻选择独立于 emptyDraft；删除意图经异步 CAS 队列合并保存，失败回读权威选择且保留普通正文。
- [ ] Stage 2 真实目录接入后才为现有内联标题增加导航：使用服务端提供且通过可信白名单的对应库目录目标；编辑删除不得触发导航。覆盖正常导航、非法目标拒绝、无链接回退及移除不导航；Stage 1 合成引用无链接。
- [ ] 使用现有 token 和反馈组件；App 只连线，不直接请求 .NET，也不持有任何 token。无登录/不支持连接时显示原因和正常登录入口。
- [ ] 通过状态测试、typecheck、lint 与构建后提交；手动验证键盘/鼠标和窄窗口。

## Task 5：问题快照、回放、重试和来源

**Modify:** 前端 `session/question-draft.ts`、`use-question-retry.ts`、`use-conversation.ts`、`chat/blocks.ts`、`chat/block-view.tsx`；后端 `internal/desktop/questions.go`、`sessions.go`、知识存储和相关 DTO。
**Create:** 前端 `chat/knowledge-sources.tsx` 及纯引用映射测试；后端 `internal/desktop/knowledge_history_test.go`。

**Consumes:** 每题库快照、准备状态与来源元数据。
**Produces:** 实时/恢复一致的标签、原文及来源；重试重新检索且分支隔离。

- [ ] 写失败测试：重开/压缩后选库不丢；旧历史字段缺失兼容；重新生成沿用原题快照；修改后重试可显式空选；取消不改会话配置；新分支继承覆盖而来源会话不改。
- [ ] 写来源测试：只有检索命中才能显示“已参考”；来源名称/片段 ID 可追溯；未知引用编号不能伪造链接；恶意标题/URL 不执行；失败题的历史投影规则保持。
- [ ] 通过失败验证后接入回放和分支元数据；标题来自后端规范值，原问题仍仅包含用户文字及原有附件表示。普通发送后保留常驻选择。
- [ ] 将来源编号和文档名呈现在回答旁；只允许实际命中集合的来源。明确区分“检索得到的参考资料”与模型精确引用，不承诺模型每句话都有证据。
- [ ] 通过前后端针对性测试后提交。

## Task 6：自动化、真接口和 Windows 验收

**Create:** `docs/knowledge-base-chat-acceptance.md`，记录脱敏数据、环境版本、预期/实际与未通过项；不记录 token 和真实私有原文。

- [ ] 前端在 `cmd/runcode-desktop/frontend` 下运行 `npm run typecheck`、`npm run lint`、`npm test`、`npm run build`，均需退出码 0。
- [ ] 桌面项目根目录使用固定引擎 tag、PowerShell `$env:GOWORK='off'`；内部依赖访问配置好后运行 `go test -race ./...`、`go -C cmd/runcode-server test -race ./...`、`go -C cmd/runcode-desktop test ./...`，以及三模块 `go build ./...`。Windows race 需可用 CGO 工具链。
- [ ] 运行 `golangci-lint run ./...` 及 `go run ./tools/protogen --check`；Bridge/agentloop 修改分别运行其仓库要求的相关检查。不要用本地临时 replace 代替固定 tag 验收。
- [ ] .NET 根目录运行 `dotnet test OUCOnline.AI.KnowledgeBase.API.Tests/OUCOnline.AI.KnowledgeBase.API.Tests.csproj -c Release`；新增权限/检索测试必须实际被发现执行，已有认证测试通过不能替代业务测试。
- [ ] 在授权测试环境验证真实目录/详情/检索；提供非敏感已索引文档和确定答案。使用至少 10 条相关、无关及同义问题观察召回与阈值；记录命中正文与原文是否一致，而非只记录返回码。
- [ ] Bridge 仓库执行 `mvn test`，覆盖已有模型/OA 路由回归及新增知识库权限、限额、错误和来源契约；如果修改 Java boundary，运行它的模块授权/路由测试。新增知识库回归尚未运行；Stage 0 原版基线结果见报告。
- [ ] Windows Git Bash 进入 `cmd/runcode-desktop`，按已选品牌运行 `bash ./scripts/build-desktop.sh --brand zhikai-guokai --test`（品牌为建议，需与目标产品一致）。测试包设置中启用“上下文审核”，只使用合成测试资料。
- [ ] 正向实测：+ 菜单选择指定库 → 标题正确 → 提问“星河测试计划的验收口令是什么” → 确认模型请求含 `KB-TEST-7421` 文段、原始提问不变 → 流式回答正确且来源对应。
- [ ] 已提供文档的语义验证：问“创建工单前需要谁确认？”、“资料缺失或制度冲突时应怎么处理？”、“不同账号能否查看别人的工单？”，检查分别引用员工本人确认、暂无法确认并提供负责岗位、只能查本人工单的文段。这里只测文档问答，不真正建单或查工单。
- [ ] 持续性实测：第二问重新检索且标签仍在；新会话无选库；A/B 交错不串库；移除标签不再调用检索。验证“无标签的新会话”请求中无片段，不能用同一历史中的回答去证明模型遗忘。
- [ ] 负向实测：无权限账号/租户、禁用库、撤权、列表/检索故障、401 续期失效、停止、关闭、运行中补充失败、重试、切模型。对应行为与设计逐项相符。
- [ ] 验收通过后关掉测试审核，按仓库发布流程构建正常包；服务端接口先在测试环境可用，再联调客户端。生产部署/发布另按用户当时的授权处理。

## 用户可以先完成的测试准备

1. 在可用于测试的知识库里准备一份非敏感文件，写入唯一事实和 3–5 个明确答案；等网页显示解析/索引完成再测试。
2. 已记录分片分页的 Network 请求；用户确认页面没有独立检索测试入口，因此无需继续提供检索抓包。开发阶段先运行契约测试，再使用非敏感测试库验证真实 `SearchMore` 调用；所有分享资料仍删除 Cookie、Authorization、各种 token 和内部密钥。
3. 提供真实 tenantId 的来源说明和库名称；确认测试账户有权看到该库，再准备一个无权账户测试拒绝行为。
4. Bridge、agentloop 和 Java 平台源码均已取得，无需再次提供；分片样例、详情页面及真实分页 Request URL/Method 均已齐。真实联调前由维护方在服务端配置内部知识库地址和权威库授权入口，无需将 SearchMore 先公开到公网。登录续期和分片分页均不能替代检索验证。

## 开发过程中便捷检查桌面状态

每个可验收的改动阶段交付实际运行入口、构建时间/文件摘要、已通过检查、需要用户点击的 3–5 步和预期结果；不要只给“代码已修改”。窗口内产品版本不会随每次源码修改自动递增，因此它只能辅助识别，不能单独证明是本次构建。

| 变化范围 | 快速检查方式 | 需要重启的部分 |
| --- | --- | --- |
| React 菜单、标签、布局 | 配好工具链后，在 `cmd/runcode-desktop` 运行 `wails3 task dev`，在真实桌面开发窗口看 Vite 热更新 | 通常不重启；纯浏览器预览不能代替 Wails 登录/IPC 验证 |
| Go 桌面逻辑、协议或 agentloop | 重新构建测试 EXE，再退出旧进程并启动新文件 | 桌面测试客户端；当前 dev 监听根目录为桌面壳目录，不能假定它监听仓库外层 `internal` 或同级 agentloop |
| Java Bridge | 运行相关测试并重启本地/测试环境 Bridge，客户端连接到该实例 | Bridge；仅改本地源文件不会更新线上服务，切换客户端 Bridge 地址后重新登录 |

Stage 0 已准备本机入口，以下 PowerShell 命令明确使用功能 worktree、用户目录工具和外部开发 workspace：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\Users\yyzxc\Desktop\Client-Platform\.codex-stage0\desktop.ps1' -Action Build
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\Users\yyzxc\Desktop\Client-Platform\.codex-stage0\desktop.ps1' -Action Run
```

Windows 产物为 `C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main/cmd/runcode-desktop/bin/智开.exe`。也可以双击 `.codex-stage0` 下的 `build-desktop.cmd`、`start-desktop.cmd`、`check-desktop.cmd`。这些入口已固定为智开测试版；底层品牌脚本使用 `--brand zhikai --test --local-engine`，通过外部 workspace 联动本功能的引擎工作区。最终验收必须发布固定引擎 tag，移除本地引擎参数并使用 `GOWORK=off`。

Stage 0 已验证 Go 1.26.10、Wails v3.0.0-beta.9、GCC 16.2.0、Node/npm 和 WebView2，工具版本、构建结果与实际检查证据统一记录在 [Stage 0 报告](../../knowledge-base-stage0.md)。

操作顺序：关闭旧客户端主窗口 → 从上述实际产物路径启动（不要点仍指向已安装旧 EXE 的快捷方式）→ 设置中检查“上下文审核（测试版专属）” → 开启后点击“打开查看页” → 新建测试会话进行本阶段检查。应用按品牌限制单实例，旧进程未退出时，新启动可能只是唤起旧窗口。测试构建并不自动隔离配置和历史，验证使用新测试会话；审核会在本机记录完整提示词，使用非敏感测试材料。

Stage 1 当前日常检查按 [内联修订报告](../../knowledge-base-stage1-inline.md) 执行：入口、元信息、输入框内普通编辑删除、正文保持、异步保存恢复和阻止带库发送。初版与紧凑版报告仅为对应历史产物的证据。Stage 4 完整联调后再检查发送后引用保留、第二问重新检索和回答来源；届时在审核页确认实际主模型请求含命中文段、用户原问未改写，并测试 A/B 会话、停止和重试。完整验收见 Task 6。

账号使用现有“设置 → 账号(通行证) → 基座通行证登录”（或该账号采用的统一认证入口）。用户账号密码属于 Passport；租户是该用户所属的组织/授权空间，没有本功能专用的“租户密码”。登录后从服务端返回的租户列表选择“国开在线租户”，并选择获授权的平台模型；`tenantId` 取返回的租户 `id`，不拿 userId/parentId 或显示名称代替。缺租户或库访问权时由平台管理员分配，源码和 GitLab 凭据不能自动提供平台权限。

## 本轮结果边界

已核查桌面、.NET 知识库、Bridge、agentloop 和 Java 平台五套源码；Stage 0 已建立三个功能分支、准备工具并验证原版基线。本轮继续执行用户授权的 Stage 1，使用真实输入框内原子引用替代上方标签，复用元信息选择器和 Go 会话选择存储；当前修订详细验证待 [内联报告](../../knowledge-base-stage1-inline.md) 填写。Bridge/agentloop 分支及产品边界不变。Stage 2–5 的真实目录、检索、上下文和发布验收仍待后续授权与实现，不因 Stage 1 完成而勾选。
