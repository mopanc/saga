# Saga — Public Release Plan

**Status:** pre-flight (private repo, dev branch `pivot/v2-go`)
**Target version:** v1.0.0-beta.1
**Owner:** @mopanc
**Last updated:** 2026-05-05

This document is the canonical playbook for taking Saga from *"private repo, working"* to *"public, distributable, multi-client validated, security-hardened"*. Phases are sequential — do not skip. Tick boxes as work completes.

---

## Phase 0 — Reference state (current)

- [x] Branch dev: `pivot/v2-go`
- [x] Repo: private GitHub
- [x] No formal governance, no CI
- [x] Saga functional end-to-end on Claude Code
- [x] 21 personal topics in production use
- [x] Project layer initialised in this repo (2026-05-05)

---

## Phase 1 — Governance & files (1 day)

Branch `chore/public-release-prep`, single PR with all of:

- [ ] `LICENSE` — Apache 2.0, *"Copyright 2026 Jorge Morais"*
- [ ] `CONTRIBUTING.md` — issue/PR flow, Conventional Commits, dev setup
- [ ] `CODE_OF_CONDUCT.md` — Contributor Covenant 2.1, contact `jorgemopanc@icloud.com`
- [ ] `SECURITY.md` — private CVE email, response SLA, optional GPG, 90-day embargo
- [ ] `.github/CODEOWNERS` — `* @mopanc`
- [ ] `.github/ISSUE_TEMPLATE/bug_report.yml` — modern YAML form
- [ ] `.github/ISSUE_TEMPLATE/feature_request.yml`
- [ ] `.github/PULL_REQUEST_TEMPLATE.md` — checklist for tests/docs/breaking/issue link
- [ ] `.github/dependabot.yml` — Go modules + GH Actions, weekly
- [ ] `README.md` — remove "Repositório actualmente privado…" section; add badges

**DoD:** PR open with all files; Phase 2 unblocked.

---

## Phase 2 — CI/CD (1 day)

| File | Trigger | Steps |
|---|---|---|
| `.github/workflows/ci.yml` | push + pull_request | golangci-lint → govulncheck → `go test ./...` → `goreleaser build --snapshot --clean` |
| `.github/workflows/release.yml` | tag `v*` | `goreleaser release --clean` (matrix darwin/linux/windows × amd64/arm64) |
| `.github/workflows/codeql.yml` | push + PR + cron weekly | CodeQL Go analysis |
| `.goreleaser.yaml` | (root) | Matrix builds, archives, brew tap, conventional-commits changelog |
| `.golangci.yml` | (root) | Strict: errcheck, gosec, govet, ineffassign, staticcheck, unused, gofumpt, gocritic |

**DoD:** Phase-1 PR runs green. `goreleaser build --snapshot` locally produces 6-platform binaries.

---

## Phase 3 — Repo hardening (30 min — only after Phase 2)

### Branch protection on `main`

- [ ] Require pull request before merging (1 approval — even solo)
- [ ] Require status checks: `ci/lint`, `ci/test`, `ci/build`, `codeql`
- [ ] Require branches up to date before merging
- [ ] Require conversation resolution
- [ ] Require **signed commits** (configure GPG first)
- [ ] Require **linear history** (forces squash/rebase)
- [ ] Restrict pushes (PR-only)
- [ ] **Apply rules to admins** ← without this, protections are theatre
- [ ] No force push, no deletion

### Tag protection

- [ ] Pattern: `v*`
- [ ] Require admin

### Settings → Code security

- [ ] Dependabot alerts + security updates
- [ ] Secret scanning + push protection
- [ ] Code scanning (CodeQL)

**DoD:** `git push origin main` directly fails. Dependabot opens first PR.

---

## Phase 4 — Security audit (3-4h)

Initial findings already captured: see `.saga/topics/security-audit-pre-public-release.md` (project layer).

### Automated tools

- [ ] `brew install gitleaks` then `gitleaks detect --source . --log-opts="--all"` — **secrets in current AND history**
- [ ] `govulncheck ./...` — known CVEs in deps
- [ ] `gosec ./...` — security hot-spots
- [ ] `golangci-lint run` (strict config from Phase 2)
- [ ] depguard (own tool) — bonus signal, dogfooding

### Manual review status (initial pass 2026-05-05)

Already verified safe ✓:
- Slugify is path-traversal-proof (`[a-z0-9-]+` only, see `internal/saga/render.go:41`)
- SQL queries parameterised, FTS5 query sanitised against keyword injection
- YAML parsing uses yaml.v3
- No hardcoded secrets in source
- No `/Users/frontkom/...` paths in source
- Single author email in git history (`jorgemopanc@icloud.com`)

