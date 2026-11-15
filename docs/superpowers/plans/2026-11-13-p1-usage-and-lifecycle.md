# P1 Usage and Image Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 修复平台用量租户隔离、生图提交边界、localusage 流生命周期和 usageStore 关闭竞态，保证统计与实际请求一致。

**Architecture:** 平台 provider 从每个会话配置快照绑定租户，禁止依赖当前焦点会话；生图在进入 HTTP `Do` 前完成所有可拒绝检查，进入 `Do` 后固定为 attempted；localusage 使用明确的 done 信号和一次性 finish；App 对 usageStore 使用稳定生命周期管理。

**Tech Stack:** Go、`testing`、SQLite-backed `internal/localusage`、agentloop `llm.Provider`。

**Spec:** `C:/Users/yyzxc/Desktop/1113client-platform/问题清单.md` 的 P1 条目。

## Global Constraints

- 生图请求只发送一次；进入 HTTP `Do` 后超时、取消、断连均保持 `attempted`。
- BaseURL 缺少合法 `/t/<tenant>`、账号为 opaque token 或身份不可验证时，不写平台用量统计。
- usageStore 关闭与查询不得产生 nil 解引用、use-after-close 或静默半页结果。
- 使用 `$env:GOWORK='off'` 运行 Go 检查；不修改 Core/agentloop。

## Review Focus

- 后台会话租户与前台租户切换并发时，LLM 与 vision 调用仍归各自租户。
- 生图在目录、账本、token、请求构造或 ctx 取消失败时不产生图片调用；进入 `Do` 后永不降级为未提交。
- 消费者提前停止读取且底层 Events 通道不关闭时，pump 必须退出且底层流被 Close。
- Shutdown 与并发查询交错时，查询必须获得稳定结果或明确错误。

### Task 1: Tenant-bound platform usage capture

**Files:**
- Modify: `internal/desktop/usage_capture.go`
- Test: `internal/desktop/usage_capture_test.go`

- [x] 为两个会话、两个租户的 LLM/vision 并发调用增加失败测试，并覆盖缺租户、opaque token、非法租户和前台切换。
- [x] 从 `llm.Config.BaseURL` 严格解析 `/t/<tenant>`，在 provider 工厂闭包捕获租户；身份仍从稳定 JWT issuer+sub 获取。
- [x] 运行 `go test ./internal/desktop -run 'Test.*Usage|Test.*Vision' -count=1`。

### Task 2: Image generation submission boundary

**Files:**
- Modify: `internal/desktop/image_generation_client.go`
- Test: `internal/desktop/image_generation_client_test.go`, `internal/desktop/image_generation_usage_boundary_test.go`

- [x] 增加配置、目录、账本、token 和 ctx 取消在 HTTP `Do` 前失败的断言：图片调用数为 0，结果和账本为 `not_submitted`。
- [x] 增加 HTTP `Do` 后超时/取消的断言：结果和账本保持 `attempted`，不重试。
- [x] 让 before-Do 回调只使用冻结 scope，并确保 marker/统计失败不阻止唯一一次 HTTP 请求或触发重试。
- [x] 运行生图 client、flow、lifecycle 定向测试。

### Task 3: localusage counting stream lifecycle

**Files:**
- Modify: `internal/localusage/provider.go`
- Test: `internal/localusage/provider_test.go`

- [x] 先增加底层 Events 通道保持打开、消费者提前 Close 的失败测试。
- [x] 创建 stream 时初始化 done；pump 监听 done；finish 使用 CAS，保证 done 和 finish 各执行一次；Close 先停止底层流。
- [x] 运行 `go test ./internal/localusage -count=1`。

### Task 4: usageStore shutdown lifecycle

**Files:**
- Modify: `internal/desktop/app.go`, `internal/desktop/usage_capture.go`, `internal/desktop/usage.go`, `internal/desktop/usage_local.go`
- Test: relevant desktop usage tests

- [x] 增加 Shutdown 与并发 Query 的失败测试。
- [x] 用生命周期锁或稳定指针策略保护取用、关闭和查询，避免先判空后解引用。
- [x] 运行桌面用量测试并验证 race-sensitive paths。

### Final verification

- [x] `go test ./internal/localusage ./internal/desktop -count=1`
- [x] `go vet ./internal/desktop ./internal/imagegentool`
- [x] `go build ./...`
- [x] `git diff --check` and review the final diff before one P1 commit.

## Verification results

- RED reproduced tenant mixing for both LLM and vision, the token lock held across image bookkeeping, and shutdown racing an in-flight query. Each regression passed after its fix.
- The existing image host usage test counted automatic title traffic as chat traffic; its fixture now checks two chat calls and one image call in their actual Bridge scopes.
- PASS: root `go test ./... -count=1`, server module tests, root and desktop module builds, and `go vet ./internal/desktop ./internal/imagegentool ./internal/localusage`.
- Windows runtime-probe fixture builds inherit an isolated user profile. The final root test run explicitly preserved `GOMODCACHE` and `GOCACHE` so the child Go build could use the already installed dependencies; an earlier run timed out downloading `golang.org/x/sys`.
- Independent review found no blocking issues. Stream-close tests passed 20 repeated runs; shutdown, URL parsing, and image-boundary tests passed 10 repeated runs.
- Race detector unavailable on this host (`CGO_ENABLED=0`, no C compiler); concurrency regression tests passed without `-race`.
