# Workflow reference

## Scheduled pull request

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
    secrets:
      LZC_API_HOST: ${{ secrets.LZC_API_HOST }}
      LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
      APPSTORE_URL: ${{ secrets.APPSTORE_URL }}
      APPSTORE_TOKEN: ${{ secrets.APPSTORE_TOKEN }}
      APP_ID: ${{ secrets.APP_ID }}
      PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
```

Keep `update.strategy: pull`. This uploads a validation Artifact and creates/updates a PR. It does not publish stores.

For mirror delivery, the reusable workflow first accepts optional `docker-mirror`, `ghcr-mirror`, and `registry-mirrors` inputs, then reads GitHub Variables named `LAZYCAT_DOCKER_MIRROR`, `LAZYCAT_GHCR_MIRROR`, and `LAZYCAT_REGISTRY_MIRRORS`. Missing inputs and Variables resolve to an empty string and fall back to a historical `image_template` or the built-in Docker Hub/GHCR defaults:

```yaml
with:
  docker-mirror: mirror.example/docker
  ghcr-mirror: mirror.example/ghcr
  registry-mirrors: quay.io=mirror.example/quay
```

These ordinary inputs and GitHub Variables contain only public Registry/path prefixes, never Registry credentials. Composite Action metadata cannot access `vars`; it exposes the same three lower-case inputs, so a composite caller maps `${{ vars.LAZYCAT_DOCKER_MIRROR }}` and its siblings through the caller's `with` block. Missing Variables become empty input values and safely fall back to same-named environment variables, historical templates, or built-in defaults.

For `operation: auto`, a manual `workflow_dispatch` checks an image version source but builds a Git version source, reading `package.yml.version` when no explicit version is supplied. Set the reusable workflow input `version: 1.2.3` to request an explicit build version. Tag pushes build and schedules check images. Explicit operations otherwise keep their requested behavior, but `check` rejects Tag and Release events before image inspection or delivery.

For automatic scheduled or manually dispatched direct publication with the official store enabled, `stores.official.continue_if_newer_version` defaults to true. A pending review equal to or newer than the selected version-source image candidate pauses the workflow; an older review lets that same candidate continue. The comparison happens before mirror verification, LazyCat delivery, and project-file edits; mutable `bump: patch` candidates are planned from the persisted source digest. The reusable workflow repeats the authenticated comparison against the final verified LPK immediately before official upload. Set the option to false to pause for every pending review. Git version sources, missing mutable digest baselines, and invalid/non-SemVer comparisons fail closed by pausing or returning an error; explicit operations, dry runs, `pull` strategy, and Tag/Release publication keep their existing behavior.

## Tag source build

```yaml
name: Publish tagged LPK
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
      go-version: 1.25.x
      versioned-release-asset: true
    secrets:
      LZC_API_HOST: ${{ secrets.LZC_API_HOST }}
      LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
      APPSTORE_URL: ${{ secrets.APPSTORE_URL }}
      APPSTORE_TOKEN: ${{ secrets.APPSTORE_TOKEN }}
      APP_ID: ${{ secrets.APP_ID }}
      PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
