# LazyCat GitHub Action

[简体中文](README.zh-CN.md)

This repository is an independently maintained derivative of
[`ca-x/lazycat-github-action`](https://github.com/ca-x/lazycat-github-action).
The original and derivative work are distributed under the MIT License.

`wcaqrl/lazycat-action` checks Docker image versions, updates explicit LazyCat Manifest targets, builds LPK files, creates update pull requests, and attaches validated LPK files to GitHub Releases.

The Action uses [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.6.0`. Its compatibility baseline is `@lazycatcloud/lzc-cli` `2.0.9`.

Current scope:

- Milestone 1: static Web and Exec builds, LPK validation, SHA256, amd64 and arm64 Action binaries.
- Milestone 2: stable, beta, date, nightly, and custom OCI checks; LazyCat, direct, and mirror delivery; pull requests; Artifacts; tags; Releases; Release Assets.
- Milestone 3: LazyCat official developer-platform submission, MiaoMiao private-store submission, complete source-build examples, and the repository Agent Skill.

## Choose the interface

Both public entry points are supported and follow the floating `v1` release tag:

| Entry point | Reference | Use it when |
|---|---|---|
| Composite Action | `wcaqrl/lazycat-action@v1` | Your job already owns checkout, permissions, toolchain setup, and GitHub mutations. |
| Reusable Workflow | `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1` | You want the complete LazyCat CI/CD path, including toolchains, pull requests, Artifacts, tags, Releases, assets, and store publication. |

Why is the reusable-workflow reference longer than an Action reference such as `KSXGitHub/github-actions-deploy-aur@v4.2.0`? GitHub uses different syntax for the two entry-point types. A repository-root `action.yml` is a step-level Action and uses the short `<owner>/<repo>@<ref>` form. A reusable workflow is a job-level workflow file, so GitHub requires `<owner>/<repo>/.github/workflows/<file>@<ref>`. The longer path does not mean a different repository or installation method; it selects the complete workflow instead of the single composite step.

Current official checkout and Node setup Actions use the Node.js 24 runtime. Self-hosted GitHub Actions Runners must be `v2.327.1` or newer. Caller-owned composite jobs should use `actions/checkout@v7` and `actions/setup-node@v7`; the reusable workflow also uses the current supported major lines for setup-go, github-script, Docker setup/login, pull-request creation, and build provenance.

Use the reusable workflow for normal CI/CD:

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      config: .github/lazycat-action.yml
    secrets: inherit
```

Use the composite Action directly inside an existing job:

```yaml
- uses: wcaqrl/lazycat-action@v1
  id: lazycat
  with:
    operation: build
    version: ${{ github.ref_name }}
```

Gitea Actions and Forgejo Actions users should call the composite Action directly with a fully qualified URL. The GitHub reusable workflow is not portable as-is. See [Using this Action with Gitea Actions and Forgejo Actions](docs/gitea-forgejo-actions.md).

Callers do not compile this repository. The bootstrap downloads a checksum-verified Action binary for the Runner architecture.

## Progress logs

The Action emits structured `log/slog` progress records without printing Secret values or protected build environment variables. A run identifies its execution mode (`docker-image`, `source-build`, `prebuilt-content`, or `store-publish`) and then reports the applicable stages:

- Docker discovery, candidate count, selected tag/version/digest/platform, delivery start, throttled layer progress, and delivery result.
- LPK buildscript start, package assembly, official lint, and the completed LPK path, size, and SHA256.
- Store target, verified publication artifact, equal-version skip, publication start, and publication result.

Project buildscript stdout and stderr are streamed live so native-tool failures remain visible. The Action reports the process exit code but does not print the buildscript body or protected environment values.

Local LPK validation errors include safe structured diagnostics when available. For example, a templated Manifest parse failure may report `upstream=INVALID_CONFIG`, `op=build.template_manifest`, `path=lzc-manifest.yml`, and a bounded `message="yaml: line 90: ..."`. A path is public only when it exactly matches a package, build, or Manifest file already confirmed during project inspection; runner paths, unknown paths, Unicode log separators, and suspected credential content are suppressed.

## Using the Skill

Ask an agent naturally, for example: “Inspect this LazyCat repository, create the GitHub workflows for versioned Release publishing to both stores, and preserve the Go Template Manifest.” The repository Skill inspects `package.yml`, `lzc-build.yml`, the configured Manifest, toolchain files, `.gitignore`, tracked `*.lpk` files, and existing `.github/` content. It creates or updates `.github/lazycat-action.yml` and the necessary `.github/workflows/*.yml`, then reports every changed file, verification result, unresolved decision, and required GitHub Secret name without reading Secret values.

The Skill pauses before generated project files when paths, image ownership, strategy, stores, or toolchains cannot be proven. For historical LPK migration it runs `git ls-files '*.lpk'`, reports the tracked count and total bytes, and shows a separate visible STOP immediately before deletion. Declining preserves all files. Approval removes only the inventoried files and adds `*.lpk`/output ignore rules; it never rewrites history or backfills old Releases without a separate request.

Publishing workflows explicitly map the Secrets required by each enabled store instead of relying only on `secrets: inherit`. Organization Secrets must authorize every newly added repository; Environment overrides Repository, and Repository overrides Organization for duplicate names.

For version-bearing releases, set `versioned-release-asset: true`. The verified build output remains the validation Artifact and the GitHub Release uses `<package-id>-v<version>.lpk`. The private store receives that verified Release Asset URL and SHA256. The official store uploads the same locally verified LPK bytes and SHA256, but it does not receive the GitHub Release URL.

Go Template Manifests are never evaluated. Standalone `if`, `else`, `end`, `with`, and `range` control lines are protected and restored exactly, including indentation and trim markers; inline expressions remain untouched. The edit fails closed on marker loss/collision, invalid protected YAML, ambiguous targets, or unexpected template changes, and verifies the control lines plus the real build before completion.

## Runner architecture and LazyCat target

The Action host and the LazyCat application target are separate:

| Concern | Supported value |
|---|---|
| Runner OS | Linux |
| Runner CPU | amd64 or arm64 |
| LazyCat target OS | Linux |
| LazyCat target CPU | `project.target_arch`; defaults to amd64, optionally arm64 |
| OCI inspection and copy platform | `linux/amd64` or `linux/arm64`, matching the project target |

An ARM64 self-hosted Runner uses the ARM64 Action binary. Build scripts still receive:

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=<project.target_arch>
LAZYCAT_TARGET_PLATFORM=linux/<project.target_arch>
```

The reusable workflow accepts a Linux Runner label:

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      runner: self-hosted-linux-arm64
      config: .github/lazycat-action.yml
    secrets: inherit
```

The label above is an example. Configure that label on your self-hosted Runner. Changing the Runner does not change the LPK target.

## Concepts

- `package.yml` holds the package ID, version, display metadata, and locales.
- `lzc-manifest.yml` holds the application routes and optional application or service images.
- `lzc-build.yml` points to the Manifest, content, and optional project `buildscript`.
- `.github/lazycat-action.yml` tells this Action which version source and image targets it owns.
- A Workflow Artifact is a CI result retained by GitHub Actions.
- A Release Asset is a public file attached to a GitHub Release.

The Action applies basic LPK lint by default. Set `stores.official.enabled: true` to apply the official LazyCat lint profile. Official mode also requires every configured runtime image to use `delivery.mode: lazycat`.

## Docker image application quick start

Consider an application with a database service named `db` and a visible Web service named `web`:

```yaml
# lzc-manifest.yml
application:
  subdomain: example
  routes:
    - /=http://web:8080/

services:
  db:
    # upstream: postgres:17
    image: registry.lazycat.cloud/acme/postgres:copy-id
  web:
    # upstream: ghcr.io/acme/example-web:v1.0.0
    image: registry.lazycat.cloud/acme/example-web:old
```

The Action never guesses that `web` is the main service. Configure both decisions explicitly:

- `update.version_source.image: web` means the selected `web` image version updates `package.yml.version`.
- `images[].target: service` and `service: web` mean the Manifest editor may change only `services.web.image`.

`db` is already stored in the LazyCat Registry but is not listed under `images`, so this automation leaves it unchanged.

Create `.github/lazycat-action.yml`:

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/example.lpk
  target_arch: amd64

update:
  strategy: pull
  allow_downgrade: false
  version_source:
    type: image
    image: web

build:
  run_buildscript: true

images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example-web
    channel: stable
    delivery:
      mode: lazycat

stores:
  official:
    enabled: true
    create_if_missing: false
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
  private:
    enabled: false
```

`allow_downgrade` defaults to `false`. After the version-source image tag is mapped to SemVer, the Action blocks a version lower than the current `package.yml.version` before image copying or file edits. Equal versions remain eligible for image-reference or digest refresh. Set it to `true` only for an intentional rollback.

Production defaults to `appstore.api.lazycat.cloud`; store the PAT as the `LZC_API_TOKEN` GitHub secret. Existing workflows may continue using their lzc-cli session token under `LAZYCAT_TOKEN`. Set `LZC_API_HOST` only to override the PAT API host.

Then add a scheduled and manual caller workflow:

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

`strategy: pull` is the default. When a newer image exists, the workflow updates only the configured targets, builds and validates the LPK, uploads a Workflow Artifact, and opens or updates `lazycat/update-all`.

With `operation: auto`, `workflow_dispatch` builds a Git-version-source project (using `package.yml.version` when no version input is supplied) and checks an image-version-source project. An explicit manual version always builds. Tag pushes build and schedules check images. Explicit operations otherwise keep their requested behavior, but `check` is rejected for Tag and Release events so an image update cannot be published under an unrelated event tag.

Use `image-id` to process one configured image:

```yaml
with:
  operation: check
  image-id: web
  config: .github/lazycat-action.yml
```

With `strategy: pull`, selecting a non-version-source image creates a reviewable Manifest change while keeping the current package version. Direct publish requires `image-id` to select the configured version-source image, because a GitHub Release needs a new application version.

## Channels

| Channel | Selection rule |
|---|---|
| `stable` | Highest valid non-prerelease SemVer by default; may opt into Docker Hub `updated` sorting |
| `beta` | Highest valid prerelease SemVer by default; may opt into Docker Hub `updated` sorting |
| `date` | Date-like tags mapped to stable SemVer; sort may be `semver` or Docker Hub `updated` |
| `nightly` | Newest regex-matched target-platform OCI image creation time |
| `custom` | Regex filtering with explicit `semver`, `created`, or `updated` sorting |

Stable example:

```yaml
channel: stable
tag_regex: '^v?\d+\.\d+\.\d+$'
exclude_regex: 'windows|arm64'
```

The filter must describe an evolving release family, not the currently observed version. For example, staying on v2 uses `tag_regex: '^v?2\.\d+\.\d+$'`; `tag_regex: '^2\.2\.0$'` pins one immutable tag and will never discover 2.2.1 or 2.3.0. Use an exact filter only for a mutable name such as `latest` with digest-based bumping, or when no automatic version updates are intentionally desired. Keep candidate filtering (`tag_regex`), version mapping (`version_regex`/`version_template`), and ordering (`sort`) separate.

Beta example:

```yaml
channel: beta
tag_regex: '^v?\d+\.\d+\.\d+-(alpha|beta|rc|preview)\.'
```

Date-tag example:

```yaml
channel: date
tag_regex: '^[0-9]{8}$'
version_regex: '^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$'
version_template: '{version}.{month}.{day}'
```

`date` follows stable-channel SemVer ordering after applying the configured
regex/template mapping. Numeric SemVer core components are canonicalized after
template expansion, so `20260626` maps to `2026.6.26` and `20260101` maps to
`2026.1.1`. Existing templates that explicitly consume zero-padded captures,
such as `0*(?P<build>[1-9]\d*)`, remain supported.

Docker Hub update-time example:

```yaml
channel: stable
sort: updated
tag_regex: '^v?\d+\.\d+\.\d+$'
```

`updated` uses Docker Hub tag metadata `last_updated`. It is different from OCI `config.created`: moving or republishing an existing tag can change `last_updated` without rebuilding the image. Ties use mapped SemVer and then lexical tag order. This mode is explicit, Docker Hub-only, and never falls back to creation time. The normal `allow_downgrade: false` guard still applies if the newest-updated tag maps to a lower package version.

Nightly example:

```yaml
channel: nightly
tag_regex: '^nightly(-.*)?$'
```

Nightly versions are deterministic SemVer values derived from the selected target image creation time and digest:

```text
0.0.0-nightly.20260710153020.a1b2c3d4e5f6
```

### Mutable tags and automatic patch bumps

For an upstream that publishes only a mutable tag such as `latest`, opt into digest-based patch bumps:

```yaml
update:
  strategy: publish
  allow_downgrade: false
  version_source:
    type: image
    image: app
    bump: patch

images:
  - id: app
    target: service
    service: app
    source: ghcr.io/acme/app
    channel: custom
    sort: created
    tag_regex: '^latest$'
    delivery:
      mode: lazycat
```

The Action compares the selected target-platform digest with the currently delivered image. An equal digest is a successful no-op and retains the current package version. A changed digest increments only the patch component (`1.4.6` → `1.4.7`), delivers the new image, and follows the normal versioned Release/store path. The current package version must be strict stable SemVer without prerelease or build metadata. `bump: patch` cannot be combined with `allow_downgrade`, tag-to-version mapping, or a non-`custom`/non-`created` rule.

Mutable `direct` and `mirror` references are digest-pinned so the previous state is durable. Mutable mirrors require `require_digest_match: true`. Official-store workflows must continue to use `delivery.mode: lazycat`. Dry-run performs the same digest comparison without copying or writing. `image-results` reports `currentDigest`, `sourceDigest`, `digestChanged`, `bump`, `previousVersion`, and `selectedVersion` for auditability.

For LazyCat delivery, the selected source digest is persisted in the Manifest's `upstream` comment and the remote copy uses that digest-pinned source reference. Subsequent runs compare the baseline without anonymously reading the private LazyCat Registry. A legacy LazyCat reference without a baseline performs one authenticated digest-pinned copy and compares the returned content-addressed reference; an external runtime performs one Registry migration copy without a version bump. A dry-run fails closed only for the legacy private-reference case where no read-only baseline exists yet.

Custom example:

```yaml
channel: custom
sort: created
tag_regex: '^edge-'
version_regex: '^edge-(?P<version>\d+\.\d+\.\d+)$'
version_template: '{version}'
```

`version_template` may reference every named capture from `version_regex`:

```yaml
version_regex: '^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$'
version_template: '{version}.{build}.0' # 20260603.01 -> 20260603.1.0
```

The `version` group remains required. Unknown placeholders and expanded values that are not valid SemVer fail closed. When a regex/template mapping is configured, leading zeroes in the three numeric SemVer core components are removed before validation; prerelease and build identifiers remain subject to strict SemVer rules.

Registry discovery uses `github.com/google/go-containerregistry`. `tag_regex` and `exclude_regex` run before the Action fetches individual manifests. `max_tags` bounds the raw Registry tag list and defaults to `10000`; set it per image only for a known large upstream, up to `50000`. `max_matching_tags` independently bounds filtered candidates and also defaults to `10000`; it must not exceed `max_tags`. For SemVer sorting, the Action ranks tag names first. For `updated`, it ranks Docker Hub tag metadata first. Both inspect manifests in order only until the first usable configured target is found. Creation-time sorting must inspect every eligible manifest because the target image timestamp is part of the ordering. OCI indexes and Docker manifest lists are reduced to `project.target_arch`. The default downgrade guard prevents an older mapped version from silently lowering the application version.

## Image delivery modes

### LazyCat Registry copy

```yaml
delivery:
  mode: lazycat
```

The Action sends the selected source reference to the LazyCat developer platform with `Platform` equal to `project.target_arch` (`amd64` by default, optionally `arm64`). The platform performs a remote Registry-to-Registry copy and returns the final `registry.lazycat.cloud/...` reference. Local Docker is not used for this copy.

This mode prefers a PAT in `LZC_API_TOKEN`; existing workflows may keep their legacy lzc-cli session token in `LAZYCAT_TOKEN`. `LZC_API_HOST` defaults to `appstore.api.lazycat.cloud` for PAT authentication and may be overridden. It is the only delivery mode accepted when `stores.official.enabled` is true.

### Configurable Registry mirror

```yaml
delivery:
  mode: mirror
  require_digest_match: true
```

When `image_template` is omitted, Docker Hub uses `docker.1ms.run` and GHCR uses `ghcr.1ms.run`. Existing configurations may keep a hand-written `image_template`; without an environment override it remains unchanged. Templates support `{tag}`, `{digest}`, and `{source}`. With `require_digest_match: true`, the Action inspects the mirror image for the configured target platform and requires its digest to match the source digest before editing the Manifest.

Mirror verification never copies an image: failures use `MIRROR_VERIFICATION_FAILED`, while `IMAGE_COPY_FAILED` is reserved for LazyCat Registry delivery.

The reusable workflow automatically reads GitHub Repository/Organization Variables named `LAZYCAT_DOCKER_MIRROR`, `LAZYCAT_GHCR_MIRROR`, and `LAZYCAT_REGISTRY_MIRRORS`. Undefined Variables resolve to an empty string and safely fall back. Optional workflow inputs can override those Variables for one caller:

```yaml
with:
  docker-mirror: mirror.example/docker
  ghcr-mirror: mirror.example/ghcr
  registry-mirrors: quay.io=mirror.example/quay,registry.example.com=mirror.example/registry
```

The Composite Action accepts `docker-mirror`, `ghcr-mirror`, and `registry-mirrors` inputs and falls back to equivalent job/step environment variables; composite metadata cannot access the GitHub `vars` context. A caller that wants Repository/Organization Variables maps them in its own workflow, for example `docker-mirror: ${{ vars.LAZYCAT_DOCKER_MIRROR }}` under `with`. Values are Registry/path prefixes without a URL scheme. Precedence is reusable/composite input (including a caller-mapped GitHub Variable), composite environment, existing `image_template`, then the built-in Docker Hub/GHCR default. Missing or empty values are not errors. Other registries require an entry in `LAZYCAT_REGISTRY_MIRRORS`.

For migration, mirror delivery can recover an omitted `source` from the Manifest's `upstream` comment or from a known built-in/configured mirror reference. For example, `docker.1ms.run/acme/api:v1` recovers `docker.io/acme/api`. The Action does not rewrite `.github/lazycat-action.yml`; if the upstream cannot be recovered unambiguously, it fails before querying a Registry or editing the Manifest.

### Direct source image

```yaml
delivery:
  mode: direct
```

The Manifest uses the selected source image directly. The Action performs no copy. Use this for a private store or a deployment that intentionally relies on an external Registry or image accelerator.

`direct` and `mirror` are rejected when official-store mode is enabled. They are intended for non-official distribution.

## Does the Runner need Docker?

| Scenario | Docker requirement |
|---|---|
| Inspect public OCI tags and manifests | No |
| LazyCat remote image copy | No |
| Direct or mirror reference update | No |
| Authenticate the reusable workflow to a private source Registry | Docker CLI is required; GitHub-hosted Linux Runners include it |
| Run your own Docker buildscript | Docker is required |
| Execute x64 Dockerfile `RUN` steps on an ARM64 Runner | Docker Buildx and QEMU are required |

Select the Docker toolchain only when the project buildscript needs it:

```yaml
with:
  toolchains: docker
  enable-qemu: true
```

For private source Registry inspection, add these repository secrets:

```text
REGISTRY=ghcr.io
REGISTRY_USERNAME=<username>
REGISTRY_PASSWORD=<token or password>
```

The reusable workflow runs `docker/login-action`, which writes Docker credentials used by the OCI client. These credentials authenticate Action-side inspection. LazyCat's remote `CopyImage` API has no source Registry credential fields in the lzc-cli 2.0.9 contract, so a private source used with `mode: lazycat` must also be pullable by the developer platform.

## Authentication

LazyCat image copy and official LPK publishing prefer a PAT:

```text
LZC_API_TOKEN=your-personal-access-token
```

The production defaults are:

```text
LZC_API_HOST=appstore.api.lazycat.cloud
LZC_APPSTORE_COS_DOMAIN=dl.lazycat.cloud
```

Existing workflows may instead keep their lzc-cli session token under the historical name:

```text
LAZYCAT_TOKEN=your-existing-lzc-cli-session-token
```

The two variables select different authentication protocols; they are not aliases. `LZC_API_TOKEN` sends requests under `/sdk/v3/developer` with `X-API-Token`. `LAZYCAT_TOKEN` preserves the legacy developer endpoints and `X-User-Token` session authentication. When both are set, `LZC_API_TOKEN` wins. `LZC_API_HOST` applies to the PAT API and defaults to `appstore.api.lazycat.cloud`. `LZC_APPSTORE_COS_DOMAIN` is used only for anonymous `skip_if_version_exists` lookups, and credentials are never sent to that domain. Domain overrides contain only a host name, without a scheme or path.

The reusable workflow passes `LZC_API_TOKEN` and `LAZYCAT_TOKEN` separately so the nested Action can preserve their protocol semantics. It does not create, copy, or synchronize GitHub Secrets. Historical callers can keep only `LAZYCAT_TOKEN`; new callers should configure only `LZC_API_TOKEN`. Direct composite and local callers use the same precedence. Never put either credential in repository configuration, normal workflow inputs, or logs. Project build scripts do not receive either variable.

## Pull request and Release workflows

### Safe default: PR, then publish after merge

Use the scheduled workflow above with `strategy: pull`. Add a second caller for the default branch:

```yaml
name: Publish merged LazyCat update

on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

After the update PR is merged, the default-branch run rebuilds the LPK. If `v<package version>` has no Release, the workflow creates it and uploads the LPK. Existing same-name assets are reused only when GitHub reports the same SHA256 digest; a different digest fails the run.

### Direct publish

Set:

```yaml
update:
  strategy: publish
```

A successful scheduled or manual image check commits only the managed package and Manifest files with `[skip ci]`, pushes the current branch, creates `v<version>`, and uploads the LPK to a GitHub Release. An existing tag is never moved. If it points to another commit, the workflow fails.

Direct publish creates the Git commit, tag, GitHub Release, and Release Asset. If a store is enabled, the reusable workflow then submits the verified LPK to that store. Store publishing never runs for `strategy: pull`.

Before an automatic scheduled or manually dispatched direct publish, an enabled official store is queried through the authenticated developer API for an exact version awaiting review. For an image version source, `stores.official.continue_if_newer_version` defaults to `true`: the real image check selects the latest candidate and compares it before mirror verification, LazyCat delivery, or file edits. For mutable `bump: patch` sources, the candidate application version is planned from the persisted source digest: an unchanged digest keeps the current version and a changed digest selects the next patch. The run pauses when the waiting-review version is equal to or newer than that candidate; if the candidate is newer, the same selection continues through automatic update and publication. Immediately before official upload, the reusable workflow repeats the authenticated comparison against the final verified LPK version so a review created during build/Release work cannot bypass the gate. Set the option to `false` to restore the conservative behavior of pausing for every pending review. A pause reports `official-review-pending: true` plus `official-review-version`; the initial pause does not edit, commit, push, tag, create a Release, or reconcile either store, while a final recheck pause prevents official upload/review submission. Git version sources pause for any pending review because there is no upstream image candidate to compare. Invalid/non-SemVer comparisons, authentication errors, and remote failures fail closed. Explicit operations, dry runs, pull-request updates, and Tag/Release publication keep their existing behavior.

## Store publishing

Store publication happens only after the workflow has uploaded or safely reused a GitHub Release Asset and confirmed its GitHub-reported SHA256. Projects with no `services` or `images`, including static Web and Exec applications, use the same store flow.

### LazyCat official developer platform

Enable official lint and publishing:

```yaml
update:
  strategy: publish
  version_source:
    type: git

stores:
  official:
    enabled: true
    continue_if_newer_version: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
    application:
      language: zh
      name: Example App
      brief: A focused workspace for collaborative agents
      description: A longer store description shown on the application page.
      keywords: agents, collaboration, workspace
      source: https://github.com/acme/example
      source_author: acme
      support_pc: true
      support_mobile: true
      screenshot_pc_files:
        - .github/screenshots/pc-1.png
        - .github/screenshots/pc-2.png
      screenshot_mobile_files:
        - .github/screenshots/mobile-1.png
        - .github/screenshots/mobile-2.png
        - .github/screenshots/mobile-3.png
```

`create_if_missing: false` publishes only to an application that already exists. When creation is enabled, `application.name` defaults to `package.yml.name`; `language` defaults to `zh`. Official mode enforces the lzc-cli-compatible preferences, including official locales, an icon no larger than 200 KB, SemVer metadata, and LazyCat Registry runtime images. General compatibility warnings such as an unknown `container_name` remain visible but do not block the build. Only warnings classified as official-store warnings block official publication, and they never block a private-only workflow. Any configured `direct` or `mirror` image makes configuration fail before publishing.

The application-information fields are optional. Adding `brief`, `description`, `keywords`, either support flag as true, or either screenshot list enables authenticated first-submission state detection through the developer API; `language`, `name`, `source`, and `source_author` alone retain create-only behavior. Both support flags default to false. The Action does not infer first submission from the public catalog. It distinguishes a missing application, an application whose information is incomplete, approved information, and an existing pending review. Approved information is not uploaded again. A pending review fails closed before any LPK or screenshot upload.

Screenshot files must already be committed and pushed to a ref the workflow will checkout. An agent may use `agent-browser` to capture project-confirmed desktop and mobile views, save them under a directory such as `.github/screenshots/`, and then run this Action. The Action never downloads screenshots from remote URLs. Desktop support requires 2-8 files and mobile support requires 3-8 files. Inputs must be PNG or JPEG, at most 15 MiB, and between 320 and 3840 pixels in both dimensions. Each image is center-cropped to 16:9 and uploaded as PNG. Paths outside `project.root`, symbolic links, non-regular files, and unsafe filenames are rejected. Safe failures include the repository-relative screenshot path and an allowlisted reason without exposing runner paths or credentials.

`skip_if_version_exists: true` performs an anonymous exact-package lookup after the LPK is verified. An equal version succeeds with `published: false`, `skipped: true`, and `skipReason: version-already-online`. When both values are valid SemVer, an online version newer than the candidate is also skipped with `skipReason: online-version-newer` while `update.allow_downgrade: false`; explicit `allow_downgrade: true` permits the rollback submission. Non-SemVer values use exact equality only and are never ordered lexically. Skips happen without resolving a developer token or submitting the LPK. The anonymous lookup makes up to three attempts with exponential backoff for status-less connection failures, HTTP 429, and HTTP 5xx. Not-found continues publishing; other errors or retry exhaustion stop the operation. The option defaults to `false`, and `dry-run` remains network-free.

When `LZC_APPSTORE_COS_DOMAIN` is set, that lookup uses the configured COS domain; otherwise it uses the production catalog.

Official publishing always uploads the verified local LPK file as multipart data; it never sends the GitHub Release URL to the official platform. A recovered Release Asset is first downloaded beneath the project root and revalidated.

Official retry is opt-in and defaults to `enabled: false`. `max_attempts` includes the initial attempt and accepts 2-10 when enabled. `initial_delay` and `max_delay` use Go duration syntax. A safe retry before review repeats the application existence check and reopens the LPK, while credentials are resolved once. Upload/check failures may retry status-less connection/TLS/reset errors, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; a review network failure or 5xx is returned without replay because the server may already have accepted the non-idempotent request. Cancellation, deadline expiry, authentication/permission failures, NotFound, integrity failures, HTTP 400, and other 4xx responses are not retried.

Failures identify the safe stage as `store.official.upload` or `store.official.review`. The Action never prints a raw upstream response body. For valid JSON failures it may display a normalized, bounded `message`, `msg`, string `error`, or nested `error.message`/`error.msg`; suspected credential content is suppressed. In a dual-store reusable workflow, an official failure becomes a warning and `store-results.official.failureReason: official-publish-failed` after the private result is preserved. An official-only workflow remains strict and fails. If the official store is disabled, official lint blocking, precheck, credentials, and publication do not run.


### MiaoMiao private store

Configure the application metadata without putting credentials in the repository:

```yaml
stores:
  official:
    enabled: false
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

Add these GitHub secrets:

```text
APPSTORE_URL=https://store.example.com
APPSTORE_TOKEN=lcst_...
APP_ID=42
PRIVATE_STORE_GROUP_CODES=ABC123,LATE23
```

`APP_ID` and `PRIVATE_STORE_GROUP_CODES` are optional. Group codes are access credentials: store them as a GitHub Secret, comma-separated. They are used only by the anonymous latest-version lookup, sent through the toolkit's default `X-Group-Codes` header, and never written to Action inputs, outputs, summaries, or result JSON. The toolkit removes Cookie jars and rejects redirects so group codes are not forwarded to another origin.

With `skip_if_version_exists: true`, the Action queries the exact package through the public Miaomiao latest-version API before reading `APPSTORE_TOKEN`. Equal and newer-online SemVer versions follow the same `version-already-online` / `online-version-newer` rules as the official store, independently per store. Status-less connection failures, HTTP 429, and HTTP 5xx are retried up to three attempts with exponential backoff. Not-found continues publishing; other errors or retry exhaustion stop the operation. If `APP_ID` is absent during a real publish, the write client searches first by exact `packageId`, then calls the authenticated `GET /api/v1/apps/by-name?name=...` resolver with `stores.private.name`. The store returns only the unique exact-name application to which the Token may upload; 404 creates a new application, while ambiguity or authorization errors stop. A name-resolved historical application may retain a different `packageId`; its numeric ID is used only to append the new external version. If `APP_ID` is present, the client still verifies that the application's `packageId` matches the LPK before adding a version.

### Release/store reconciliation

Scheduled or manually dispatched `publish` workflows also reconcile GitHub Releases with both stores. If image inspection is unchanged but the current version has no Release or exact versioned asset, the reusable workflow performs a recovery build, verifies the LPK, and creates the missing Release/asset. If the exact `<package-id>-v<version>.lpk` already exists but a store lacks that version, it downloads the asset beneath the project root, verifies the GitHub `sha256:` digest and local SHA256, then submits those same bytes. A store already reporting the version is skipped, and the workflow never guesses another file or version.

### GitHub Secret scope and precedence

The reusable workflow reads ordinary GitHub Actions Secrets by name, regardless of whether they are defined for the organization or the repository. Organization Secrets must grant the current repository access through their repository policy.

When the same Secret name exists at multiple levels, the most specific value wins: an Environment Secret takes precedence over a Repository Secret, and a Repository Secret takes precedence over an Organization Secret. For example, a repository-level `APPSTORE_URL` overrides an organization-level `APPSTORE_URL`. Use organization Secrets for shared defaults and repository Secrets only for intentional per-repository overrides. Do not define the same name at several levels unless that override is deliberate.

The Action sends JSON to `POST /api/v1/apps` for a new application or `POST /api/v1/apps/{APP_ID}/versions` for an external version. Both `downloadUrl` and the confirmed 64-character lowercase `sha256` are required. The reusable workflow passes the SHA verified against GitHub to the publish operation, which recomputes the local LPK and rejects any mismatch. The URL must be a real `https://github.com/<owner>/<repo>/releases/download/...` asset URL. The store can record the supplied checksum without downloading the LPK merely to recompute it. The same version and SHA256 is returned as an idempotent existing result; different content under the same version fails.

The private store supports Docker `lazycat`, `direct`, and `mirror` delivery, plus applications with no Docker images. `direct` and `mirror` applications are intentionally not publishable to the official store.

## Tag and release builds for static, Exec, Go, Rust, and TypeScript projects

Projects without Docker services use Git as the version source:

```yaml
update:
  strategy: pull
  version_source:
    type: git
```

Choose either a tag-triggered workflow or a release-triggered workflow. Enabling both for the same tag causes two builds.

Tag trigger:

```yaml
name: Build tagged LPK

on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      toolchains: go
    secrets: inherit
```

Release trigger:

```yaml
name: Build released LPK

on:
  release:
    types: [published]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      changelog: ${{ github.event.release.body }}
      toolchains: node
      node-package-manager: pnpm
    secrets: inherit
```

For an ordinary `v<version>` tag, the Action removes the leading `v`. When an explicit version is supplied, a matching SemVer event tag is preserved. A component tag such as `client-v0.1.38` or `server-v0.1.44` must end with the matching `-v<version>` suffix; the Action preserves that event tag as the Release identity and rejects an unrelated suffix or version mismatch. Without an explicit version, event tags must use canonical `v<version>`. The Action updates `package.yml.version`, runs the project buildscript, builds and reopens the LPK, lints it, computes SHA256, and uploads it to the matching Release. If the tag/release checkout changed `package.yml`, the workflow synchronizes that file to the default branch after a successful asset upload.

### TypeScript static Web build

`lzc-build.yml`:

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

`scripts/build.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

Use `toolchains: node` and either pass `node-version` or commit `.node-version`.

If `.github/lazycat-action.yml` also declares `build.toolchains`, its toolchain kinds must match the reusable workflow input. Explicit versions must match when both places provide one.
`build.toolchains[].version` is supported only for `go`, `node`, and `rust`; omit it for `docker`.

### Go Exec build

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

Use `toolchains: go` and either pass `go-version` or keep the Go version in `go.mod`.

### Rust Exec build

```bash
#!/usr/bin/env bash
set -euo pipefail
cargo build --release --target x86_64-unknown-linux-gnu
mkdir -p dist/content
cp target/x86_64-unknown-linux-gnu/release/example dist/content/app
```

Use `toolchains: rust`. Pass `rust-toolchain`, or commit a `rust-toolchain.toml` with a `toolchain.channel` value. The reusable workflow installs both `x86_64-unknown-linux-gnu` and `aarch64-unknown-linux-gnu`; the buildscript selects the triple matching `LAZYCAT_TARGET_ARCH` and provides any required cross-linker.

### Docker buildscript

```bash
#!/usr/bin/env bash
set -euo pipefail
docker buildx build \
  --platform "${LAZYCAT_TARGET_PLATFORM}" \
  --load \
  -t example-build:local .
```

Use `toolchains: docker`. On ARM64, keep `enable-qemu: true` if Dockerfile build stages execute x64 programs.

Complete copyable files are under [`examples/`](examples/):

- [`docker-stable-lazycat`](examples/docker-stable-lazycat/.github/lazycat-action.yml) and [`docker-mirror`](examples/docker-mirror/.github/lazycat-action.yml)
- [`go-exec`](examples/go-exec/.github/workflows/lazycat.yml) and [`rust-exec`](examples/rust-exec/.github/workflows/lazycat.yml)
- [`typescript-static`](examples/typescript-static/.github/workflows/lazycat.yml) and [`typescript-exec`](examples/typescript-exec/.github/workflows/lazycat.yml)
- [official and private stores together](examples/stores/.github/workflows/lazycat.yml)

The examples do not map `LAZYCAT_TOKEN` by default. New callers use `LZC_API_TOKEN`; add the legacy Secret only when an existing workflow still depends on an lzc-cli session token and has not migrated to PAT authentication. Do not map both credentials as a generic fallback.

The TypeScript Exec example expects `@yao-pkg/pkg` in the committed lockfile and demonstrates the default `amd64` target with `node22-linux-x64`. TypeScript static assets are architecture-neutral. Go, Rust, TypeScript Exec, and Docker builds must honor `LAZYCAT_TARGET_ARCH`/`LAZYCAT_TARGET_PLATFORM`; projects opting into arm64 need matching toolchains and packaged runtimes.

## Static and Exec Manifests can have no services

Static Web:

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

Exec:

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

These projects do not need an `images` section. Their version comes from the tag or release.

## Outputs

| Output | Meaning |
|---|---|
| `operation` | Resolved `check`, `build`, `publish-official`, or `publish-private` operation |
| `changed` | Managed project files changed |
| `package-id` | LazyCat package ID |
| `package-file` | Absolute `package.yml` path |
| `manifest-file` | Absolute Manifest path |
| `version` | Normalized SemVer without a leading `v` |
| `tag` | Matching event tag when an explicit version is supplied; otherwise normalized `v<version>` |
| `lpk-path` | Absolute built LPK path inside the job |
| `sha256` | Lowercase 64-character LPK SHA256 |
| `download-url` | Verified GitHub Release Asset URL when released |
| `image-results` | JSON array of selected and delivered images |
| `store-results` | JSON object containing official/private publication results |
| `official-store-enabled` | Official store is enabled in configuration |
| `official-review-pending` | Automatic direct publication paused because an official review is in progress |
| `official-review-version` | Exact version currently awaiting official review, when present |
| `private-store-enabled` | Private store is enabled in configuration |
| `update-strategy` | `pull` or `publish` |
| `channel` | Channel of the version-source image |
| `result-file` | Complete secret-free JSON result path |
| `runner-arch` | `amd64` or `arm64` |
| `target-platform` | `linux/amd64` by default, or `linux/arm64` when `project.target_arch: arm64` |

Example `image-results` item:

```json
{
  "id": "web",
  "target": "service",
  "service": "web",
  "platform": "linux/amd64",
  "tag": "v2.0.0",
  "sourceRef": "ghcr.io/acme/example-web:v2.0.0",
  "sourceDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "deliveryMode": "lazycat",
  "deliveredRef": "registry.lazycat.cloud/acme/example-web:copy-id",
  "copied": true,
  "copyResult": {
    "sourceImage": "ghcr.io/acme/example-web:v2.0.0",
    "platform": "amd64",
    "lazyCatImage": "registry.lazycat.cloud/acme/example-web:copy-id",
    "finished": true
  }
}
```

`.lazycat-action/result.json` contains the complete secret-free result. Tokens, passwords, cookies, and authorization headers are not written to outputs or summaries.

Example `store-results`:

```json
{
  "official": {
    "published": true,
    "skipped": false,
    "created": false,
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "uploadUrl": "/developer/uploads/example.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "private": {
    "published": true,
    "skipped": false,
    "created": false,
    "existing": false,
    "appId": "42",
    "versionId": "56",
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "downloadUrl": "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}
```

When an equal online version is found, the selected store result instead contains `published: false`, `skipped: true`, and matching `version`/`onlineVersion`; no write credentials or submission endpoint are used.

## Artifact versus Release Asset

- Every non-empty build result is uploaded as a Workflow Artifact for CI inspection.
- Pull-request mode stops after the Artifact and PR.
- Release flows also attach the LPK to a GitHub Release and return `download-url`.
- Private-store publishing uses the confirmed Release Asset URL plus local SHA256, so the store can trust the provided digest without downloading the file just to compute it.

## Dry run

```yaml
with:
  operation: check
  config: .github/lazycat-action.yml
  dry-run: true
```

Dry run selects versions and reports planned references without copying images, editing files, running the buildscript, creating a PR, or creating a Release.

See the [design specification](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md) for the complete target behavior.