Pending fixes (see audit topic):
- [ ] BM email exposure (4 occurrences in tests + DESIGN_v2.md)
- [ ] Internal product name (scrubbed) used as example fixtures
- [ ] Decision point: rewrite history with `git-filter-repo`, or accept exposure
- [ ] Confirm `references[].path` cannot be weaponised (verify indexer doesn't `os.Open` it)

**DoD:** all tools green; audit topic updated with all findings + fixes; if non-trivial issues remained, commit `docs/SECURITY_AUDIT_v0.3.md` for transparency.

---

## Phase 5 — Multi-client validation (½ day)

Minimum: **3 clients beyond Claude Code** before claiming multi-IDE support.

| Client | MCP config | Test queries | Notes |
|---|---|---|---|
| Claude Code | [x] | [x] | hook + MCP, full auto-injection |
| Cursor | [ ] | [ ] | MCP only, no auto-injection |
| Windsurf | [ ] | [ ] | MCP only |
| Antigravity (Google) | [ ] | [ ] | MCP only |
| ChatGPT Desktop | [ ] | [ ] | optional |

For each: document config snippet in README under "Tested MCP clients". Explicit note about auto-injection being Claude Code-only.

**DoD:** README "Tested with" table lists 4+ clients confirmed.

---

## Phase 6 — Merge + tag (30 min)

```bash
gh pr merge <fase-1-PR> --squash --delete-branch
git checkout main && git pull
git tag -s v1.0.0-beta.1 -m "v1.0.0-beta.1 — first public release candidate"
git push origin v1.0.0-beta.1
# release.yml fires, goreleaser publishes
```

**DoD:** Releases page has `v1.0.0-beta.1` with binaries for 6 platforms + checksums + auto-generated changelog.

---

## Phase 7 — Distribution (1h)

- [ ] Create Homebrew tap repo: `mopanc/homebrew-tap` (goreleaser publishes formula automatically)
- [ ] README badges: build status, latest release, license, Go version, Go report card
- [ ] Verify `pkg.go.dev` indexes correctly (auto)

After this: `brew install mopanc/tap/saga` works.

---

## Phase 8 — Make public (5 min, **irreversible**)

⚠️ **Pre-condition:** gitleaks ran clean over full history. Once public, the history is mirrored across the internet (Wayback Machine, archive.org, GHArchive). Rewriting history after going public is practically impossible.

Settings → Danger Zone → Change visibility → Public.

---

## Phase 9 — Announce (your timing)

Do not announce before living with Saga in each client for at least a week — you want feedback grounded in real use, not demos.

When ready:
- LinkedIn post tied to `mopanc.github.io` portfolio
- Show HN: *"Saga — Topic-grained RAG memory for AI coding agents"*
- `/r/golang`, `/r/LocalLLaMA`, `/r/mcp`
- PR to `awesome-mcp`, `awesome-go`
- Add Saga case study to `mopanc.github.io/projects`

---

## Effort summary

| Phase | Effort | Blocks |
|---|---|---|
| 1. Governance files | 1 day | Phase 2 |
| 2. CI/CD | 1 day | Phase 3 |
| 3. Repo hardening | 30 min | Phase 6 |
| 4. Security audit | 3-4h | Phase 8 |
| 5. Multi-client | ½ day | Phase 9 |
| 6. Merge + tag | 30 min | Phase 7 |
| 7. Distribution | 1h | Phase 9 |
| 8. Make public | 5 min | Phase 9 |
| 9. Announce | as desired | — |

**Total: ~3-4 days of focused work**, distributable over 2 weeks alongside day-job commitments.

---

## Audit summary at-a-glance (2026-05-05)

Full detail: `.saga/topics/security-audit-pre-public-release.md`

**Verdict: 2 medium findings, 1 informational. No criticals.**

| Finding | Severity | Where | Recommended action |
|---|---|---|---|
| BM email exposure | **Medium** | `parser_test.go:21`, `DESIGN_v2.md:156/224`, indexer test fixture | Replace with placeholder (`alice@example.com`) |
| Internal product name as example fixtures | **Low-Medium** | `cmd_mcp.go`, `parser_test.go`, `DESIGN_v2.md` (multiple), `README.md:71`, `baseline_test.go:158` | Replace with generic (`acme-platform`) |
| Old TypeScript code in deleted history | **Informational** | `packages/core/*` deleted files | Verify clean via gitleaks before public |
| Slugify path-traversal-proof | ✓ Safe | `internal/saga/render.go:41` | None |
| SQL + FTS5 properly parameterised | ✓ Safe | `internal/saga/service.go`, `sanitize.go` | None |
| Single author email in history | ✓ Safe | `git log --all` | None |

### Critical decision point

History rewrite (with `git-filter-repo` to scrub the BM email and possibly internal names) **must happen NOW while still private**. Once Phase 8 executes, mirrored copies of the history exist across the internet and rewriting is no longer effective. Decide before starting Phase 1.

---

## Locked decisions (2026-05-05)

- [x] **License:** Apache 2.0
- [x] **Homebrew tap repo:** `mopanc/homebrew-tap`
- [x] **Initial public version:** `v1.0.0-beta.1`
- [x] **History rewrite:** YES, Option A (scrub email + internal product names) — to execute before Phase 1 commit, while repo is still private
- [x] **Current-files scrub:** completed 2026-05-05 (sed-based, tests pass) — this commit is the baseline for filter-repo
- [ ] **GPG key setup:** pending (required for signed-commits rule in Phase 3)

These decisions are locked. Subsequent phases assume them.
