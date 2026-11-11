# Stage 1 紧凑版修订记录

> 历史记录，已被 [Stage 1 内联引用修订](knowledge-base-stage1-inline.md) 取代。本文的 commit、EXE、SHA256 和测试结果仅对应紧凑版；其中“本次保留原可移除标签”“输入框内样式留到后期”已不适用于当前要求。当前 Stage 1 要求真正文本编辑器内的灰色小字号标题原子引用，可用普通删除/剪切/撤销操作；仅真实目录导航仍留待 Stage 2。当前修订的检查和产物证据见新报告。

2026-11-04，按用户提供的截图和五项要求修改。功能分支仍为 `feat/knowledge-base-chat`，本次产品提交 `80b489196796c5604ef78b2b3971e0d2486ad196`。

## 本次效果

1. 移除条目右侧的复选小框，保留整行点击、选中变色；辅助技术仍能读取选中状态。
2. 每项保留标题，以及下面一行灰字“简介 · 目录 · 几篇文档”。压缩行高，超长标题/简介/目录单行省略，悬停可读完整内容，文档数量保持可见。
3. 移除上一页/下一页按钮。首批加载 10 项，接近底部自动追加；追加期间保留当前列表和滚动位置。失败后原地重试，搜索或切换会话不会混入旧返回。
4. 最多允许 5 个知识库，前端、Go 校验及持久化读取上限一致；第 6 个被拒绝，已有选择不变。合成目录扩为 12 项，保留原来的 4 个 ID，方便检查跨 10 项滚动。
5. “输入框内灰色、小字号、仅标题的知识库链接”按用户要求留到后期。本次保留原可移除标签；Stage 2 真实目录接入后，使用服务端返回且经过可信地址校验的目录链接。点击移除与打开链接分开处理。

后期链接示例：`https://ai.ouchn.edu.cn/knowledge-base/08df2f18-e3db-4ea4-8cdb-6ad67e9185fa/data-collection`。详细要求已写入 [分阶段计划](superpowers/plans/2026-11-03-knowledge-base-chat.md) 与 [设计](superpowers/specs/2026-11-03-knowledge-base-chat-design.md)。没有给合成条目编造链接，也未实施 Stage 2。

当前仍是明确标记的测试目录；有选库时禁止发送并保留草稿。真实个人知识库、检索和 LLM 上下文注入不属于本次修订。Bridge 和 agentloop 功能分支未修改。

## 新测试包

- EXE：`C:/Users/yyzxc/.codex/worktrees/kb-desktop/runcode-desktop-main/cmd/runcode-desktop/bin/智开.exe`。
- 构建时间：2026-11-04 11:37（Asia/Shanghai）；产品 `zhikai`，版本 `1.0.20`。
- SHA256：`3D8D9EE68278F2E47EC6C3EFCA2A6B926BC312EAD5D6E8B3F8250BFF26D9B4DD`。
- 品牌脚本 `--brand zhikai --test --local-engine` 构建和 `--build-info` 均退出 0。
- 清单：`C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/logs/stage1-compact-desktop-build-manifest.json`。

构建时产品源码已提交；未提交内容只有文档，以及本轮开始前已存在、未读取或修改的用户文件 `docs/start.bat`。这是本地功能测试包，保留原有大 chunk 和重复 manifest 构建提示，尚不是发布候选。

## 验证

| 检查 | 结果 |
| --- | --- |
| 前端 typecheck、lint、测试 | 全部退出 0；42 文件、532 项测试通过 |
| 前端生产构建与 Windows EXE | 退出 0；沿用既有打包脚本 |
| Go `go test ./...` | 退出 0；19 个测试包通过 |
| 知识库 `go test -race ./internal/desktop -run TestKnowledge -count=1` | 退出 0；覆盖 5 库保存/重开、6 库拒绝且磁盘不变、10+2 分页无重复 |
| `go run ./tools/protogen --check`、`git diff --check` | 通过；协议结构未变 |
| 独立代码审查 | 无新增问题；检查滚动观察器、追加失败重试、异步隔离、5 库持久化一致性 |
| 新版窗口 | 进程/窗口路径确认是本次 EXE；界面已加载并恢复原有 3 个测试库标签 |
| 完整 GUI 验收 | 未完成：点击工具报告窗口最小化，恢复操作又触发焦点/用户输入检测；未把静态检查当作实际滚动和点击通过 |

原始 Go 日志在 `.codex-stage0/logs/stage1-compact-go-root-test.log`、`knowledge-compact-backend-{red,green}.log`（`C:/Users/yyzxc/Desktop/Client-Platform/` 下）；前端滚动/上限回归先观察失败后实现通过。新增测试覆盖追加保留首屏、并发触底去重、追加失败原页重试、旧搜索/旧会话追加返回失效。

## 便捷检查

1. 从任务栏还原新版窗口，或正常退出客户端后双击 `C:/Users/yyzxc/Desktop/Client-Platform/.codex-stage0/start-desktop.cmd`；不要打开安装目录里的旧版本。
2. 点击“+ → 知识库”，检查两行紧凑条目、选中变色、没有右侧小方框和翻页按钮。
3. 向下滚动，确认可继续看到第 11 项“采购指南”和第 12 项“学习资源”；再搜索“采购”，确认只有匹配结果，清空搜索后恢复列表。
4. 选择 5 项后第 6 项不可再选；取消其中一项后可以替换。切会话、重开后检查选择恢复；有选库时仍不能发送。标题暂不支持跳转，待 Stage 2 实现。

后续修改仍用同目录的 `build-desktop.cmd` 重建、`start-desktop.cmd` 启动、`check-desktop.cmd` 核对来源。版本号没有因本次修改递增，应使用路径、commit 和 SHA256 确认产物。
