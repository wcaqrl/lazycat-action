# Configuration reference

## Core shape

```yaml
version: 1
project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/application.lpk
  target_arch: amd64
update:
  strategy: pull
  allow_downgrade: false
  version_source:
    type: image
    image: web
build:
  toolchains:
    - kind: go
      version: 1.25.x
  run_buildscript: true
images: []
stores:
  official:
    enabled: false
    continue_if_newer_version: true
    skip_if_version_exists: false
    retry:
      enabled: false
  private:
    enabled: false
    skip_if_version_exists: false
```

Unknown fields fail validation. Paths must remain under `project.root`. Output must end in `.lpk`. `project.target_arch` defaults to `amd64` and accepts `amd64` or `arm64`; the target OS remains Linux.
`build.toolchains[].version` is supported only for `go`, `node`, and `rust`; `docker` must omit `version`.

`project.output` is the verified build output and validation Artifact. When the caller sets reusable-workflow input `versioned-release-asset: true`, the workflow copies that verified file to `<package-id>-v<version>.lpk` for the GitHub Release. The copy stays beside the verified LPK under `project.root`. Private publication uses the verified Release Asset URL and SHA256; official publication uploads the same locally verified LPK bytes and SHA256 without receiving that URL.

## Go Template Manifest handling

Manifests may contain standalone Go Template controls `if`, `else`, `end`, `with`, and `range`. Detect them before YAML parsing, never evaluate repository templates, protect each control line during inspection/editing, and restore its exact bytes, indentation, order, and trim markers. Inline expressions remain unchanged. Fail closed on marker collisions, missing/duplicate markers, invalid protected YAML, ambiguous image targets, or changed control lines; do not replace template values with guessed deployment data.

## Version source

- `type: image`: Docker automation; `image` must name one configured image ID.
- `type: git`: tag/release/static/Exec/source builds; `image` must be empty.

The version source answers “which upstream version changes package.yml.” The image target answers “which Manifest field changes.” They are separate decisions.

## Channels

| Channel | Required behavior |
|---|---|
| `stable` | Highest non-prerelease SemVer by default; sort may be `semver` or Docker Hub `updated` |
| `beta` | Highest alpha/beta/rc/preview SemVer by default; sort may be `semver` or Docker Hub `updated` |
| `date` | Date-like tags mapped to stable SemVer; sort may be `semver` or Docker Hub `updated` |
| `nightly` | `tag_regex` required; newest target-image creation time; sort is `created` |
| `custom` | `tag_regex` and explicit `semver`, `created`, or Docker Hub `updated` sort required |

Use `exclude_regex` to remove Windows/ARM tags. `version_regex` must contain `(?P<version>...)`; `version_template` defaults to `{version}`. Every named capture is available as an exact placeholder. For example, `^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$` plus `{version}.{build}.0` maps `20260603.01` to `20260603.1.0`. Unknown placeholders and non-SemVer expanded values fail closed.

Use `channel: date` for fixed-width date tags that are mapped by a regex/template, for example:

```yaml
channel: date
tag_regex: '^[0-9]{8}$'
version_regex: '^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$'
version_template: '{version}.{month}.{day}'
```

Date uses stable-channel ordering. After any regex/template mapping, the three
numeric SemVer core components are canonicalized so padded fields are valid:
`20260626` becomes `2026.6.26` and `20260101` becomes `2026.1.1`. Explicit
zero-stripping expressions such as `0*(?P<build>[1-9]\d*)` remain compatible;
prerelease and build identifiers continue to use strict SemVer validation.

Design `tag_regex` to filter a release family rather than the currently selected immutable version. First inspect representative upstream tags, then keep the four decisions separate:

1. `channel` defines stable, prerelease, date, nightly, or custom semantics.
2. `tag_regex` includes candidates that may appear in future runs.
3. `version_regex` plus `version_template` extracts and normalizes package SemVer when tag text is nonstandard.
4. `sort` chooses the winning candidate.

For example, to remain on the v2 release line, use `channel: stable`, `sort: semver`, and `tag_regex: '^v?2\.\d+\.\d+$'`. Do not use `tag_regex: '^2\.2\.0$'` after observing version 2.2.0: that immutable exact match excludes every future patch and minor release and is not an automatic update strategy. If the user explicitly wants an immutable one-version pin, state that future discovery is disabled. Exact filters such as `^latest$` are appropriate only for mutable channel names paired with digest comparison, or for that explicit no-update pin.

