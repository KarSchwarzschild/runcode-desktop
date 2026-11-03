# 命令、联网与 MCP 授权边界

本地 go.work 联调，尚未发布引擎 tag。三个 module 的固定版本仍为 v0.15.0；本轮不打包、不部署、不提交或发布。已有测试 EXE 不包含这些改造。正式发布须真实引擎 tag、三个 require 一致升级，并单独完成 GOWORK=off 验证。

## 批准一次和记住范围不同

一次调用以完整规范化 JSON、实际工具绑定、来源会话及回合区分；参数身份只保存散列，不用截断预览作身份。重复键、无效/过深 JSON、超过 8 MiB 的参数拒绝。原执行 JSON 不改写。

审批显示实际目的地、至多 4096 字节的参数预览、常见敏感字段遮盖，以及记住后的范围。URL userinfo 和常见 token/password 查询字段不展示；这不是任意自由文本的秘密识别器。原始参数、用户任务和 URL query 不进入权限遥测。预览截断时仍按完整输入区分 once，但不支持智能免审批。

| 类型 | 会话 / 项目记忆范围 |
| --- | --- |
| Bash | 完整命令文字 + 执行壳绑定，不再按同一个程序或粗类别放行不同参数 |
| WebFetch | HTTP(S) origin（协议、主机、有效端口）+ auto/configured/指定回环代理路线 + 是否仅诊断 |
| WebSearch | 实际注册工具披露的 origin + 平台搜索/公共搜索路线，不再把 Bridge 写成 DuckDuckGo |
| MCP | 实际服务器配置指纹 + tools/call、资源、提示词种类 + 指定工具 / URI / 提示词名称 |
| 自定义宿主工具 | 精确参数摘要（不再把原始参数写进新规则）；旧 allow 需重批，旧 deny 保留 |
| 目录 | 上一轮的 read/write + 目录及子目录范围，见 [目录授权](directory-access.md) |

MCP standing 允许同一个已配置工具的其它参数；once 只覆盖具体参数。服务器同名但端点、stdio 命令/参数/目录、环境或非凭据静态头改变，不能沿用配置绑定授权。动态令牌来源和凭据刷新不改变绑定。没有绑定的第三方 / 自定义客户端只支持具体调用批准，不伪造配置级记忆。

同一回合并发且参数身份相同的调用可能共享 once；不同参数、工具、来源或回合不得借用它。目录 standing 可按已有目录范围合并，once 不开放兄弟文件。批准完整命令不代表冻结程序或脚本文件的内容；同路径内容、依赖、环境和外部进程的后续行为不是 OS 沙箱。

## 命令与网络

命令分类、计划只读判断和真实 Bash 前台 / 后台启动使用同一执行壳快照。已知的简单读取形态可证明只读；git branch/tag 的修改参数、env 启动程序、输出参数、非空设备重定向、换行、单 &、展开/转义等不能仅凭程序名放行。Go 测试/构建不再标成纯读取；计划阶段不把它们当作无副作用探索。PowerShell 不套用 POSIX 的只读证明，复杂命令回到人工。危险删除、提权和破坏性 VCS 的确定性限制仍保留，不靠审批解除。

对已识别读取命令的参数检查真实路径，包括工作区内指向外部文件的链接；目录可达性与命令批准取交集。不能静态枚举任意脚本/程序的一切读写，目录授权也不等于执行授权。

HTTP 客户端保留原 transport、TLS、SSRF、NO_PROXY、cookie jar 和更严格的 redirect hook；不改调用方共享 client。允许有界同源跳转，跨 origin（含改端口或 HTTPS 降为 HTTP）在发送前停止。WebFetch 返回新目标，须另一次正常工具授权，不自动沿用原批准；平台搜索和 MCP 也不携带身份头跨 origin 跟随。浏览器打开后的导航不属于这一 HTTP 客户端边界。

## 智能审核不是用户授权的替代

