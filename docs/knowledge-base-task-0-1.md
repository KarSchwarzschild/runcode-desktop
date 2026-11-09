# 知识库接入 Task 0–1 实施记录

日期：2026-11-05。范围是固定版本、核对 Bridge 契约、移除桌面对未发布引擎接口的依赖。Task 2–7 的宿主知识库问答、真实服务联调及 Windows 成品验收尚未执行。

## 版本与工作区

| 仓库 | 分支／版本 | 基准提交 | 本轮变更 |
| --- | --- | --- | --- |
| 桌面 | feat/knowledge-base-chat | 878713db5ba563b8eb5e455744dfd56cfc796f3a | 本记录和 Task 1 代码，未提交 |
| Bridge | feat/knowledge-api | 163b265ca9463a638d20c58055307ce281f693dd | 无源代码变更；包含 3a133cd 知识库及 163b265 Core 独立认证 |
| agentloop | v0.16.0 | 833be6238b4211fc6b9c23497cc2ff91298b74f5 | 无变更 |

桌面工作区为 `C:/Users/yyzxc/.codex/worktrees/knowledge-base-stage4/runcode-desktop-main`，Bridge 为 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/worktrees/ouconline-ai-bridge`。复用已有分支，没有新建分支、提交、推送或部署。旧 `feat/question-context` 工作区未用于构建。

## Task 0：配置及契约证据

用户提供的三个地址与本分支配置职责对应如下。下面是候选环境的可达性核对，不是账号权限验收。

| 配置 | 值 | 已观察到的结果 |
| --- | --- | --- |
| BRIDGE_PASSPORT_ISSUER_URI | https://passport-ai-dev.ouc-online.com.cn | OIDC discovery 成功，issuer 无尾斜杠；公布 manageapi 等 scope |
| BRIDGE_KNOWLEDGE_PLATFORM_URL | https://aibase-preview.ouc-online.com.cn | 未带凭据访问 `/api/TenantUser/CurrUserTenants` 返回 401 |
| BRIDGE_KNOWLEDGE_BASE_URL | http://123.249.28.107:15003 | 未带凭据访问 `/Library/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa` 返回 401；未据此认定该库存在或可读 |

本机 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/stage4/Start-Stage4-Bridge.ps1` 也配置了上述三个地址，Core 根为 `http://123.249.28.107:15001`；本轮只读取配置来源，未运行旧启动脚本。它从进程／用户环境读取两个服务 key，核查 Process/User/Machine 范围均未设置（只输出存在性，未输出值）。

地址均不附加 `/api` 或 `/Library`。知识库目录、详情和 SearchMore 共用最后一个根地址；无需再猜另一个 SearchMore 服务端口。平台与知识库是否确属同一业务环境、该账号是否属于目标租户，仍须认证请求确认。

必要配置仍待部署方／本机服务配置确认：

- `BRIDGE_KNOWLEDGE_INTERNAL_SERVICE_KEY`：仅发往 .NET 的 `X-Internal-Service-Key`，不使用网页登录密码替代。
- Core 模型链路独立的 `BRIDGE_AICORE_BASE_URL=http://123.249.28.107:15001`（来自既有本机启动脚本）和按部署要求提供的 `BRIDGE_AICORE_SERVICE_API_KEY`。知识库地址和密钥不能替代它们。
- 桌面 OAuth client/callback 注册与实际令牌的 `aud`、`scope`、`UserId`。公开 discovery 只公布支持的 scope，不能证明一次登录获得了它们。不要复用历史账号／旧管理模式测试来宣称本分支已联通。

本机 .NET 的 `appsettings.json`、`appsettings.Development.json` 未提供可用的内部服务认证配置；本轮没有获取或输出真实密钥、完整令牌或私有正文。真实认证验证保留到 Task 7。Task 0 的“从部署配置取得必要凭据”仍为待完成项，其余版本、代码契约和本地验证已落实。

Bridge `163b265` 契约（依据 `docs/knowledge-base.md`、Controller／Service／Mapping／Properties）：

| 项目 | 固定行为 |
| --- | --- |
| 桌面入口 | `GET /t/{tenantId}/v1/knowledge-bases`、`GET .../{libraryId}`、`POST .../retrieve` |
| 身份 | Spring 验签、issuer、有效期；知识库要求 aud 和 scope 均包含 manageapi；用户取 JWT 字符串 UserId |
| 当前成员 | 用户 Bearer 调平台 `/api/TenantUser/CurrUserTenants`；租户／用户精确匹配且 disabled=false |
| .NET 身份 | 不转发用户 Bearer；仅向知识库根注入独立内部服务 key |
| 目录 | `/Library/{tenantId}/AllPaging` 是候选页；默认 10、上限 50，逐条详情 ACL，不跨页补齐，total=-1 |
| 库 ACL | 同租户、启用、未删除、非应用标注库；库主可读。Private=0 非库主拒绝；1/2 当前租户共享；3 白名单精确匹配用户 |
| 检索 | 1–5 库，先逐库 ACL，再一次 `POST /Library/SearchMore`；原问题保持不变，maxMatchesCount=5、minRelevance=0 |
| 文段 | 核对 libraryId／dataCollectionId 与 tags；chunkId 使用 DataSlicingId，不用 fileId 假冒文档身份；最多 5 片及有界正文 |
| 错误 | 400 输入；403 用户/成员/库权限；404 不存在/停用/删除；502 协议/上游；503 .NET 服务认证/配置；504 超时 |