```

Use `update.version_source.type: git`. The workflow updates `package.yml.version`, builds, uploads the Release Asset, and syncs the version to the default branch. An ordinary event tag uses canonical `v<version>`. With an explicit version, a matching SemVer event tag is preserved. A component tag such as `client-v0.1.38` must pass `version: 0.1.38` and end with the matching `-v<version>` suffix; the Action preserves the component tag as the Release identity and rejects a mismatched or unrelated suffix. Without an explicit version, noncanonical event tags fail closed. With `versioned-release-asset: true`, the Release filename is `<package-id>-v<version>.lpk`. The private store uses its verified Release Asset URL and SHA256; the official store uploads the same locally verified LPK bytes and SHA256 without receiving that URL.

## Historical LPK migration checkpoint

Before writing Release automation, run `git ls-files '*.lpk'`, then report the tracked file count and total bytes. Stop for an explicit yes/no confirmation immediately before deleting those files, even if broad repository editing was already authorized. A decline preserves every tracked LPK. Approval removes only the reported files, verifies the result, and adds `*.lpk` plus the generated output directory to `.gitignore`; ignore rules alone do not untrack files. Never rewrite history or backfill older Releases without a separate request.

## Go Template Manifest workflow safety

Detect standalone `if`/`else`/`end`/`with`/`range` controls before parsing; you must never evaluate a repository Go Template Manifest. Protect and restore exact control lines, leave inline expressions unchanged, and fail closed on collisions, invalid protected YAML, marker loss/duplication, ambiguous targets, or an unexpected template diff. Compare control lines before/after and run the actual LazyCat build or validation command.

## Release-triggered private/official publication

```yaml
name: Publish LPK
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
    secrets:
      LZC_API_HOST: ${{ secrets.LZC_API_HOST }}
      LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
      APPSTORE_URL: ${{ secrets.APPSTORE_URL }}
      APPSTORE_TOKEN: ${{ secrets.APPSTORE_TOKEN }}
      APP_ID: ${{ secrets.APP_ID }}
      PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
```

Use `update.strategy: publish`. Configure only the repository/organization secrets required by enabled stores:

```text
LZC_API_TOKEN
LZC_API_HOST (optional PAT API override)
APPSTORE_URL
APPSTORE_TOKEN
APP_ID (optional)
PRIVATE_STORE_GROUP_CODES (optional)
```

New and generated callers map `LZC_API_TOKEN` only for LazyCat image delivery or official publishing. Do not add `LAZYCAT_TOKEN` as a redundant fallback when a PAT is available or confirmed working. The workflow does not create, copy, or synchronize GitHub Secrets. `LZC_API_HOST` is an optional PAT API override. `APP_ID` is optional. `PRIVATE_STORE_GROUP_CODES` is an optional comma-separated GitHub Secret; never expose it as a normal workflow input.

### Legacy-only compatibility

Keep `LAZYCAT_TOKEN` only when an existing caller still has an lzc-cli session token and PAT migration is not yet available or authorized:

```yaml
secrets:
  LAZYCAT_TOKEN: ${{ secrets.LAZYCAT_TOKEN }}
