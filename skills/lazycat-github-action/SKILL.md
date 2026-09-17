---
name: lazycat-github-action
description: Use when setting up, generating, reviewing, or debugging LazyCat GitHub Actions for LPK repositories, including Docker image updates, release/store publishing, historical LPK migration or cleanup, versioned Release assets, Go Template Manifest preservation, static or Exec builds, configurable amd64/arm64 targets, and cross-architecture runners.
---

# LazyCat GitHub Action

Use it to automatically inspect the repository, then create or update `wcaqrl/lazycat-action@v1` configuration and workflows from the application's real package, build, and Manifest files. Keep image targets, build tools, publication policy, and `project.target_arch` explicit.

## Choose the supported GitHub interface

Both repository entry points are supported and use the floating `v1` release tag:

| Entry point | Reference | Responsibility |
|---|---|---|
| Composite Action | `wcaqrl/lazycat-action@v1` | Runs one `check`, `build`, or `publish` operation inside a caller-owned job. The caller owns checkout, permissions, toolchains, Release handling, and any other GitHub mutation. |
| Reusable Workflow | `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1` | Owns the complete automation path: toolchains, pull requests, Artifacts, tags, Releases, versioned assets, store reconciliation, and private/official publication. |

Default to the reusable workflow for generated project automation. Use the composite Action only when the existing workflow already provides the surrounding lifecycle. Do not describe one entry point as replacing or disabling the other.

## Primary outcome: working GitHub workflows

Unless the user asks for review-only guidance, finish with repository changes rather than prose alone:

1. Inspect the project and existing automation.
2. Create or update `.github/lazycat-action.yml` from [assets/lazycat-action.yml](assets/lazycat-action.yml).
3. Create or update the appropriate `.github/workflows/*.yml` from [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml).
4. Preserve unrelated workflow jobs and repository conventions.
5. Run `actionlint` plus the project's relevant build/test commands.
6. Report the files created, unresolved placeholders, required Secrets, and verification results.

Do not stop after printing sample YAML when repository editing is authorized. Write the files, validate them, and leave the repository in a reviewable state.

Choose the workflow mode before writing either file:

| Intended automation | `update.strategy` | Version source | Workflow trigger |
|---|---|---|---|
| Scheduled image review PR | `pull` | `image` | `schedule` / `workflow_dispatch` |
| Tag-built GitHub Release Asset | `publish` | `git` | `push.tags` |
| Release-triggered store publication | `publish` | `git` or configured `image` | `release.published` |
| Direct image update, tag, and Release | `publish` | `image` | `schedule` / `workflow_dispatch` |

Never use `pull` for a workflow whose required outcome includes a Git tag, GitHub Release, Release Asset, or store submission.

## Inspect before generating

Read these files when present:

1. `package.yml`: package ID, current version, name, description, locales, icon.
2. `lzc-build.yml`: Manifest path, content directory, buildscript, local image construction.
3. The configured Manifest: application image, services, routes, Exec launch commands.
4. Project toolchain files: `go.mod`, `Cargo.toml`, `rust-toolchain.toml`, `package.json`, lockfile, Dockerfile.
5. Existing `.github/lazycat-action.yml` and workflows.
6. `git status --short`, tracked historical LPKs, `.gitignore`, and pre-existing or untracked `.github/` files.

Do not infer a “main service” from route order, service order, or the first image. Ask for or identify the exact image that drives the application version and the exact service/application field each image updates.

## Historical LPK migration

Before generating Release automation, inventory tracked LPKs with `git ls-files '*.lpk'`. Report the file count and total bytes; do not confuse untracked files with Git history. Keep the configured build output outside packaged content.

## 🔴 CHECKPOINT — before deleting tracked LPKs

Run `git ls-files '*.lpk'`, show the tracked paths, count, and total bytes, recommend cleanup, and **STOP for an explicit yes/no answer immediately before deletion**. General authorization such as “handle it directly” is not deletion approval.

