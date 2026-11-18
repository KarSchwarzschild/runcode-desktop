# Automation implementation log

## Step 0 — baseline

| Field | Value |
| --- | --- |
| Date | 2026-11-18 |
| Implementation worktree | `C:\Users\yyzxc\.codex\worktrees\automation-tasks-1118\imagegen-client` |
| Branch | `codex/automation-tasks-1118` |
| Start HEAD | `59cb3eeb7d5da1262a1ba7dd9f908143d42436aa` (`feat/upgrade-1118` tip) |
| Spec | `docs/automation/01-自动化任务执行方案.md` |
| Handbook | `docs/automation/02-自动化任务分步执行手册.md` |

### Main checkout (left untouched)

Path: `C:\Users\yyzxc\Desktop\Client-Platform\imagegen-client` on `feat/upgrade-1118`.

Dirty at Step 0 start (not carried into this worktree):

- `cmd/runcode-desktop/build/config.yml`, `build/darwin/Info.plist`, `build/windows/info.json`
- `frontend` inline-preview / static-html / static-detect edits
- `.build-logs/`

### Toolchain (this session)

```
GOWORK=off
GOPRIVATE=gitlab.ouc-online.com.cn
go 1.26.0 windows/amd64
gcc 8.1.0 (mingw)
node v24.20.0
npm 11.19.0
```

### Baseline commands

| Command | Result |
| --- | --- |
| `go test ./internal/desktop ./internal/protocol` | PASS (`desktop` ~44s) |
| `go run ./tools/protogen --check` | PASS |
| `npm run typecheck` (frontend) | PASS |
| `npm run lint` (frontend) | PASS |
| `npm test` (vitest) | **1 fail / 902 pass** — `inline-file-preview.test.tsx` expects img click → open side preview (known stale vs `59cb3ee`; not part of automation). Logs in `_baseline/` |

Step 0 complete.

## Step 1 — schedule

| Field | Value |
| --- | --- |
| Commit | `feat(automation): add validated schedule calculation` |
| Tests | `go test ./internal/automation -run 'TestNextOccurrence|TestScheduleDST|TestValidateSchedule|TestCountOccurrencesBounded' -count=1` → PASS |
| Files | `internal/automation/{types,schedule,schedule_test}.go` |

## Step 2 — store

| Field | Value |
| --- | --- |
| Commit | `feat(automation): persist tasks runs and session links` |
| Tests | `go test ./internal/automation -run TestStore -count=1` → PASS |
| Race | `go test -race` failed with Windows `0xc0000139` (toolchain DLL); non-race suite green |
| Files | `internal/automation/{errors,store_types,migrations,store,store_test}.go` |

## Step 3 — scheduler service

| Field | Value |
| --- | --- |
| Commit | `feat(automation): schedule bounded background runs` |
| Tests | `go test ./internal/automation -count=3` → PASS |
| Files | `internal/automation/{service,runner,service_test,fake_clock_test}.go`; Recover leaves queued for re-check |
| Notes | Global active=1; per-task queued≤1; miss>60s aggregated; queue wait>60s → busy; 60m timeout holds slot until Fact; Observe non-blocking |

## Step 4 — desktop runner and identity

| Field | Value |
| --- | --- |
| Commit | `feat(automation): run isolated sessions with bound identities` |
| Tests | `go test ./internal/desktop ./internal/automation ./internal/codexauth ./internal/codexproxy -count=1` → PASS (`desktop` 49.2s, `automation` 1.5s, `codexauth` 1.5s, `codexproxy` 1.1s) |
| Race | Not run. Step 2 `-race` on this Windows toolchain exits `0xc0000139`. This step does not claim a race PASS. |
| Files | `internal/desktop/automation_{runner,identity,lease,runner_test,identity_test}.go`; guards in `app.go`, `questions.go`, `turn.go`, `sessions.go`, `session_settings.go`, `mcp.go`; identity in `tokens.go`, `codex.go`, `custommodels.go`, `internal/protocol/custommodels.go`, `internal/codexauth`; receipt merge in `internal/automation/{runner,service}.go` |

Notes:

- Runs open with `buildSessionModeHeld(..., focus=false)` under `startMu`. They do not call `OpenSession`. Workspace comes from the task. `AllowSteering` is forced off. The lease is installed before submit.
- `turn:end` with `Stopped=true` is interrupted, not success. A success that arrives first is kept. Timeout and other errors map to `timed_out` and `failed`. Emit only queues a fact; the unfocused session closes on a later worker.
- Passport identity is issuer+subject plus the selected tenant. Codex OAuth persists `iss`/`sub`; a login that has an account id but no subject must sign in again. Static keys use `credentialRevision`, bumped only when the key actually changes. Old configs with a key and no revision count as 1.
- The Codex proxy package is unchanged. While a run is leased, the desktop resolver checks the bound subject or key revision before it returns a bearer.
- Not accepted here: a real Passport, Codex, or OA round trip, or the Windows race detector.

Next: Step 5 command facade and protocol.