```

This is a compatibility exception, not a default example. Do not map both `LZC_API_TOKEN` and `LAZYCAT_TOKEN` for speculative fallback. After the PAT path is confirmed, keep only `LZC_API_TOKEN`. The reusable workflow continues to accept the legacy Secret separately so old callers do not break.

Publishing callers must assign required Secrets explicitly. `secrets: inherit` alone is not sufficient documentation and can hide that an Organization Secret has not authorized a newly added repository.

Organization and repository Secrets use the same names in the reusable workflow. The organization Secret must authorize the repository. For duplicate names, GitHub uses the most specific scope: Environment overrides Repository, and Repository overrides Organization. Treat organization Secrets as shared defaults and repository Secrets as deliberate overrides.

Official retry is configured in `.github/lazycat-action.yml`, not as a workflow input:

```yaml
stores:
  official:
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
```

Enabling it lets upload/check failures retry status-less connection/TLS/reset failures, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; ambiguous review network/5xx outcomes are returned without replay. HTTP 400 and other 4xx responses are not retried. Cancellation and deadline expiry stop requests and waits. A retry before review reopens the LPK and repeats the application existence check; credentials resolve once.

Store steps are independent. A private-store failure does not suppress the official attempt. When both stores are enabled, an official failure is reported as a warning and `failureReason: official-publish-failed` while the private result remains available; an official-only workflow still fails. With official disabled, no official lint blocking or publication path runs. Compatibility lint such as unknown `container_name` stays visible without blocking private publication.

Official errors retain HTTP status and `store.official.upload`/`store.official.review`. The Action never prints a raw response body. It may display a bounded single-line JSON `message`, `msg`, string `error`, or nested `error.message`/`error.msg` after credential-marker suppression.

## Existing Release/store reconciliation

A scheduled or manually dispatched workflow with `update.strategy: publish` is also a repair loop. When image inspection is unchanged but the current version lacks a Release or, with `versioned-release-asset: true`, the exact `<package-id>-v<version>.lpk`, the reusable workflow runs `operation: build` to create and verify the missing artifact before continuing through the normal Release path. When the exact asset already exists, it may download it and fill a missing official or private-store version without rebuilding. Existing assets require the GitHub `sha256:` digest plus a matching locally recomputed SHA256; stores publish or skip independently. Never guess a different filename, version, or digest.

With `update.version_source.bump: patch`, a mutable image digest change produces the next patch version before this Release flow runs. A repeated workflow against the same digest retains the current version; Release reconciliation and per-store `skip_if_version_exists` remain independent duplicate-publication safeguards.

## Source build mapping

| Source | Config toolchain | Workflow input | Required target |
|---|---|---|---|
| Go Exec | `go` | `toolchains: go` | `GOOS=linux GOARCH=$LAZYCAT_TARGET_ARCH` |
| Rust Exec | `rust` | `toolchains: rust` | target triple matching `LAZYCAT_TARGET_ARCH` |
| TypeScript static | `node` | `toolchains: node` | architecture-neutral files |
| TypeScript Exec | `node` | `toolchains: node` | packaged runtime matching `LAZYCAT_TARGET_ARCH` |
| Docker buildscript | `docker` | `toolchains: docker` | Buildx `$LAZYCAT_TARGET_PLATFORM` |

For a real Go source reference, inspect `lazycat-contrib/cat-led`. For a Rust + Node musl reference, inspect `lazycat-contrib/lazycat-neko-webshell`. The latter pins `protoc` from the official GitHub Release with SHA256 verification because Ubuntu's packaged compiler may not support Protobuf Edition 2023. Native dependencies required by Tag publication must be available from the shared buildscript path, not only from a pull-request setup step.

Runner architecture changes only the Action host binary. The build output must match `project.target_arch`, which defaults to amd64 and may be arm64. Use `enable-qemu: true` when Dockerfile execution crosses architectures.

## Direct composite operations

Use the composite Action directly only when another workflow owns GitHub Releases:

The caller also owns checkout and project toolchains. Use the Node.js 24-compatible official majors:

```yaml
- uses: actions/checkout@v7
- uses: actions/setup-node@v7
```

Replace legacy v4 checkout/setup-node steps when updating an existing caller-owned workflow.

```yaml
- uses: wcaqrl/lazycat-action@v1
  id: publish-private
  env:
    APPSTORE_URL: ${{ secrets.APPSTORE_URL }}
    APPSTORE_TOKEN: ${{ secrets.APPSTORE_TOKEN }}
    APP_ID: ${{ secrets.APP_ID }}
    PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
  with:
    operation: publish-private
    config: .github/lazycat-action.yml
    version: 1.2.3
    lpk-path: dist/application.lpk
    download-url: https://github.com/acme/example/releases/download/v1.2.3/application.lpk
    sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    changelog: Release notes
```

The LPK path must remain under the project root. The Action reopens the LPK, checks package/version, and computes SHA256 before sending store metadata.

When `skip_if_version_exists` is enabled, `store-results` includes `skipped`, optional `onlineVersion`, and optional `skipReason`. Equal versions use `version-already-online`; valid SemVer online versions greater than the candidate use `online-version-newer` while `allow_downgrade: false`. Explicit rollback authorization continues publishing, and a non-SemVer value is compared only for exact equality. Decisions are independent per store and happen before write credentials are used. Status-less connection failures, HTTP 429, and HTTP 5xx receive up to three exponential-backoff attempts; not-found publishes; other errors or retry exhaustion stop. `dry-run` remains network-free.