- If the user declines, preserve every tracked LPK and report that the migration remains incomplete.
- If the user approves, remove only the inventoried tracked LPKs, verify the post-delete tracked count, and add `*.lpk` plus the generated output directory to `.gitignore`. An ignore rule does not untrack a file by itself.
- Never rewrite Git history or backfill historical GitHub Releases unless the user separately requests that work.

Future version-bearing releases use `versioned-release-asset: true`. The verified build output remains the validation Artifact and Release upload uses the copied `<package-id>-v<version>.lpk`. The private store uses the verified GitHub Release Asset URL and SHA256. The official store uploads the same locally verified LPK bytes and SHA256 without receiving the Release URL.

## Go Template Manifest safety

Before YAML handling, detect standalone Go Template controls: `if`, `else`, `end`, `with`, and `range`, including indentation and trim markers. You must never execute or evaluate repository templates or invent deployment values.

Protect every standalone control line, perform the narrow YAML inspection/edit, and restore each line byte-for-byte in its original order. Leave inline expressions such as `PASSWORD={{.U.password}}` untouched, and fail closed on a reserved-marker collision, invalid protected YAML, lost or duplicated markers, missing or duplicate image targets, or any unexpected control-line/diff change. Verify the ordered control lines before and after, then run the real LazyCat build or validation command. An ordinary YAML parse/serialize round trip over the raw templated Manifest is unsafe.

## 🔴 CHECKPOINT — before writing project files

Confirm all repository-specific decisions that affect generated files:

- exact package, build, and Manifest paths;
- version source and every managed image target;
- `pull` versus `publish` strategy and enabled stores;
- required build toolchains and application target (`project.target_arch`, default `amd64`, optional `arm64`);
- Secret names and transport paths, without reading or reproducing Secret values.

Also compare `project.output` with `lzc-build.yml` `contentdir`. Keep the final LPK outside the packaged content tree so a build cannot include its own output.

If any required decision cannot be proven from inspected files or the user's request, **STOP before editing** and ask for that missing fact. If the user explicitly requests a template, use conspicuous placeholders and list every unresolved value; never describe that template as deployment-ready.

## Choose the project path

| Project shape | Version source | Images | Workflow toolchain |
|---|---|---|---|
| Docker service/application image | `image` with explicit image ID | One entry per managed target | `docker` only when buildscript builds locally |
| Static Web | `git` | None | Usually `node` |
| Exec binary | `git` | None unless runtime also uses an image | `go`, `rust`, or `node` |
| Prebuilt content | `git` | None | `none` |

Maintained source-build references:

- Go Exec: [`lazycat-contrib/cat-led`](https://github.com/lazycat-contrib/cat-led) shows a Git version source, explicit Go toolchain, versioned Release Asset, and dual-store publication.
- Rust + Node Exec: [`lazycat-contrib/lazycat-neko-webshell`](https://github.com/lazycat-contrib/lazycat-neko-webshell) shows a Vite frontend embedded into a Rust musl binary, pinned GitHub-downloaded `protoc`, native Runner dependencies, PR LPK validation, and Tag-only dual-store publication.

Use these as contract references, not blind templates. Re-inspect package paths, buildscript dependencies, target triple, store name, locales, and Secret needs in the target repository.

Use `update.strategy: pull` for scheduled review PRs. Use `publish` only when the workflow should commit/tag/release and optionally publish stores.

## Configure Docker images

For every managed image, set:

- stable `id`;
- `target: service` plus exact `service`, or `target: application` with no service;
- upstream `source` for new configurations; an existing mirror migration may recover it only from a preserved Manifest `upstream` comment or a recognized mirror prefix;
- channel and filters;
- delivery mode.

Delivery policy:

| Mode | Result | Local Docker | Official store |
|---|---|---:|---:|
| `lazycat` | Remote copy to `registry.lazycat.cloud` | Not required | Allowed |
| `direct` | Manifest uses upstream reference | Not required | Forbidden |
| `mirror` | Manifest uses a default, configured, or historical accelerator template | Not required | Forbidden |

Set `require_digest_match: true` for mirrors when the accelerator must contain exactly the source image for `project.target_arch`.

Mirror verification is read-only and must not be described as image copying; diagnose `MIRROR_VERIFICATION_FAILED` separately from LazyCat delivery's `IMAGE_COPY_FAILED`.

For new mirror configurations, keep `source` explicit and omit `image_template` when the built-in defaults are intended: Docker Hub maps to `docker.1ms.run` and GHCR maps to `ghcr.1ms.run`. Preserve an existing hand-written `image_template`; it remains effective when no runtime override is configured. Do not bulk-rewrite historical `.github/lazycat-action.yml` files merely to adopt the defaults.

Inspect existing Manifest image values and `upstream` comments before migrating mirror configuration. A missing source may be recovered from `upstream`, from `docker.1ms.run/...` as `docker.io/...`, from `ghcr.1ms.run/...` as `ghcr.io/...`, or from a prefix declared in `LAZYCAT_REGISTRY_MIRRORS`. If none is unambiguous, STOP instead of guessing. The Action performs this resolution before Registry access and does not write the recovered source back to Action configuration.

Use runtime overrides only when the project or organization needs a different accelerator. The reusable workflow reads GitHub Variables named `LAZYCAT_DOCKER_MIRROR`, `LAZYCAT_GHCR_MIRROR`, and `LAZYCAT_REGISTRY_MIRRORS`; missing Variables are empty and trigger fallback. Reusable inputs `docker-mirror`, `ghcr-mirror`, and `registry-mirrors` override those Variables. Composite Action metadata must never reference `vars`; it exposes the same three inputs and then falls back to same-named job/step environment variables. Composite callers map GitHub Variables through their own `with` block, where they become Action inputs. The generic value is comma-separated `registry=mirror-prefix`, such as `quay.io=mirror.example/quay`. Prefixes omit `https://`. Precedence is reusable/composite input (including a caller-mapped GitHub Variable), composite environment, existing `image_template`, then the Docker Hub/GHCR built-in default. Other registries require an explicit generic mapping.

A version-discovery rule must match a release family that can produce future candidates. Inspect representative upstream tags before choosing the channel, filter, mapping, and sort. Do not generate an exact immutable SemVer filter such as `^2\.2\.0$`; it selects only the already-known version, so scheduled checks can never discover `2.2.1`, `2.3.0`, or another later release. Use these shapes instead:

| Upstream intent | Configuration shape |
|---|---|
| Latest stable across all major versions | `channel: stable`; omit `tag_regex` when ordinary tags already parse as SemVer, or use `^v?\d+\.\d+\.\d+$` to exclude non-release aliases |
| Latest stable within major version 2 | `channel: stable`, `sort: semver`, `tag_regex: '^v?2\.\d+\.\d+$'` |
| Latest supported prerelease line | `channel: beta` plus a family regex that accepts future alpha/beta/rc/preview tags |
| Date-formatted immutable tags such as `20260626` | `channel: date`, `sort: semver`, a date-family `tag_regex`, and named `version_regex`/`version_template` mapping to stable SemVer |
| Custom tag prefix such as `release-2.3.4` | `tag_regex` selects the whole `release-...` family; `version_regex` extracts `(?P<version>...)`; `version_template` normalizes it |
| Mixed immutable families such as two-part weekly plus three-part LTS/JDK tags | `channel: custom`, `sort: semver`, a `tag_regex` for only the intended family, and `version_regex`/`version_template` to produce strict SemVer |
| Mutable channel name such as `latest` | exact `tag_regex: '^latest$'` plus digest comparison and `bump: patch` |

`tag_regex` filters candidate tags; `version_regex` and `version_template` map a matched tag to package SemVer; `sort` decides which mapped candidate wins. For fixed-width date tags, use `channel: date` with a mapping such as `tag_regex: '^[0-9]{8}$'`, `version_regex: '^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$'`, and `version_template: '{version}.{month}.{day}'`; template expansion canonicalizes `20260626` to `2026.6.26` and `20260101` to `2026.1.1`. Existing explicit zero-stripping groups remain compatible. Do not collapse these separate jobs into a regex for the current version. Exact tag filters are reserved for mutable channel names whose digest changes behind the same tag, or for an intentional one-version pin that the user explicitly wants. An immutable one-version pin is not an automatic update strategy; report that no future tag discovery will occur.

When a tag needs normalization, reference named `version_regex` groups directly in `version_template`, for example `(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)` with `{version}.{build}.0`. Keep the required `version` group. Unknown placeholders and non-SemVer results fail closed; do not add repository-specific rewriting when this mapping is sufficient.

Read the **Mixed immutable tag families** example in [references/configuration.md](references/configuration.md) when one repository combines non-SemVer weekly tags, strict-SemVer LTS tags, suffixed runtime variants, and mutable aliases. [`lazycat-contrib/jenkins-lzcapp`](https://github.com/lazycat-contrib/jenkins-lzcapp) is the maintained real-repository reference for this shape.

For large image repositories, `images[].max_tags` limits raw tag discovery (default `10000`, maximum `50000`) and `images[].max_matching_tags` limits the tags remaining after `tag_regex`/`exclude_regex` (default `10000`, maximum `50000`, never above `max_tags`). Keep defaults unless the upstream has a measured tag count above the default; do not raise an organization-wide limit. For SemVer sorting, rank filtered tag names before manifest inspection and stop after the first usable configured target; continue past a higher tag only when that tag lacks the target platform. `sort: updated` is an explicit Docker Hub-only mode: rank by tag `last_updated`, then mapped SemVer, then tag name, and inspect manifests in that order. Never substitute OCI `config.created` when Docker Hub update metadata is unavailable. Keep full manifest inspection for `created` sorting because the target image creation time is required for ranking.

When the upstream exposes only a mutable tag such as `latest`, use `update.version_source.bump: patch` with `channel: custom`, `sort: created`, and an exact `tag_regex`. Compare the selected target-platform digest with the currently delivered digest. Equal digests must be a no-op; changed digests increment only the current stable SemVer patch after the prior digest is proven. Reject prerelease/build versions, `allow_downgrade: true`, version mapping, and mutable mirrors without `require_digest_match: true`. Direct/mirror mutable references must remain digest-pinned; official publication continues to require LazyCat delivery.

For mutable LazyCat delivery, persist the selected source digest in the Manifest upstream comment, copy the selected source through a digest-pinned reference, and compare that baseline on later runs; never anonymously inspect the private LazyCat Registry. A legacy LazyCat runtime without a baseline may perform one authenticated digest-pinned copy and compare the content-addressed returned reference. An external runtime may perform one migration copy without bumping. A dry-run without a legacy private baseline must fail closed until a trusted non-dry migration establishes it.

Keep `update.allow_downgrade: false` or omit it for the safe default. Compare the mapped version-source image SemVer with the current package version before delivery. Equal versions may refresh an image reference or digest. Set `allow_downgrade: true` only after the user explicitly confirms an intentional rollback.

## 🔴 CHECKPOINT — before enabling version downgrades

Show the current package version, selected lower version, affected version-source image, and why the rollback is required. **STOP for an explicit yes/no answer immediately before writing `allow_downgrade: true`.** General authorization to fix CI or update dependencies is not rollback approval. If the user declines, keep the default guard and repair the channel, sorting, or version mapping instead.

Read [references/configuration.md](references/configuration.md) for channel rules, the configuration schema, authentication, and store constraints.

## Configure builds and workflows

The Action may execute on Linux amd64 or Linux arm64. The LazyCat target OS is Linux; `project.target_arch` defaults to `amd64` and may be `arm64`:

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=<project.target_arch>
LAZYCAT_TARGET_PLATFORM=linux/<project.target_arch>
```

Buildscripts must honor those values. For the default target, Go uses `GOOS=linux GOARCH=amd64`, Rust uses `x86_64-unknown-linux-gnu`, TypeScript Exec packages a Linux x64 runtime, and Docker Buildx uses `--platform linux/amd64`. For `target_arch: arm64`, use the matching arm64/aarch64 toolchain and `linux/arm64` container platform. A Runner whose CPU differs from the target may require cross-compilation and QEMU.

The reusable workflow's `toolchains` input must match `build.toolchains` in Action configuration. Do not rely on an implicit moving toolchain version.

GitHub-hosted workflows must use Node.js 24-compatible official JavaScript Actions. When a caller-owned job uses the composite Action, generate `actions/checkout@v7` and `actions/setup-node@v7`; replace the legacy v4 majors during workflow review. The reusable workflow owns these setup steps internally, but generated direct-composite examples still need the same majors.

Keep the maintained reusable workflow on the current major lines: `actions/checkout@v7`, `actions/setup-go@v7`, `actions/setup-node@v7`, `actions/upload-artifact@v7`, `actions/github-script@v9`, `docker/login-action@v4`, `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`, `peter-evans/create-pull-request@v8`, and `softprops/action-gh-release@v3`. The Action release workflow uses `anchore/sbom-action/download-syft@v0` and `actions/attest-build-provenance@v4`. When updating these majors, review upstream breaking changes and synchronize the workflow contract tests.

Set `build.run_buildscript: false` explicitly when `lzc-build.yml` has no `buildscript`; the Action default is `true`. For every publishing workflow, explicitly map each credential required by the enabled stores under the reusable job's `secrets:` block. Do not use only `secrets: inherit`: explicit mappings make missing repository authorization and Environment/Repository/Organization overrides reviewable. A public-image scheduled PR workflow with stores disabled should not receive unrelated repository Secrets.

Use `LZC_API_TOKEN` as the default credential for new or regenerated callers. Do not map `LAZYCAT_TOKEN` by default. Include `LAZYCAT_TOKEN` only when an existing caller still depends on an lzc-cli session token and no PAT migration has been confirmed. Do not map both credentials as a generic fallback; once the PAT path is confirmed, remove the legacy mapping from generated examples and workflow updates. Keep the reusable workflow's optional `LAZYCAT_TOKEN` input for backward compatibility.

For versioned Release assets, set the reusable workflow input exactly:

```yaml
with:
  versioned-release-asset: true
```

The final Release Asset name is `<package-id>-v<version>.lpk`. Verify package ID, package version, Release tag, asset filename, and SHA256. Verify the private-store download URL identifies that asset. Official publication reuses the same locally verified LPK bytes and SHA256, not the Release URL.

Copy [assets/lazycat-action.yml](assets/lazycat-action.yml) and [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml) as starting points, then replace only values confirmed from the inspected project. Read [references/workflows.md](references/workflows.md) for tag/release, permissions, secrets, PR, and store examples.

## Configure stores

Official publishing requires:

- `update.strategy: publish`;
- `stores.official.enabled: true`;
- optional `stores.official.continue_if_newer_version: false` to pause every automatic direct-publication run that finds a pending review; omitted or true uses the newer-candidate comparison described below;
- optional `stores.official.skip_if_version_exists: true` to query the anonymous official catalog and skip an equal version (`version-already-online`) or, with `allow_downgrade: false`, a newer online SemVer (`online-version-newer`);
- optional retry policy, defaulting to `retry.enabled: false`; when enabled, `max_attempts` includes the first attempt and `initial_delay`/`max_delay` use Go duration syntax;
- only `lazycat` image delivery;
- official lint compliance, including locales and icon size at most 200 KB;
- preferred PAT in `LZC_API_TOKEN`, with an optional `LZC_API_HOST` PAT API override;
- protocol-preserving legacy lzc-cli session authentication through `LAZYCAT_TOKEN` only for a confirmed legacy-only caller that has not migrated to a PAT.

Automatic first information submission is optional and is enabled only when `stores.official.create_if_missing: true` plus at least one of `brief`, `description`, `keywords`, `support_pc: true`, `support_mobile: true`, `screenshot_pc_files`, or `screenshot_mobile_files` is configured. `language`, `name`, `source`, and `source_author` alone retain the legacy create-only behavior. Both support flags default to false. Use the authenticated developer API for exact first-submission state; never infer it from the anonymous public catalog. The supported states are missing application, incomplete information, approved information, and pending review. Approved information is preserved without re-uploading screenshots. A pending review must fail closed before LPK or screenshot upload.

Screenshot fields accept only committed project-relative PNG/JPEG files. An agent using `agent-browser` must capture the PC/mobile views into the repository, commit and push the files to a ref the workflow will checkout, and then run publication. Choose project-confirmed viewport, DPR, authentication state, and test data rather than inventing them. Do not configure remote screenshot URLs. PC support requires 2-8 screenshots; mobile support requires 3-8. Each source must be at most 15 MiB and 320-3840 pixels in both dimensions; the Action center-crops it to 16:9 and uploads PNG. Paths outside `project.root`, symbolic links, and non-regular files are invalid. Read [references/configuration.md](references/configuration.md) for the complete optional YAML contract.

Use this complete retry shape when the project explicitly opts in:

```yaml
retry:
  enabled: false
  max_attempts: 3
  initial_delay: 2s
  max_delay: 30s
```

Upload/check failures may retry status-less connection/TLS/reset failures, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; do not replay an ambiguous review network/5xx outcome because the non-idempotent request may already have been accepted. Never retry cancellation, deadline expiry, authentication, permissions, NotFound, integrity failures, HTTP 400, or another 4xx. A retry before review rechecks application existence and reopens the LPK; credentials resolve once. A valid `Retry-After` may extend the jittered wait up to `max_delay`.

Keep lint severity store-scoped. A compatibility warning such as unknown `container_name` stays visible without blocking the shared build or private publication. Only official warnings block the official precheck. If official publishing fails in a dual-store reusable workflow, preserve the private result, emit a warning, and set `failureReason: official-publish-failed`; an official-only workflow remains fatal. With the official store disabled, do not run official lint blocking, precheck, credential resolution, or publication.

For an HTTP rejection, keep status and stage (`store.official.upload` or `store.official.review`). Never expose the raw response body. A recognized JSON `message`, `msg`, string `error`, or nested `error.message` may be displayed only after one-line normalization, a 512-byte bound, and credential-marker suppression.

Private publishing requires:

- `update.strategy: publish`;
- `stores.private.enabled: true`;
- optional `stores.private.skip_if_version_exists: true` to query the exact package before reading write credentials;
- `APPSTORE_URL` and `APPSTORE_TOKEN`;
- optional `APP_ID`;
- optional GitHub Secret `PRIVATE_STORE_GROUP_CODES`, comma-separated, for private groups;
- a real GitHub Release Asset URL and the local SHA256.

When `APP_ID` is absent, preserve a confirmed `stores.private.name`: publishing searches by exact package ID first and then calls authenticated `GET /api/v1/apps/by-name?name=...`. The resolver must return the unique exact-name app for which the Token can upload versions. A 404 permits app creation; ambiguity, authorization failure, an inexact name, or `canUploadVersion: false` must STOP. A name-resolved historical app may have a different package ID; use its numeric ID only for `POST /api/v1/apps/{id}/versions`.

Private stores support `lazycat`, `direct`, `mirror`, static Web, and Exec applications. Never enable the official store merely to get stricter lint for a direct/mirror application; that configuration is intentionally invalid.

Both skip options default to false. When enabled, exact equality returns `published: false`, `skipped: true`, and `skipReason: version-already-online`. If both values are valid SemVer, an online version greater than the candidate also skips with `skipReason: online-version-newer` while `update.allow_downgrade: false`; explicit rollback authorization through `allow_downgrade: true` continues publishing. A non-SemVer value disables ordering and keeps exact-equality-only behavior. Apply this independently per store before resolving write credentials. Anonymous lookups make up to three attempts with exponential backoff for status-less connection failures, HTTP 429, and HTTP 5xx. Not-found continues publishing; other errors or retry exhaustion stop. `dry-run` never queries stores. Group codes are secrets: do not put them in Action YAML, ordinary inputs, generated outputs, summaries, or examples with real values.

For scheduled `publish` workflows, treat the exact versioned GitHub Release Asset as the delivery source of truth. If `<package-id>-v<version>.lpk` already exists for the current tag and a store lacks that version, download that exact asset beneath `project.root`, require a GitHub `sha256:` digest, recompute local SHA256, and publish the verified bytes. If a store already has the version, skip it independently. If the Release, exact asset name, or digest is missing, do not select another asset or infer another version; continue only through the normal build/Release path.

Before an automatic scheduled or manually dispatched `publish` run with the official store enabled, query the authenticated developer API for the exact waiting-review version. For an image version source, `continue_if_newer_version` defaults to true: prioritize and select the version-source image in the real check, compare before mirror/LazyCat delivery or file edits, pause when the review version is equal to or newer than the candidate, and continue with that same selection when the candidate is newer. For mutable `bump: patch`, plan the candidate application version from the persisted source digest before comparison: unchanged keeps the current version and changed selects the next patch. Immediately before official upload, repeat the authenticated comparison against the final verified LPK version; this closes the build/Release TOCTOU window. Setting the option to false pauses every pending review before image selection and again at final publication. Git version sources pause conservatively for any pending review. The initial pause must not edit project files, commit, push, tag, create or reconcile a Release, or publish either store; a final recheck pause must prevent official upload/review submission. Report `official-review-pending` and `official-review-version`. Invalid/non-SemVer comparisons, missing trusted mutable digest baselines, authentication errors, and remote errors fail closed. Do not apply this gate to explicit operations, dry runs, `pull` strategy, or Tag/Release publication.

Organization Secrets must authorize the target repository. For the same Secret name, GitHub applies the most specific scope: Environment overrides Repository, and Repository overrides Organization. Use organization Secrets as shared defaults and repository Secrets only for intentional overrides; state the effective scope when reviewing a workflow with duplicate names.

## Verify the generated result

Before finishing:

1. Confirm every image target exists exactly once in the Manifest.
2. Confirm `update.version_source.image` names a configured image when type is `image`.
3. Confirm official store plus direct/mirror is absent.
4. Confirm source-build scripts output the configured `project.target_arch`.
5. Confirm workflow toolchains and configured toolchains match.
6. Confirm permissions include `contents: write` and `pull-requests: write` for the reusable workflow.
7. Confirm secrets are referenced, never embedded.
8. Confirm `PRIVATE_STORE_GROUP_CODES` is a GitHub Secret when private groups are required.
9. Confirm every enabled store credential is explicitly assigned in the caller workflow and the selected Organization Secret authorizes the repository.
10. Confirm standalone Go Template control lines are byte-identical and were never evaluated.
11. Confirm the private store uses the verified versioned Release URL/SHA256 and official publication uploads the same verified bytes/SHA256 without that URL.
12. Confirm unchanged `publish` automation recovery-builds a missing Release/asset, while an existing exact Release Asset reconciles either missing store version without rebuilding or republishing the store that is already current.
13. Run `actionlint` and the project's build/test commands.
14. Confirm caller-owned checkout and Node setup use `actions/checkout@v7` and `actions/setup-node@v7`, with no legacy v4 majors in active workflows.

## Common failures

| Symptom | First repair | Still failing |
|---|---|---|
| Wrong service image updated | Correct the explicit `service`; never infer it | STOP if the target is missing or duplicated |
| Scheduled checks never discover a newer immutable release | Replace the exact current-version `tag_regex` with a release-family filter and the intended `semver`/`updated` sort | If the user truly wants one immutable tag, disable or describe automatic version discovery instead of pretending the pin will update |
| Date tag mapping fails on values such as `20260626` or `20260101` | Use `channel: date` with named year/month/day captures and `{version}.{month}.{day}`; mapped SemVer core fields remove padding before strict validation | STOP if the regex does not describe the complete date family or the mapped value is still not valid SemVer |
| A custom tag matches but maps to the wrong package version | Keep family selection in `tag_regex`; move extraction into a named-group `version_regex` and normalization into `version_template` | STOP if representative upstream tags cannot be mapped to valid SemVer without guessing |
| Default stable selection picks an older three-part LTS tag, or switching a mixed immutable repository to `sort: updated` returns `CONFIG_INVALID` | Filter only the intended immutable family, map it to strict SemVer, and use `sort: semver` | Keep `allow_downgrade: false`; do not use `updated` or rollback authorization to compensate for the wrong release family |
| Templated YAML does not parse | Protect supported standalone controls | STOP on invalid protected YAML or marker collision |
| Control-line order/hash changed | Restore exact original control lines | STOP without writing if any marker is lost or duplicated |
| Tracked-LPK inventory fails | Re-run `git ls-files '*.lpk'` and byte accounting | STOP; do not delete from an incomplete inventory |
| Post-delete count mismatches | Compare against the approved inventory | STOP and report the remaining tracked files |
| Versioned asset identity differs | Recompute name, URL, and SHA from verified LPK | STOP before either store submission |
| Official publish rejects registry | Use `delivery.mode: lazycat` for every managed runtime image | STOP if any managed runtime remains direct/mirror |
| `store.official.upload` fails | Confirm the exact verified local LPK path, package/version/SHA256, official lint, and multipart file upload | STOP; never replace the file with a Release URL |
| `store.official.review` fails | Treat the LPK upload as completed; inspect the safe HTTP status and bounded JSON `message` plus application/version review eligibility | A 400 is not retryable; in dual-store automation preserve the private result and warn, while official-only remains fatal |
| Runner produced the wrong target architecture | Honor `LAZYCAT_TARGET_ARCH` and `LAZYCAT_TARGET_PLATFORM` | STOP until the build proves the configured `project.target_arch` output |
| Private publish has no URL | Resolve the verified GitHub Release Asset | STOP before `publish-private` |
| Equal store version is submitted again | Enable `skip_if_version_exists` and inspect `onlineVersion` | STOP on lookup errors other than not-found |
| Official review returns 400 for an older LPK | Compare candidate and `onlineVersion`; `7.8.138 > 7.7.406` must skip with `online-version-newer` | Do not retry or set `allow_downgrade: true` without explicit rollback approval |
| Release exists but a store is behind | Recover only `<package-id>-v<version>.lpk` and verify both GitHub/local SHA256 | STOP if tag, exact asset, or digest is missing |
| Private application is invisible | Add `PRIVATE_STORE_GROUP_CODES` as a GitHub Secret | STOP rather than commit or print group codes |
| Existing private app conflicts after package lookup | Confirm `stores.private.name` exactly matches the store application and that the store exposes `/api/v1/apps/by-name` | STOP on 401/403/409, an inexact response name, or missing upload permission |
| Rust ConnectRPC build reports `failed to spawn protoc ('protoc')` | Install or download a pinned `protoc` compatible with the repository's declared proto syntax/edition; verify its SHA256 and print `protoc --version` | If system `protoc` rejects `edition = "2023"`, keep the source semantics and use a newer pinned compiler; do not rewrite the proto to `proto3` unless the generated API migration is intentionally implemented and tested |
| PR source build passes but Tag build lacks native tools | Put required tool bootstrap in the authorized buildscript or another path shared by the reusable Tag job | STOP if the dependency exists only in a PR-specific setup step |
| `VERSION_DOWNGRADE_BLOCKED` | Correct an accidental `sort: created` rule or stale tag mapping | Set `allow_downgrade: true` only after explicit rollback confirmation |

## Do Not

- Do not delete tracked LPKs based only on broad repository-edit authorization.
- Do not claim `.gitignore` removed files that are already tracked.
- Do not rewrite history or backfill old Releases as part of routine cleanup.
- Do not execute, render, or evaluate a Go Template Manifest with invented values.
- Do not round-trip a raw templated Manifest through an ordinary YAML serializer.
- Do not publish an unversioned final asset when versioned naming was requested.
- Do not rebuild, rename, or substitute a different Release asset during store reconciliation.
- Do not overwrite pre-existing or untracked `.github/` work.
- Do not expose Secret values in configuration, logs, outputs, summaries, or examples.
- Do not print an official response body; only an explicitly sanitized JSON `message` may cross the error boundary.
- Do not enable `allow_downgrade` merely to make a failing scheduled run green.
- Do not generate `tag_regex` from the current package or image version, such as `^2\.2\.0$`, for an automatic update workflow.
- Do not use `version_regex` as the candidate filter or `tag_regex` as a substitute for version mapping; keep filtering, mapping, and sorting separate.