一次目录页后逐条详情是当前 ACL 所需的 N+1，默认应是 10 次详情，不应是 500。客户端自动补页修复属于 Task 5，本轮未修改；本轮也未向真实服务制造大量请求。

### Bridge 本地验证

Java 21，带本机 Maven settings／缓存的 `clean verify`。第一次因正在运行的 `target` JAR 被 Windows 锁定，在测试前失败。未停止该服务；临时 POM 仅将 `build.directory` 改至隔离输出目录，其余内容与当前 POM 一致；验证后已删除临时 POM。

```powershell
# 在 Bridge 根，先加载本机已有工具环境
. 'C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/env.ps1'
$kbJavaTools = Join-Path $Stage0Tools 'java'
# .codex-task01-verify-pom.xml 是当时的临时文件，已清理
mvn.cmd -B -ntp -s (Join-Path $kbJavaTools 'settings-stage0-direct.xml') `
  -gs (Join-Path $kbJavaTools 'global-settings-stage0.xml') `
  ('-Dmaven.repo.local=' + (Join-Path $kbJavaTools 'm2-repository')) `
  -f .codex-task01-verify-pom.xml clean verify
```

结果：**175 tests，0 failures，0 errors，0 skipped，退出 0，BUILD SUCCESS**。本地 HTTP fixture／JWT 测试证明本分支契约，不证明真实环境已通过。

日志：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/no-engine-task0-bridge-isolated-verify.log`。
隔离产物：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/artifacts/no-engine-task0-bridge/ouconline-ai-bridge-0.1.0-SNAPSHOT.jar`。未部署此 JAR。

## Task 1：当前桌面行为

- 移除 `questioncontext`、`Options.QuestionContext`、`UserInput.Context/SourceQuestionID`、`QuestionContexts()`、`ResolveQuestionContexts` 接线及依赖这些 API 的旧集成测试；保留正常网页登录测试、目录/选择/来源 DTO 与 UI。
- 普通问题经 v0.16.0 公开接口提交原文，普通图片与重生成沿用既有逻辑。
- 选择任意库后，主提问、旧发送/图片/插入入口及创建重试分支明确拒绝；知识库 override 也拒绝。报错为“知识库问答尚未就绪”，不进行检索、不接受用户消息、不请求普通模型。清除选择后可发普通问题。
- `knowledge_history.go` 检查 JSONL 原始记录、SQLite 原始 data 和相应 checkpoint。发现旧扩展字段（含 null、大小写变体及重复 messages 字段），或发现损坏记录，都不交给旧引擎执行。checkpoint 沿用引擎 32 MiB 上限；普通 journal 不增设总记录大小限制，JSONL 逐值扫描、SQLite 按条读取，保留多图片普通历史兼容性。扫描有 30 秒截止，提交/重试读取沿用准入取消 context；没有删除、重写或迁移旧历史。正常正文或工具参数中同名文字不算格式标记。
- 恢复/重建、GetQuestion、ForkQuestion、SubmitQuestion 及重试来源接入检查。旧历史暂时不能继续或编辑重试；完整只读 UI 属于 Task 6。
- 保留 pending-admission 取消及其生命周期测试，未回滚后续已有修复。

最初 `GOWORK=off go test ./internal/desktop ./internal/protocol` 失败于不存在的 `agentloop/questioncontext` 包。移除不兼容引用后，新测试先复现“旧原始记录被接受”和“选库未在准入前阻断”，随后完成阻断实现并通过回归。

已执行且退出 0：

```powershell
$env:GOWORK='off'
go list -m -json gitlab.ouc-online.com.cn/aibase/agentloop
go -C cmd/runcode-desktop list -m -json gitlab.ouc-online.com.cn/aibase/agentloop
go -C cmd/runcode-server list -m -json gitlab.ouc-online.com.cn/aibase/agentloop
go test ./...
go build ./...
go -C cmd/runcode-server test ./...
go -C cmd/runcode-server build ./...
go -C cmd/runcode-desktop test ./...
go -C cmd/runcode-desktop build ./...
```

三个模块均为 v0.16.0，无指向 agentloop checkout 的 Replace。针对性测试覆盖 real 选库零模型请求/零 journal 写入、原问不变、普通重试、JSONL/SQLite/checkpoint 拦截、普通记录允许及生命周期取消。完整根模块测试日志：`.codex-stage0/logs/no-engine-task1-root-final.log`；针对性日志：`no-engine-task1-red.log`、`no-engine-task1-green.log`、`no-engine-task1-compat.log`（同一 logs 目录）。

独立代码复核提出的多图片记录大小限制、JSON 字段大小写/重复键解释差异均已用失败回归复现并修复；另补上准入取消传递。也验证了 Unicode 大小写折叠与普通工具原始 JSON 大数值不会误判。复核 RED/GREEN 日志为 `no-engine-task1-review-red.log` / `no-engine-task1-review-green.log`。

这是一份可编译的固定版本基线，不是已实现的知识库问答成品。尚未打包/验收新的品牌 EXE，未运行新的真实检索→模型链路；继续实现需要 Task 2–7。
