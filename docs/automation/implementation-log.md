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

## Step 5 — desktop commands and events

| Field | Value |
| --- | --- |
| Commit | `feat(automation): expose scoped task commands and events` |
| Tests | `go test ./internal/desktop ./internal/automation ./internal/protocol -count=1` → PASS (`desktop` 45.952s, `automation` 1.398s, `protocol` no test files) |
| Protocol | `go run ./tools/protogen` then `go run ./tools/protogen --check` → PASS |
| Frontend | `npm run typecheck` in `cmd/runcode-desktop/frontend` → PASS |
| Race | Not run. Step 2 `-race` on this Windows toolchain exits `0xc0000139`. This step does not claim a race PASS. |
| Files | `internal/desktop/{automation,automation_test,app,exit,passport,codex,protocol_aliases,automation_lease,automation_runner,automation_runner_test}.go`; `internal/protocol/{automation,commands,events,session,conversation}.go`; `internal/automation/{store,service}.go`; `tools/protogen/extract.go`; generated `frontend/src/core/protocol/{types,commands,events}.ts`; `frontend/src/core/bridge.ts` |

Notes:

- `New` does not open the task database. `Startup` opens `UserConfigDir/runcode/automation/<AppProduct()>/automations.sqlite3`, then Recover and Service.Start. A bad product segment or open/start failure leaves chat usable and `defaults.available=false` with a reason that does not include the path. `PrepareExit` stops the service before closing the store. `HoldAdmission`, `PrepareShutdown`, and `ExitSummary` stay reachable for Step 10.
- The authorized owner set is host-computed. A non-empty client `ownerScope` is rejected. Local static/Codex API-key tasks stay on `local:<product>:<user hash>` when the selected model changes. Passport and Codex OAuth owners join the set only with a subject (and, for Passport, a tenant and bridge). The same subject with a new `jti` does not bump the epoch. Login, logout, and tenant change bump the epoch and emit `automation:context_changed` before later content.
- `defaults.bindingVersion` is an in-memory stamp of the current binding digest and static credential revision. The value stored on the task stays schema `BindingVersion` 1, because run validation requires that constant. Create checks an existing `clientRequestId` across still-authorized owners before it checks the stamp, so a later key change returns the original task instead of creating a second one.
- Data commands re-check the context after an optional pre-return hook. Task and run events are emitted only after persist, and only while that owner is still authorized. Host envelopes keep Core `seq` and payload. `automationSource.context` is the context at emit time. An unauthorized lease still finishes internally and is not forwarded. Process automation events use an empty `sessionId`.
- `GetAutomationConversation` does not change focus. A busy or not-ready live session returns `historyPending=true` and `blocks=[]`. An idle in-manager session is read under `WithIdleSession` and passed through `IdentifyQuestions`. A missing transcript is `not_found`. `OpenAutomationSession` on a live session only focuses it. A closed session is resumed from the session link's workspace.
- Successful create records the workspace with `mergeRecentWorkspaces` and `updateRawConfig` only. It does not change CWD, Resume, or the connection. If that write fails, the task remains and the result warning says the directory history was not updated.
- SQLite uses one connection. Listing ids and then loading each row while that result set was still open deadlocked `TaskByClientRequestForOwners`, `claimDueIntoQueue`, and `reloadQueued`. Those paths now close the rows before the follow-up query.
- `bridge.onEnvelope` returns `DesktopEnvelope<P>`. Generated `events.ts` still delivers the Core `Envelope`. Pages call the new commands through `bridge`, not `Call.ByName`.

Deferred:

- Step 6 fills `OpenSessionInfo` / `SessionSummary` / `ConversationInfo.automation` from `session_link` and projects execution cards. Step 5 only adds the optional field. Kind stays `agent`.
- Step 10 supplies the Windows `WindowController`, tray, and confirm-exit. A nil controller returns `automation_unavailable`. `BackgroundSupported` stays false.
- No real Passport, Codex, or OA round trip. No race-detector PASS.

Next: Step 6 execution cards, history projection, and cross-workspace open.
