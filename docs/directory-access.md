# 统一目录授权与来源预览

**本地 go.work 引擎联调，尚未发布 tag。** 三个 module 仍固定 v0.15.0；旧测试 EXE 不包含本轮改造。正式发布须先发真实引擎 tag、统一升级三处 require，再做 GOWORK=off 验证。本轮不打包、不发布、不部署。

## 使用

在目标会话的权限页打开「目录授权」，选择或输入目录，默认「只读 + 本次会话」。可选择「读写 / 本项目」，授权覆盖该目录及子目录，不自动提升到父目录。磁盘根目录需要额外确认；链接实际指向根目录时，须改填真实根路径再确认。

随后该会话的 ReadOffice、open_preview、产物卡和文件预览入口共用此授权，可用工作区相对路径或已授权外部绝对路径。目录列表展示来源会话/工作区、授权范围和仍覆盖它的其它规则。

撤销按具体目录、read/write、session/project 精确移除一条 allow，不删除 deny、其它 scope 或祖先授权。读写含读、只读不含写；安全模式可使用已有显式目录读授权，但不因此获得写入或执行能力。模式矩阵的外部读取行描述的是未授权情况。

## 权限边界

- 复用引擎 `.runcode/permissions.json`，不另建桌面白名单。每个 session overlay 独立；项目规则在每次授权边界读取有界快照，不依赖陈旧缓存。
- 项目 RMW 跨进程锁 + rooted 原子替换。损坏、超限、持久化失败明确拒绝，不换空 store、不降级成会话授权。项目文件上限 1 MiB / 4096 条，批量和 session 规则也有界。
- session/project 批准的是弹窗展示的目录范围；once 只覆盖实际展示的工具及具体目标，不创建目录规则、不开放兄弟文件。不同目标的并发 once 不共享答复；standing 目录批准仍可合并询问。
- Bash 必须同时满足目录可达性和独立操作授权。保留 OA 外层 gate、提权人工关、危险命令限制及计划模式。
- 授权不改变 CWD，不加载额外目录中的配置、指令、技能、hooks 或 MCP；不新增外部目录浏览和外部文件编辑复审。
- check-only 读取不弹审批、不调模型、不跑 hooks、不执行工具、不写盘。UI 未获准读取时提示用户授权，不暗中变成新的审批来源。

## 接线

`file_read_resolver.go` 在通用分类前显式解析两个工具的 path，生成 canonical 文件 read action。InputPresentation 不参与授权。

执行器只为实际允许的具体读取调用提供 `tool.Context.ReadAccess`；`permissions.ReadPermitFor` 不能取出其它目标的凭据。OutsideAllowed 布尔值不是任意外部读取权限。ReadOffice 从授权句柄解析 ZIP/工作簿，不再重新按可变源路径打开文件。

预览先调用 `OpenArtifact(sessionID,path)`，后续只传后端引用 ID：ReadArtifact、ReadArtifactBytes、RenderOfficePDF、OpenExternal、RevealInFolder、ResolveArtifactPath；关闭调用 ReleaseArtifact，不删除源文件。不保留裸路径或焦点回退。

引用绑定 session、host generation、canonical 单文件目标与权限依据。前端 tab、diff、自动预览、菜单均携带来源；切走再切回不会复活旧请求。每次签发独立引用，临时菜单动作不会释放已打开标签的引用。

引用最长 4 小时，全应用最多 512 个、每会话实例 64 个，到期须重开。文本上限 2 MiB，二进制/HTTP 文件 25 MiB。

## HTTP / Office

监听器按工作区共享，授权不共享。只服务 `/<reference>/file/<卷根相对路径>` 和 `/<reference>/pdf`，不再裸 FileServer 整个工作区。GET/HEAD/Range 均检查来源实例和 live permit；HTML 相对资源独立检查 standing 读取权。once HTML 不顺带开放兄弟文件。卷根布局使正常 `../assets` 保留引用前缀；跨出引用路由不能得到权限。目录不枚举，旧 `.runcode/preview-cache` URL 不服务。

响应 no-store、no-referrer、nosniff；回环地址不是浏览器/OS 沙箱。文件读取使用真实路径和 rooted open，拒绝链接逃逸或目标改变。

Office 转换从授权句柄生成私有临时快照，转换器不拿原可变路径；只清理自己生成的临时目录。PDF 仅在引用绑定的内存缓存保存，合计不超过 64 MiB；缓存命中/发送仍查源权限与版本。转换等待可取消；无转换器或 HTTP listener 时走原前端渲染退路。不改变 Office 宏/外链设置，不宣称隔离网页或 Office 后续网络行为。

## 生命周期

辅助读取与回合并行，通过 host operation 接入取消/等待，不在 App/host mutex 下做文件 I/O。每会话最多 8 个辅助操作；预览全进程并发 4，操作最长 90 秒；HTTP 另有限流和写超时。

关闭先取消并等待；失败保留 closing 状态和引用供重试，拒新读取，前端不提前丢 tab。关闭成功后清引用。同逻辑会话重建只显式恢复 session 目录规则，不复制命令 grant 或 once；旧 generation 引用失效，可按仍有效权限重开。

普通关闭重开和提问分支不继承 session grants；项目授权仍属于工作区。撤销后新读取重查真实规则，桌面尽力取消已知受影响的在途预览；其它进程的修改在后续权限边界生效，不依赖 watcher。异步结果交付前再查权限，错误不返回文件内容。

不能收回已显示/发送/交给系统应用的字节，不终止用户自己的外部程序。Rooted 读取不是全平台 OS 沙箱。

## 验证范围

引擎测试覆盖跨会话/进程事务、scope、deny、目录/执行组合、once 目标与 standing 合并。桌面覆盖 GET/HEAD/Range、切焦点、撤销、PDF 缓存、关闭等待/失败、同 ID 代际和授权后链接重定向。

`TestDirectoryOfficePreviewActualHostFlow` 用隔离 App、真实 host/engine、最小 DOCX、本机合成 SSE 模型执行「授权 → ReadOffice/open_preview → 切焦点 → 撤销」，不碰账号或付费模型。

`?preview=directory-access` 是开发组件入口。隔离 Edge 驱动真实 React 钩子/组件，检查授权/撤销、根确认、失败提示、切换与迟到结果，以及 1024px/1280px 布局。它不是原生 Wails/真实 Office 验收。

Windows 普通 symlink 创建缺权限时明确 skip；无需提权的目录 junction 用例实际执行。其它 OS GUI、真实 Office 自动化、跨盘真机行为仍须目标环境验收。跨平台编译不替代真机测试。
