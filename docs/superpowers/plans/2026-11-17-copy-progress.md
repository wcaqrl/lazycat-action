# Copy Progress and Docker Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans task-by-task.

**Goal:** Fix AFFiNE's Docker empty-field false positive and make image-copy waits observable and bounded.
**Architecture:** Normalize only two optional scalar settings in the adapter; centralize copy monitoring in delivery. Preserve the existing SDK copier and token boundary.
**Tech Stack:** Python stdlib, Go stdlib, existing toolkit and GitHub Actions.
**Spec:** docs/superpowers/specs/2026-11-17-copy-progress-design.md

## Global Constraints

- Work in the two requested clean checkouts; no other adapter changes.
- User/WorkingDir only: missing/null normalize to empty; actual configuration changes fail.
- Defaults 1h total/min(20m,total) idle; AFFiNE 30m/10m. Optional settings do not alter source/artifact identity.
- No copy retries; close timers/goroutines and cancel HTTP on timeout.
- Real container acceptance runs on Runner, not this resource-constrained workstation.

## Review Focus

- Never accept a changed user, workdir, platform, digest or filesystem.
- Unchanged/reordered/finalizing polls must not evade idle timeout.
- Active transfers still obey total timeout; parent cancellation and success release monitors.
- Callbacks/logging must be safe under concurrency and errors must retain safe actionable details.
- Preserve legacy configurations, source fingerprints and the exact released workflow/binary pins.

### Task 1: AFFiNE scalar compatibility

**Files:** affine-adapter/scripts/registry.py, tests/test_registry.py.
**Interface:** verify_local(image,local) retains its return/exception contract.
- [x] RED: table tests accept missing/null/empty User/WorkingDir, reject real changes/types and identity/layer mismatches.
- [x] GREEN: normalize only these fields; run all Python/Node tests and real API 1.48/1.54 image checks.
- [x] Commit the tested adapter change.

### Task 2: Copy limits and centralized progress

**Files:** internal/config/{types,load}.go, internal/delivery/{delivery,copy}.go, corresponding tests, internal/action/action.go and docs.
**Interface:** optional Delivery.CopyTimeout/CopyIdleTimeout; Resolver.Logger; copy helper uses the existing Copier.CopyImage context/callback.
- [x] RED: config/default validation and real httptest SDK copies cover progress, finalization, total/idle timeout, repeated polls, caller callbacks/cancellation and cleanup.
- [x] GREEN: add central monitor, wire resolver logger, document and configure AFFiNE limits.
- [x] Run full Go race/vet, bootstrap, workflow and shell checks, existing nine config loads and adapter regressions; commit.

### Task 3: Review and rollout

- [x] Fresh-context review; fix material findings RED→GREEN and rerun affected suites.
- [x] Release coherent v1.10.1, verify CI/release then floating v1; push fixed adapter.
- [x] Start a new hosted AFFiNE runtime check, then real run; verify LPK, state, review and independent release outcomes.

## Verification record

- Full Go race/vet, Python client5, bootstrap, Node publisher5, actionlint/ShellCheck passed; nine existing adapter configs load.
- AFFiNE27 Python/3 Node tests and all three real pinned images at API1.48/1.54 pass.
- Hosted runtime verification: https://github.com/wcaqrl/affine-adapter/actions/runs/44009542621
- Core CI: https://github.com/wcaqrl/lazycat-action/actions/runs/44010578341
- v1.10.1 release: https://github.com/wcaqrl/lazycat-action/actions/runs/44010649733
- v1/v1.10.1 resolve to 9117661aad84bec5827efecc448aa85bdf2cfee4.
- Full official-image acceptance, packaging, store submission and independent GitHub Release succeeded: https://github.com/wcaqrl/affine-adapter/actions/runs/44010886589
- Store review23359 (waiting), package cloud.lazycat.app.affine, version0.27.4, LPK49152 bytes, SHA256e9ba3458988b720cefddd49c01900b2f78474ca67040b9f01edcc94eec59e80a.
- Independent review issues resolved RED→GREEN: terminal cancellation, duplicate/premature legacy logging, reference parsing/normalization, serialized concurrent callbacks.