### Mixed immutable tag families

Some repositories publish several incompatible release families together. The Jenkins image, for example, combines plain two-part weekly tags such as `2.578`, three-part LTS tags such as `2.568.2`, runtime variants such as `2.578-jdk21`, and aliases such as `latest`. If the application intentionally follows the weekly family, default stable discovery can select the LTS family instead. `sort: updated` is also the wrong repair: these are immutable releases whose winner is defined by mapped SemVer, not Docker Hub `last_updated` metadata.

Filter the intended family before mapping and sorting:

```yaml
images:
  - id: ci-server
    target: service
    service: ci-server
    source: jenkins/jenkins
    channel: custom
    sort: semver
    tag_regex: '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
    version_regex: '^(?P<version>(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$'
    version_template: '{version}.0'
    delivery:
      mode: mirror
```

This admits current and future plain weekly tags, rejects LTS/JDK/alias tags, and maps `2.578` to strict package SemVer `2.578.0`. Keep `update.allow_downgrade: false`; a `VERSION_DOWNGRADE_BLOCKED` result after selecting the wrong family is evidence to repair filtering and mapping, not permission to enable rollback. See [`lazycat-contrib/jenkins-lzcapp`](https://github.com/lazycat-contrib/jenkins-lzcapp) for the maintained real-repository configuration.

Nightly mutable tags become deterministic SemVer values based on creation time and the configured target-platform digest.

For a registry that exposes only a mutable tag such as `latest`, configure the image version source with `bump: patch`, plus `channel: custom`, `sort: created`, and an exact `tag_regex`. The Action compares the selected target-platform digest with the currently delivered digest. Equality preserves the current package version; a change increments only the stable SemVer patch component. Bump mode rejects prerelease/build package versions, `allow_downgrade: true`, `version_regex`, non-created rules, and unverified mirrors. Mutable direct/mirror references are digest-pinned; mutable mirrors require `require_digest_match: true`. Official publication still requires LazyCat delivery.

Mutable LazyCat delivery stores the selected source digest in the Manifest upstream comment and uses that baseline instead of anonymously inspecting the private LazyCat Registry. A legacy LazyCat runtime without a baseline performs one authenticated copy and compares the returned content-addressed reference; an external runtime performs one migration copy without bumping. Dry-run fails closed only when a legacy private runtime has no persisted baseline.

Registry discovery uses `github.com/google/go-containerregistry`. `max_tags` limits an image's raw Registry tag list, defaults to `10000`, and may be explicitly raised to at most `50000` for a known large upstream. `max_matching_tags` independently limits tags remaining after `tag_regex` and `exclude_regex`, defaults to `10000`, and must not exceed `max_tags`. Keep both defaults unless the repository has a measured need. SemVer rules rank filtered tag names before manifest inspection and stop at the first usable configured target, falling back past platform-incompatible higher tags. `sort: updated` uses Docker Hub `last_updated`, then mapped SemVer and tag name; it is Docker Hub-only and fails closed instead of falling back to OCI creation time. `created` rules inspect all eligible manifests because the configured target image creation timestamps determine the result.

## Image target examples

Service:

```yaml
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: stable
    delivery:
      mode: lazycat
```

Application image:

```yaml
images:
  - id: runtime
    target: application
    source: ghcr.io/acme/runtime
    channel: beta
    delivery:
      mode: direct
```

Mirror:

```yaml
delivery:
  mode: mirror
  require_digest_match: true
```

`image_template` is optional. Docker Hub defaults to `docker.1ms.run/<repository>:{tag}` and GHCR defaults to `ghcr.1ms.run/<repository>:{tag}`. A historical explicit template is preserved when no environment override is set. `LAZYCAT_DOCKER_MIRROR` and `LAZYCAT_GHCR_MIRROR` replace the corresponding prefix; `LAZYCAT_REGISTRY_MIRRORS` adds comma-separated `registry=mirror-prefix` mappings for arbitrary registries. The reusable workflow obtains these values from explicit inputs and then same-named GitHub Variables; missing values are empty and trigger fallback. Prefixes may include a path but not a URL scheme, tag, or digest.

Resolution precedence is runtime environment mapping, historical `image_template`, then the built-in Docker Hub/GHCR default. New configurations keep `source` explicit. During migration only, mirror delivery may recover a missing source from the Manifest `upstream` comment or a recognized built-in/configured mirror reference. It never writes that recovered source back to `.github/lazycat-action.yml` and fails before Registry access when recovery is ambiguous.

Mirror verification is a read-only Registry operation and reports `MIRROR_VERIFICATION_FAILED`; it does not use the LazyCat image-copy path.

## Build environment

Buildscripts receive version, tag, channel, source date, and LazyCat target variables derived from `project.target_arch`. They do not receive LazyCat credentials, private-store credentials, Registry credentials, GitHub tokens, or GitHub control-file paths.

Only local Docker/buildscript work requires Docker. OCI inspection, direct/mirror edits, and LazyCat remote Registry copying do not invoke local Docker.

## Version downgrade guard

```yaml
update:
  strategy: publish
  allow_downgrade: false
  version_source:
    type: image
    image: web
```

`allow_downgrade` defaults to false. The mapped version-source image SemVer must be greater than or equal to the current package version before delivery or file writes. Equal versions may refresh an image reference or digest. Set true only after the user explicitly confirms an intentional rollback; otherwise `VERSION_DOWNGRADE_BLOCKED` is the required fail-closed result.

## Official store

```yaml
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

Defaults: locales `zh,en`; language `zh`; application name from `package.yml.name`. Application metadata is valid only with `create_if_missing: true`.

The additional information and screenshot fields are optional. Automatic information submission is enabled when any of `brief`, `description`, `keywords`, `support_pc: true`, `support_mobile: true`, `screenshot_pc_files`, or `screenshot_mobile_files` is present. `language`, `name`, `source`, and `source_author` alone retain create-only behavior. Both support flags default to false. When enabled, authenticated state detection uses `/api/v3/developer/app/list` with an exact package match; the public catalog is not a first-submission signal. The Action distinguishes:

| State | Behavior |
|---|---|
| Application missing | Create it when `create_if_missing: true`, then submit LPK and information together. |
| Application exists, information incomplete | Upload configured screenshots and submit one information review with the LPK. |
| Information already approved | Submit only the new LPK/version; do not upload or replace screenshots. |
| Review already pending | Stop with `CONFLICT` before uploading the LPK or screenshots. |

`brief` is required when automatic information submission is enabled. At least one of `support_pc` or `support_mobile` must be true. PC support requires 2-8 `screenshot_pc_files`; mobile support requires 3-8 `screenshot_mobile_files`. A screenshot list is invalid when its matching support flag is false.

Screenshot paths are relative to `project.root` and must name committed local PNG/JPEG regular files. Remote URLs are unsupported. Inputs are limited to 15 MiB and 320-3840 pixels in both dimensions, then center-cropped to 16:9 and encoded as PNG. Parent traversal, absolute paths, symbolic links, unsupported formats, and unsafe filenames fail closed. Safe Action diagnostics may expose only the repository-relative screenshot path and an allowlisted reason.

For agent-generated screenshots, use `agent-browser` to open the application at project-confirmed PC/mobile viewports and states, capture files such as `.github/screenshots/pc-1.png` and `.github/screenshots/mobile-1.png`, verify the minimum counts, then commit and push them to a ref the reusable workflow will checkout. The Action reads the checkout; an uncommitted or unpushed file on the agent's machine is unavailable to GitHub Actions.

`continue_if_newer_version` defaults to true. In an automatic scheduled or manually dispatched direct-publication run, a pending official review is compared with the selected version-source image candidate before mirror verification, LazyCat delivery, or file edits. Mutable `bump: patch` candidates use the persisted source digest to select the current version for an unchanged digest or the next patch for a changed digest; a missing trusted baseline fails closed, and LazyCat delivery copies the selected source through that digest-pinned reference. An equal or newer review pauses the run; an older review allows the same selected candidate to continue through publication. The reusable workflow performs a second authenticated comparison against the final verified LPK immediately before official upload. Set false to pause for every pending review without selecting an image and at final publication. Git version sources always pause when a review is pending. Invalid/non-SemVer comparisons and review lookup/authentication failures fail closed. Explicit operations, dry runs, `pull` strategy, and Tag/Release publication do not use this gate.

`skip_if_version_exists` defaults to false. When true, the Action anonymously queries the exact package after LPK verification. Equality skips with `skipReason: version-already-online`. When both values are valid SemVer, a newer online version skips with `skipReason: online-version-newer` while `allow_downgrade: false`; explicit `allow_downgrade: true` permits publishing. A non-SemVer value uses exact equality only. All skips happen before resolving official credentials. Anonymous lookups make up to three attempts with exponential backoff for status-less connection failures, HTTP 429, and HTTP 5xx. Not-found continues; other errors or retry exhaustion fail closed. `dry-run` does not query.

`retry.enabled` defaults to false. When enabled, `max_attempts` is 2-10 and includes the first attempt; `initial_delay` and `max_delay` use Go duration syntax. Upload/check failures may retry status-less connection/TLS/reset failures, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; a review network failure or 5xx is returned without replay because the request may already have succeeded. Do not retry cancellation, deadline expiry, authentication/permission errors, NotFound, integrity failures, HTTP 400, or another 4xx. A retry before review rechecks application existence and reopens the LPK, while credentials resolve once. Valid `Retry-After` values can extend the jittered delay up to `max_delay`.

Official lint does not turn every compatibility warning into a failure. Unknown `container_name` remains a visible warning; only official warnings block the official precheck, and an equal/newer online version skips before that precheck. Official HTTP failures keep the safe stage and status. The raw body is hidden, while a recognized JSON `message`, `msg`, string `error`, or nested `error.message`/`error.msg` may be displayed after one-line normalization, a 512-byte limit, and credential suppression.

LazyCat developer-platform writes use a PAT in `LZC_API_TOKEN` by default. New and generated callers must not map `LAZYCAT_TOKEN`. Retain `LAZYCAT_TOKEN` only for an existing caller that still depends on an lzc-cli session token and has not migrated to PAT authentication; that path preserves the legacy developer endpoints and `X-User-Token` authentication. These variables are not aliases, and both should not be mapped as a speculative fallback. The reusable workflow accepts them separately for backward compatibility without creating, copying, or synchronizing GitHub Secrets. PAT production defaults are `LZC_API_HOST=appstore.api.lazycat.cloud` and `LZC_APPSTORE_COS_DOMAIN=dl.lazycat.cloud`; either domain may be overridden through its environment variable. Override values contain no scheme or path. Username/password login, `LZC_CLI_TOKEN`, and token files remain unsupported.

## MiaoMiao private store

```yaml
stores:
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

Secrets: `APPSTORE_URL`, `APPSTORE_TOKEN`, optional `APP_ID`, and optional comma-separated `PRIVATE_STORE_GROUP_CODES`. Group codes never belong in this YAML. They are used only for anonymous exact-package lookup and the toolkit sends them through `X-Group-Codes` with Cookie and redirect isolation.

`skip_if_version_exists` has the same default-off, `version-already-online`, `online-version-newer`, non-SemVer fallback, bounded transient-lookup retry, not-found, fail-closed, and network-free dry-run behavior as the official option. `allow_downgrade: false` protects each store independently. Without `APP_ID`, the write client searches by exact package ID and then resolves `stores.private.name` through authenticated `GET /api/v1/apps/by-name?name=...`. A 404 creates an application with `POST /api/v1/apps`; a unique exact-name writable result supplies the ID for JSON `POST /api/v1/apps/{id}/versions`; every other resolver error stops. Historical package-ID differences are allowed only on this server-authorized name path. Requests always send `sourceType: GITHUB`, the GitHub Release Asset `downloadUrl`, and locally computed `sha256`.

With scheduled or manually dispatched `publish` automation, an unchanged image check recovery-builds the current version when its Release or exact versioned asset is missing. An existing exact `<package-id>-v<version>.lpk` can repair a missing store submission without rebuilding. The workflow verifies the GitHub asset digest and downloaded bytes before either store call; it never substitutes an unversioned or differently named LPK.

Secret scope is a workflow concern, not a configuration field. An organization Secret must authorize the repository. If the same name is defined more than once, Environment overrides Repository and Repository overrides Organization.
