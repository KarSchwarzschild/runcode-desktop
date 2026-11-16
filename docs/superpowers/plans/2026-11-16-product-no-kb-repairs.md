# product-no-kb bug and redundancy repairs

**Goal:** Resolve BUG-01..12, GATE-01..03 and CLEAN-01..10 from `C:/Users/yyzxc/Desktop/1113client-platform/问题清单.md` on `feat/product-no-kb`, committing each verified group.

**Baseline:** 4bc0f40 plus existing uncommitted image size work. An external scratch snapshot preserves the initial changes. Existing automatic image generation and configurable/unfixed sizes take precedence over obsolete UI requirements in the audit.

**Architecture:** Retain desktop boundary policies, existing replay helpers, session lifecycle locks and SQLite WAL. Share URL/identity parsing and explicit capture outcomes. Persist capture-disabled intervals. UI effects follow session/generation identity.

**Constraints:** Do not modify Core/agentloop or restore knowledge-base features. Preserve initial uncommitted changes and never commit them incidentally. Fake Bridge only. GOWORK=off; preserve Go cache paths. Root owns staging/commits to avoid shared-index conflicts. Independent agents own disjoint production files; coordinate store lifecycle interfaces before editing shared files.

## Work and verification ledger

- [x] Gate: jsdom already restored; exclude native PrepareExit, regenerate protocol and remove unused lint items. protogen test/check, typecheck, lint and all 837 frontend tests passed. Align the stale catalog assertion with its actual no-free-promise intent and current copy.
- [x] BUG-01 / CLEAN-01: Windows stream paths preserve original policy; resolver/Write/Edit regressions; `c2d8de4`.
- [x] BUG-02: public Resume/Fork restore existing image results without resubmission or renewed budget; `7d82dfa`. Independent review caught unrelated corrupt-ledger suppression; isolated read-only replay recovery in `d1ee774`, strict submission loader retained.
- [x] BUG-03/04: final lock-protected snapshot, terminal exit creation gate, session Finish before usage close and legacy pending recovery; `4fe739c`. Stable repeated exit result `5cb5f57`; recording admission also gated `4395abf`.
- [x] BUG-09/10/11/12: durable disabled intervals `bbc78d7`, URL-frozen search scope and explicit cancellation `7e5303e`, vision-only auxiliary-filter bypass `5ab5f3f`. Real HTTP/SQLite and process restart regressions passed.
- [x] BUG-05/06/07/08: foreground identity guard `159500a`; scoped save reset `d15f0bb`; preview invalidation/release `e0dcb08`, creation ready `1430f45`, one ready per rebuild `30b1651`. Current auto-generation copy verified.
- [x] CLEAN-02..10: unused resume local `4fe739c`; search wrapper `7e5303e`; pure JWT parser/noop callbacks `ec7e539`; old CSS/comments `356ef10`; production test APIs, snapshot compatibility, ignored RunID and unused scope writes `13a7248`. Historical schema columns retained.
- [x] Full verification: root Go tests/build, desktop build, server tests, vet, protogen, frontend test/typecheck/lint/build, 100K benchmark and independent review completed; evidence below.
- [x] Updated external README, issue status, execution guide and acceptance evidence with commit map and limits. Previous audit documents preserved under `archive/2026-11-16-before-repairs/`.

## Review focus

Windows aliases must retain protected configuration boundaries. Resume/Fork must not resubmit or renew image budgets. Exit must handle in-flight creation, close failure and bounded repeated exit. Disabled capture must survive restarts without claiming zero usage. Session/tenant switches must discard stale results without changing foreground or keeping controls disabled.

## Progress

Initial inspection: GATE-02 dependency has already been restored in HEAD (`jsdom ^31.0.1`); BUG-08 old toggle and approval copy have already changed in automatic generation commits. Verify current behavior rather than revert it. Root `node_modules/` is unrelated untracked content and remains unstaged.

Independent review exercised Windows trailing-dot aliases, public replay and budgets, lifecycle/recorder admission, preview close/reopen, coverage intervals and search scope. Confirmed findings were repaired before finalization. The `d1ee774` committed-source archive independently passed selected Go tests in four packages, frontend typecheck and all 842 frontend tests; working-tree frontend (including unrelated parallel UI/size work) passed 844 tests, typecheck/lint/build. This distinction prevents crediting uncommitted source to the repair commits.

Full root testing first exposed two timing-dependent localusage coverage assertions under package load. Investigation and final rerun are recorded below; do not represent that initial run as passing.

`a74e209`: measured a real Windows timer-delivery edge (parent Err=nil after absolute deadline). Coverage writes now construct an explicit minimum deadline, so already expired work is synchronously canceled before SQL while the in-memory gap remains visible. Existing 200ms/10ms budgets and blocked-SQL regression remain. SQL capacity is tested in a real fixture transaction; memory capacity has its own regression. Full localusage and 60 focused repetitions across CPU=1/4/16 passed; independent review and count=5 reruns passed.

## Final validation

- Final `go test ./... -count=1` passed: desktop 51.581s, localusage 3.780s, protogen 2.615s. Subsequent root build, relevant vet and protogen check passed.
- After the final code commit, desktop module build and server module tests passed (server 1.924s). Root Go build also passed before the final budget regression was found.
- Working-tree frontend typecheck/lint/test/build passed: 71 files / 844 tests; Vite 2900 modules, 23.73s. Vite retains its existing large-chunk warning.
- `BenchmarkQuery100K -benchtime=1x -benchmem` passed: Page 99.50ms; 600-bucket overview 346.72ms; dimensions 557.76ms; concurrent writes 666.91ms and max write 2.511ms. These are single samples.
- `git diff --check` passed. Tests used fake Bridge/providers and isolated temporary SQLite/workspaces. CGO=0: race was not run. Native signed installers and real OAuth/OA/paid providers remain outside this verification.
- Initial size edits and subsequent parallel UI edits remain outside these repair commits. In overlapping settings code, only the saving reset was staged; shared usage code was committed by owned hunks. No push or publication performed.
