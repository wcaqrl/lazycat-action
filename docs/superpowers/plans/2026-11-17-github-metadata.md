# Shared GitHub Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix AFFiNE/RSSHub GitHub API rate limits using a reusable authenticated reader.
**Architecture:** Go owns authentication, bounded HTTP reads and a scoped loopback
gateway; trusted adapter helpers receive only its endpoint/session. Both adapters
vendor the same Python client and remove unnecessary API tag-SHA lookups.
**Tech Stack:** Go standard library, Python standard library, existing GitHub Actions.
**Spec:** docs/superpowers/specs/2026-11-17-github-metadata.md

## Global Constraints

- Reuse existing requested clean checkouts; changes limited to the three projects.
- No extra PAT, token forwarding to scripts/containers, or changes to store/review state.
- Existing configurations require no changes; no additional API calls unless requested.
- Fixed candidate validation precedes stable v1 promotion; no manual app-store submission.

## Review Focus

- Authorization must never reach redirects, foreign repositories or adapter processes.
- Changelog, preparation and packaging must get a live endpoint, closed on every exit.
- Quota-reset waits must be bounded; permission403/401/404 must not become blind retries.
- Exact Git tag resolution must handle annotated tags and reject moved/inconsistent refs.
- Candidate workflows must run candidate binaries and must not update floating v1.

### Task 1: Shared reader, gateway and stage integration

**Files:** internal/githubapi/*, internal/{action,source,prepare,build}/*,
clients/python/github_api.py and their focused regression tests.

**Interfaces:** Client.Get(ctx,path) returns a bounded response; Gateway exposes
Endpoint{URL,Session}; stage requests carry that endpoint, never the upstream token.

- [x] RED tests for scoped reads, authentication, redirects, throttling, lifecycle
  and all three script stages; implement and verify GREEN.
- [x] Reuse the reader for built-in GitHub Release changelogs; run the full Go suite,
  race checks and Python-client contract tests.

### Task 2: Adapter adoption and compatibility

**Files:** AFFiNE/RSSHub scripts, tests, workflow validation and documentation.

**Interfaces:** github_json(url) uses the managed endpoint; Git resolves tag SHAs.
Selected Release metadata remains mandatory, and source/digest guards remain intact.

- [x] RED tests for Git tag peeling, first-run baseline, authenticated broker reads
  and lack of credentials in CI; implement and verify all existing adapter tests.
- [x] Verify real read-only checks and unaffected adapters' config/modes.

### Task 3: Version-coherent release and rollout

**Files:** action.yml, reusable/release workflows, metadata tests and docs.

- [x] Test exact internal pins and prerelease promotion exclusion; implement.
- [x] Fresh-context review, resolve material findings and rerun checks.
- [x] Commit/push and release v1.10.0-rc.1; run both adapter read-only checks against it.
- [ ] Release stable v1.10.0 after validation, promote v1 and restore adapter @v1.