风险与任务相关性独立输出：risk 和 aligned/unrelated/uncertain。模型推断的免审批必须同时满足低风险、明确相关、证据完整、确定性底线和自动放行额度；不相关、未知、失败、超时或证据不足均转人工。旧风险-only HarmJudge 不填写新证据字段时只能转人工，旧 AssessHarm API 仍保留作兼容的风险评估。

直接用户输入只在 RunQuestion 接收 / journal 确认点和补充问题实际入历史点进入临时快照。不从压缩摘要、助手输出、hooks、工具、网页或 MCP 内容提取用户授权。子代理只继承父工具批次的快照，委派文本不是新的人类授权。只有“继续”而没有当前打开期间的直接任务依据时转人工，不从恢复的摘要猜任务。

快照仅留内存，最多 16 条 / 64 KiB 用户正文；图片任务、缺失或截断标为不完整，结构化评估另有体积上限和 60 秒总时限。不完整后保守转人工，重新打开会话并明确提交任务可建立新的依据。它不是第二份持久化历史，也不承诺语言模型绝对识别真实意图。

桌面裁判固定来源 session ID + host generation；模型、工作区、脚本均取来源受管 lease，不回退焦点。关闭会取消评估，同 ID 重建不能复用旧裁判。通行证登录代际在凭据交付与各次评估尝试复核；正常续期不失效，换账号/重新登录须重开会话，旧会话不能取走新账号令牌。脚本只从来源工作区 rooted 读取，最多 64 KiB；越界、缺失、二进制、截断、组合或不明确解释器形态不能作自动放行依据。读到完整脚本也不是对任意依赖、运行中替换或外部效果的安全证明。

保留模式本身的确定性语义：interactive 不自动运行裁判；safe 不因本功能放宽；judge 原有工作区普通文件变更信任仍是模式选择，不是模型推断的授权；显式 standing 权限按其既定范围处理；flight 仍是用户主动选取的绕过模式。OA 外层锁、提权人工关、敏感执行面、危险命令与计划限制不能由裁判推翻。

## 旧规则与 CLI

旧粗命令、host-only 网络、names-only MCP allow 不自动扩大成 v2 授权，可能需要重新批准。旧规则保留供查看/撤销；deny 仍检查旧键，命令重分类后也保留原类别的 deny。普通模式下显式 deny 在可信服务器 / judge 工作区放行前生效。

网络 allow 要明确 origin，不能输入一个带路径/query 的 URL 后悄悄变成整站授权：

```sh
runcode permissions allow https://example.com --route auto
runcode permissions allow https://example.com:8443 --route configured
runcode permissions allow https://example.com --diagnose
runcode permissions deny example.com
runcode permissions deny https://example.com:8443 --route configured
runcode permissions list
```

WebSearch 应从实际调用的审批创建授权，CLI 不猜服务器配置的搜索路线。网络 origin 不是 URL 路径权限；对站内不同路径/参数要逐次确认时使用 once。

## 验证与剩余验收

引擎回归覆盖不同命令参数、三种壳的保守分类、危险组合、once 参数/回合、bound MCP 种类和配置、旧 deny、JSON 歧义、任务来源/继续/补充/委派/截断及独立相关性。两台本机 HTTP fixture 计数确认拒绝跨 origin 时目的端没有收到请求，MCP / 平台搜索使用合成身份头。

TestSmartReviewUsesSourceTaskModelAndScript 使用真实 host/engine 与本机合成 SSE：B 聚焦时 A 的后台动作仍由 A 的模型和脚本审核；低风险但无关转人工，未获准的临时哨兵不执行，同 ID 重建后旧裁判失效。它不使用真实账号、付费模型或真实项目内容。

开发入口 ?preview=authorization 用真实 React 审批组件和合成请求检查实际 origin、参数遮盖、精确范围、once-only 选项、短窗滚动及答复。浏览器截图不是原生 Wails、真实第三方模型、MCP 业务或其它 OS GUI 验收；这些仍须目标环境检查。交叉编译也不代替真机。
