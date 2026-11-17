# Shared GitHub metadata access

Fix authenticated upstream metadata reads for AFFiNE and RSSHub while preserving
all existing adapters, source selection, publication/state and credential isolation.
No additional user Secret: reuse the workflow's automatic GITHUB_TOKEN.

The executor owns a bounded GitHub REST reader and exposes a per-invocation,
authenticated loopback GET-only gateway restricted to configured upstream GitHub
repositories and release/tag metadata paths. Pass only the local URL/session to
changelog, preparation and packaging scripts, never GitHub/store/registry tokens.
No persistent service, daemon changes, credential files or Docker forwarding.
Requests must remain fresh for mutable tag validation. Bound retries/waits for
429/real rate-limit403, transient server/network failures; do not retry permission
errors, invalid authentication or missing releases. Reject redirects and foreign
repositories/hosts. The existing built-in Release reader uses the same client.

Provide one canonical small Python client, vendored by the two adapters. In CI,
require managed access or an explicitly supplied workflow token; local read-only
use may remain anonymous. Resolve baseline/tag SHAs through Git (including peeled
annotated tags), keep precise commit-range changelogs, and validate published
stable Releases and image/source identity as before.

Release v1.10.0 through a fixed prerelease first. Workflow, composite Action,
publisher checkout and binary must pin the same version. Prereleases must not move
v1. Regress existing adapters and validate both adapters' read-only CI before the
stable release promotes v1. Do not manually submit app-store reviews in this task.
