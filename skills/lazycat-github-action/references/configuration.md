# Configuration reference

`version: 2` adds an external `source`, resumable `state`, and `build.prepare` phase. Git sources support `branch-head`, `tag`, `release`, and `semver-tag`; `release` uses the matching immutable Git tag. OCI sources select a target-platform digest. `changelog.mode: github-release` reads the selected stable tag's published GitHub Release body; the default `git-commits` mode reads commit subjects between tags.

Start from the [complete adapter](../assets/adapter/) and choose a
[scenario recipe](scenarios.zh-CN.md). Fields below describe current executor
behavior; the examples are not an app-specific deployment specification.

## Source and preparation

Select a single authoritative source. Stable Git tags are appropriate when
upstream releases define versions; OCI tags work when official image releases
are authoritative. `semver-tag` is Git tag discovery, not the GitHub Releases
API. A `github-release` changelog additionally requires a published, non-draft,
non-prerelease Release for that exact tag.

`{tag}` preserves a v prefix; `{source_version}` uses the normalized version.
`{version}` is the allocated application version, which may differ from source.
`{revision}` is shortened; `{source_revision}` is the full revision in
`build.prepare.output_image`. Git revisions are commit SHAs, not OCI digests.

| Mode | Use | Image output |
|---|---|---|
| `images` | Bind one or more published version images to a Git release | `images[].source` templates |
| `passthrough` | Copy a selected OCI image unchanged | Original platform digest |
| `dockerfile` | Build from source or customize an OCI image | Required remote `output_image` |
| `command` | Application-specific build or candidate test | Optional `output_image`; otherwise image bindings |

`run_buildscript` separately controls the LPK `lzc-build.yml` buildscript; it
does not enable/disable `build.prepare`. The starter disables it because it
has no LPK buildscript. Enable it when using a packaging-time digest guard or
content build. Docker preparation needs the reusable workflow's `toolchains:
docker`, a real Dockerfile/build context and registry push permission.

## Private Git sources

`auth_ref: source` resolves these environment variables without putting credentials in YAML:

- `LAZYCAT_AUTH_SOURCE_SSH_KEY`
- `LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS`
- `LAZYCAT_AUTH_SOURCE_TOKEN`
- `LAZYCAT_AUTH_SOURCE_USERNAME`

The reusable workflow maps the corresponding `SOURCE_*` Secrets to these
environment names for `auth_ref: source`. For private Git changelogs use
`git-commits` with the same `changelog.auth_ref`; do not assume SSH credentials
authenticate the GitHub Release HTTP endpoint.

`dockerfile` mode injects `SOURCE_IMAGE`, `SOURCE_REF`, `SOURCE_REVISION`, and `SOURCE_VERSION` as build arguments. `command` mode receives the same values as `LAZYCAT_*` environment variables plus `LAZYCAT_SOURCE_DIR`, `LAZYCAT_OUTPUT_IMAGE`, and `LAZYCAT_TARGET_PLATFORM`.

`images` mode coordinates multiple runtime images for one source release. Configure a source template on each image binding. Templates accept `{tag}`, `{source_version}`, `{version}`, and `{revision}`. Delivery completes for the whole set before one Manifest update, and the lock records every source digest and runtime reference.

`delivery.staging_image` adds an immutable intermediate registry reference and also accepts `{id}`. The reusable workflow must set `enable-image-staging: true`; it installs pinned `crane`, verifies that the staged target-platform digest matches the source, and only then calls LazyCat copy-image.

`delivery.copy_source` points at an anonymous proxy and accepts the same placeholders. The target-platform digest must exactly match the original source. It is mutually exclusive with `staging_image`.

The server must be able to pull the copy source. `LZC_API_TOKEN` authenticates
the developer API, not the upstream registry. Use the final returned official
image reference; do not derive a personal registry namespace from the package ID.

## Numeric vendor versions

For explicit numeric OCI sorting, `sort: numeric` ranks all numeric components.
`version_regex` must capture the complete numeric version as `version`, while
`version_template` maps named groups to a three-component application base:

```yaml
sort: numeric
tag_regex: '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+-[a-f0-9]+$'
version_regex: '^(?P<version>(?P<major>[0-9]+)\.(?P<minor>[0-9]+)\.(?P<patch>[0-9]+)\.[0-9]+)-[a-f0-9]+$'
version_template: '{major}.{minor}.{patch}'
```

This is opt-in. Retain the full source version in comparisons and state; a new
build sharing the same application base receives an increasing app patch.

## New official application

Existing apps normally keep `create_if_missing: false` and require owner or
collaborator permission. For a new app you may create its listing in the
developer platform first, or explicitly configure automatic creation:

```yaml
stores:
  official:
    enabled: true
    create_if_missing: true
    skip_if_version_exists: true
    application:
      language: zh
      name: 示例应用
      brief: 请填写实际应用的一句话介绍
      description: 请填写实际功能和首次使用说明
      keywords: 示例
      source: https://github.com/example/application
      source_author: example
      support_pc: true
      support_mobile: false
      screenshot_pc_files:
        - screenshots/desktop-1.png
        - screenshots/desktop-2.png
```

The referenced real screenshots must exist. Match support/platform flags to the
application, not to another adapter. This option cannot grant access to a
package owned by someone else. The starter icon and example metadata must be
replaced before submission.

## State and changelogs

The state file is generated and tracked, not copied from a sample. Processed
source identities suppress automatic re-publication despite recipe changes.
`packaged` can resume submission. A rejected source requires explicit
`retry-rejected` after repair, allocating a new application patch. A newer
source may supersede an older pending review. Never edit state to fake success.

Only `stores.official` is valid. Use `create_if_missing`, `skip_if_version_exists`, screenshots, and retry policy as required by the official developer platform. Changelog submission locally detects Chinese or English and sends the unchanged original under one language key; the store handles missing translations. Mixed text uses dominant prose, ignoring generated headers, links, code, and technical identifiers. No translation credentials are needed.

Omit the legacy `changelog_locales` setting in new recipes. Existing lists still load, but are not translation targets and never duplicate an original into both `zh` and `en`. For otherwise unclassifiable notes, a single `[zh]` or `[en]` can provide a language hint; without one, submission fails before uploading. Other languages are not automatically detected, and historical reviews remain unchanged.
