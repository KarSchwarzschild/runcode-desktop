# P2 Usage Correctness and Performance Implementation Plan

> **For agentic workers:** Use systematic debugging and test-driven development; run focused tests before the final integrated verification.

**Goal:** 修复问题清单的三项 P2：本机用量查询性能、统计契约、不可归属采集的可见性。

**Spec:** `C:/Users/yyzxc/Desktop/1113client-platform/问题清单.md` 的全部 P2 条目。

**Architecture:** WAL 读连接与写入隔离，分页和维度交给 SQLite；总览使用同一水位的 SQL 聚合。桌面端固定上海日历范围，映射已知 Token 样本、状态、采集起点与缺口。未能归属的请求只记录有限安全诊断，不写入任何假定的统计 scope。

## Constraints

- 不修改 Core/agentloop，不处理 P3 清理项。
- 保留 P0/P1 边界；pending 不进入已完成请求数与请求日志，状态分布可显示在途。
- 任一侧 Token 已知即纳入已知合计，完整覆盖仍要求输入输出均已知；未知不得当成 0。
- SQL 查询有时限且可取消，写入预算 200ms；趋势最多 600 桶。
- 历史缺失状态保持 unknown；不从错误码推断请求提交情况。

## Tasks

- [x] localusage：读写生命周期、SQL keyset 分页、GROUP BY 维度、总览聚合、10 万条调用并发写入基准。
- [x] coverage：持久化有限安全诊断与存储失败回退；读取 scope 采集起点、崩溃缺口；账号/租户缺失不能显示完整。
- [x] desktop：近 N 天从第 N-1 天零点开始，all 使用 scope 起点；总览/分页/维度固定 snapshotSeq；部分 Token 与状态映射。
- [x] frontend：已知 Token 合计与样本、采集与存储状态、状态分布、缺口和历史未知值；查询沿用总览水位。
- [x] 验证：Go 和前端定向测试、10 万条基准、独立审查、全量测试、构建和类型检查。

## Verification environment

Go commands use `GOWORK=off`; root tests preserve the existing `GOMODCACHE` and `GOCACHE` for Windows test subprocess builds. This host has no C compiler and `CGO_ENABLED=0`, so race detection is unavailable.

## Verification results (2026-11-15)

- Root `go test ./... -count=1`: passed, including the full desktop package and Windows runtime fixture test.
- Server module `go test ./... -count=1`: passed.
- Root and desktop module `go build ./...`: passed.
- `go vet ./internal/desktop ./internal/localusage ./internal/protocol`: passed.
- `go run ./tools/protogen --check`: passed; generated protocol files are synchronized.
- Frontend `npm test`: 70 files / 873 tests passed. `npm run typecheck`, `npm run lint`, and `npm run build` passed; build retains existing large-chunk warnings.
- Independent review findings fixed with RED/GREEN regressions: all token calls form the sample denominator; failed writes and their coverage diagnostics share the same 200ms deadline. Missing/invalid historical finish statuses remain unknown.
- `go test ./internal/localusage -run '^$' -bench BenchmarkQuery100K -benchtime=1x -benchmem`: passed on Windows / Intel Core Ultra 5 225H. 100,000 calls: page 105.85ms / 267,904 bytes; 600-bucket overview 307.32ms / 1,400,224 bytes; dimensions 847.37ms / 1,146,432 bytes. Concurrent queries plus 20 Begin/Finish pairs completed in 698.94ms, all 40 writes succeeded, maximum single write 13.79ms. Repeated concurrent benchmark (`-benchtime=2x`) also passed, maximum write 16.00ms. These are local single-run measurements, not cross-machine latency guarantees.
