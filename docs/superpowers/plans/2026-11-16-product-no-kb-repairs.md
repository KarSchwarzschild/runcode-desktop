# product-no-kb bug and redundancy repairs

**Goal:** Resolve BUG-01..12, GATE-01..03 and CLEAN-01..10 from `C:/Users/yyzxc/Desktop/1113client-platform/问题清单.md` on `feat/product-no-kb`, committing each verified group.

**Baseline:** 4bc0f40 plus existing uncommitted image size work. An external scratch snapshot preserves the initial changes. Existing automatic image generation and configurable/unfixed sizes take precedence over obsolete UI requirements in the audit.

**Architecture:** Retain desktop boundary policies, existing replay helpers, session lifecycle locks and SQLite WAL. Share URL/identity parsing and explicit capture outcomes. Persist capture-disabled intervals. UI effects follow session/generation identity.

**Constraints:** Do not modify Core/agentloop or restore knowledge-base features. Preserve initial uncommitted changes and never commit them incidentally. Fake Bridge only. GOWORK=off; preserve Go cache paths. Root owns staging/commits to avoid shared-index conflicts. Independent agents own disjoint production files; coordinate store lifecycle interfaces before editing shared files.

## Work and verification ledger

- [x] Gate: jsdom already restored; exclude native PrepareExit, regenerate protocol and remove unused lint items. protogen test/check, typecheck, lint and all 837 frontend tests passed. Align the stale catalog assertion with its actual no-free-promise intent and current copy.
- [ ] BUG-01 / CLEAN-01: Windows stream path protection with real resolver/Write regression, remove unused SVG aliases; commit.
- [ ] BUG-02: reuse existing image replay in Resume/Fork, add public API regressions; commit.
- [ ] BUG-03/04: stop new creation on exit, resnapshot under lifecycle lock, finish calls before closing usage; retain dirty recovery on incomplete close; commit.
- [ ] BUG-09/10/11/12: persist disabled coverage, freeze search URL scope, distinguish cancellation, prevent vision auxiliary suppression; focused HTTP/SQLite regressions; commit each cohesive fix.
- [ ] BUG-05/06/07/08: preserve foreground on background status, reset tenant save state, consume preview lifecycle, verify current automatic-generation copy; scoped hook/component regressions; commit.
- [ ] CLEAN-02..10: remove dead interfaces/wrappers and ineffective fields, share pure JWT parsing, simplify noop callbacks and obsolete CSS; preserve schema compatibility; focused tests; commit.
- [ ] Full verification: root Go tests/build, desktop build, server tests, vet, protogen, frontend test/typecheck/lint/build, 100K benchmark where relevant; independent final review.
- [ ] Update external issue status, execution guide and acceptance evidence with commit map and practical limitations.

## Review focus

Windows aliases must retain protected configuration boundaries. Resume/Fork must not resubmit or renew image budgets. Exit must handle in-flight creation, close failure and bounded repeated exit. Disabled capture must survive restarts without claiming zero usage. Session/tenant switches must discard stale results without changing foreground or keeping controls disabled.

## Progress

Initial inspection: GATE-02 dependency has already been restored in HEAD (`jsdom ^31.0.1`); BUG-08 old toggle and approval copy have already changed in automatic generation commits. Verify current behavior rather than revert it. Root `node_modules/` is unrelated untracked content and remains unstaged.
