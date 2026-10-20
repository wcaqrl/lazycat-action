# lazycat-action

[中文](README.zh-CN.md)

`lazycat-action` is a platform-neutral pipeline for the LazyCat official application store. It discovers GitHub, Gitee, or OCI revisions, optionally builds a customized image, copies runtime images through the official developer API, builds and validates an LPK, and submits it for official review.

Source repositories remain unaware of LazyCat packaging. Keep `package.yml`, `lzc-build.yml`, `lzc-manifest.yml`, Dockerfiles, assets, and the version 2 pipeline recipe in a separate application adapter repository.

Only the LazyCat official application store is supported.

## Start here

- [Developer quick start (Chinese)](docs/quickstart.zh-CN.md): from an empty adapter to verified publication.
- [AI task prompt](docs/ai-adapter-guide.md) and [self-contained AI guide](skills/lazycat-github-action/references/adapter-guide.md).
- [Complete starter adapter](skills/lazycat-github-action/assets/adapter/), including hidden workflow files, package metadata, a placeholder PNG and ignore rules.
- [Scenario selection](skills/lazycat-github-action/references/scenarios.zh-CN.md): existing images, coordinated images, custom images and source builds.

The upstream owns source/releases; the adapter owns app-specific configuration,
tests, scheduling and state; this repository supplies the executor. Detection
runs on the adapter's Runner, not a central always-on polling service. Other
developers can use the public `@v1` workflow without forking this repository.
Keep their PAT and source/registry credentials in their own adapter's Secrets.

The new starter defaults manual runs to dry-run and schedules to checks only.
After verification set the adapter's Actions variable `LAZYCAT_AUTO_PUBLISH=true`
to enable scheduled submissions. This does not alter existing adapters or add a
new executor requirement.

## Source identities

- OCI sources compare the selected platform image digest.
- Git tags compare the immutable commit behind the highest matching stable SemVer tag.
- A newer source version may replace an older pending official review. Equal or older candidates pause, while the official store atomically cancels the previous pending review after accepting the newer submission.
- If a previously recorded Git tag moves to another commit, the pipeline stops instead of silently publishing different source under the same release tag.
- Branch sources compare the branch HEAD commit. `branch: auto` resolves the remote symbolic `HEAD` instead of assuming `main` or `master`.
- The final fingerprint also includes the preparation command, Dockerfile, build arguments, and build-context contents.

## Reviews and downloadable LPK releases

After packaging, the reusable workflow runs official review submission and GitHub Release publication as independent parallel jobs. The adapter's `v<application-version>` tag targets the packaging commit. Its Release contains only `<package>-v<application-version>.lpk`; SHA256 is checked internally. A release failure is a warning and cannot prevent store submission. Existing conflicting tags or assets are never overwritten.

Version 2 source pipelines migrate legacy state locks automatically. The lock keeps `packaged_at` and a `reviews` history with source revisions, fingerprints, application versions, LPK hashes, review IDs, statuses, creation times and rejection reasons. Previously submitted sources are not submitted again automatically, including rejected, approved, canceled or deleted reviews. A new source release can still replace an older pending review.

Review synchronization queries the official SDK list with paired `created_at_start` / `created_at_end` values in Asia/Shanghai and matches the saved review ID client-side. The fixed window follows the review's original creation time, not its last polling time. If submission succeeds but pushing its state fails, the next run recovers it from the persisted packaging time, version, automation channel and LPK hash. Locks without timestamps perform a one-time newest-first paginated lookup during migration; missing historical records become `untracked` and still block duplicate submissions. Terminal reviews are retained and no longer polled.

Adapters expose a `workflow_dispatch` boolean `retry-rejected`, default false, and pass it to the reusable workflow. After fixing the adapter, explicitly enable it and disable `dry-run` to rebuild the rejected source with a new application patch version. Old review history remains. The CLI flag is `--retry-rejected`; Gitee uses `LAZYCAT_RETRY_REJECTED=true`. Keep the `@v1` reference, `contents: write`, and the adapter's `LZC_API_TOKEN` secret.

## Minimal version 2 recipe

```yaml
version: 2
project:
  root: .
  output: dist/application.lpk
  target_arch: amd64
source:
  kind: git
  url: git@gitee.com:example/private-app.git
  auth_ref: source
  select:
    strategy: branch-head
    branch: auto
state:
  file: .lazycat-action.lock.yml
update:
  strategy: publish
build:
  prepare:
    mode: command
    context: scripts
    command: ./scripts/build-source.sh
stores:
  official:
    enabled: true
    skip_if_version_exists: true
```

Preparation modes are `images`, `passthrough`, `dockerfile`, and `command`. A Dockerfile build receives the pinned `SOURCE_IMAGE`, `SOURCE_REF`, `SOURCE_REVISION`, and `SOURCE_VERSION`. A command build also receives `LAZYCAT_SOURCE_DIR`, `LAZYCAT_OUTPUT_IMAGE`, and the target platform.

Use `build.prepare.mode: images` for a Git release that coordinates multiple runtime images. Each `images[].source` may use `{tag}`, `{source_version}`, `{version}`, or `{revision}`. All images are pinned and delivered before the Manifest is updated as one change; build and state failures restore the previous image set and package version.

Set `images[].delivery.staging_image` when the official copy service cannot reliably read the upstream registry. The Action uses `crane` to mirror the immutable platform image, verifies the staged digest, and copies from that reference. Enable `enable-image-staging` on the reusable workflow.

Use `images[].delivery.copy_source` when an anonymous registry proxy already serves the image. The Action verifies that its target-platform digest exactly matches the original source before copying. `copy_source` and `staging_image` are mutually exclusive.

Private source credentials are referenced by `auth_ref` and supplied through environment variables such as `LAZYCAT_AUTH_SOURCE_SSH_KEY`; they never belong in YAML.

## Upstream changelog for official review

### Vendor numeric versions (v1.8.0)

OCI sources can explicitly select dotted numeric versions with more than three
components. This is opt-in; existing SemVer Git/OCI adapters keep their rules.
For example, Plex's `1.43.3.10896` must rank above `1.43.3.10861`, and comparing
only `1.43.3` or using SemVer `+build` would miss the update:

```yaml
source:
  kind: oci
  image: docker.io/plexinc/pms-docker
  select:
    channel: stable
    sort: numeric
    tag_regex: '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+-[a-f0-9]+$'
    version_regex: '^(?P<version>(?P<major>[0-9]+)\.(?P<minor>[0-9]+)\.(?P<patch>[0-9]+)\.[0-9]+)-[a-f0-9]+$'
    version_template: '{major}.{minor}.{patch}'
changelog:
  mode: command
  command: python3 -B scripts/vendor-release.py
```

`version_regex` must capture the full dotted numeric identity in `version`.
`version_template` maps to a valid application SemVer **base**, not the sorting
key. The full upstream version and tag are retained in the state. When a new
vendor build maps to an application version already used, the allocator bumps
the current LPK patch; it never silently treats the new build as the old release.
Consequently the LPK version can differ from the vendor's first three numbers.
Numeric rollback and a tag/digest changing under the same numeric release stop
the pipeline. Rejected-source suppression and explicit `retry-rejected` still
apply. Registry manifests are fetched in ranked order, not once per old tag.

The adapter's changelog command runs in the project root, including dry-run
checks for new releases, **before** image delivery. It receives
`LAZYCAT_SOURCE_VERSION`, `LAZYCAT_SOURCE_TAG`, `LAZYCAT_SOURCE_REF`,
`LAZYCAT_SOURCE_REVISION`, and `LAZYCAT_APPLICATION_VERSION`; its stdout is the
original-language changelog. Nonzero exit or empty output fails the check.
Store/registry/GitHub credentials are not inherited by this command. Use it to
verify that a vendor API's public version matches the selected image before
using that API's notes. Do not mix this mode with `git_url` or Git auth fields.

Command preparation can use `{source_ref}@{source_revision}` as `output_image`
to return the original, full, immutable reference after adapter upgrade tests;
the existing `{revision}` placeholder still denotes a shortened revision.

An adapter can connect an OCI image to the Git repository that produced its releases:

```yaml
source:
  kind: oci
  image: ghcr.io/example/application
  select:
    strategy: semver-tag
changelog:
  git_url: https://github.com/example/application.git
  max_commits: 20
```

For GitHub projects, use the published release body instead of all commit subjects between versions:

```yaml
changelog:
  mode: github-release
  git_url: https://github.com/example/application.git
```

On an update, the pipeline compares the Git tag for the adapter's current application version with the selected image release tag and uses their commit subjects as review notes. Both `1.2.3` and `v1.2.3` Git tags are recognized; the tags must refer to the release corresponding to the image. A Git branch or tag source can use the same configuration with `git_url` set to its source URL. For a private repository, set `changelog.auth_ref: source` and provide the existing `SOURCE_SSH_KEY`/`SOURCE_KNOWN_HOSTS` or `SOURCE_TOKEN` secrets. If the current version has no matching Git tag, only the target release commit is described. If the target tag is missing, the check fails before copying images or submitting a misleading changelog.

The generated notes appear in dry-run results, GitHub Action outputs, and the saved pipeline lock so retries use the same text. An explicit workflow `changelog` input or CLI `--changelog` overrides automatic generation. Without `changelog.git_url`, the previous generic release text remains the fallback.

Official submission detects the original changelog's language locally and sends it under **one** key: `version.changelogs.zh` for Chinese or `version.changelogs.en` for English. A lightweight zh/en heuristic uses the dominant prose in mixed notes, excluding generated `Upstream` headers, links, code, and technical identifiers. The original text is not translated or rewritten; the app-store server handles missing translations. No translation API or TokenHub key is needed in the Action.

`changelog_locales` is optional legacy configuration, not a list of translation targets. Existing `[zh, en]` recipes still load but no longer duplicate the same text into both languages. When prose cannot be classified (for example, numbers or symbols only), a single `[zh]` or `[en]` value can supply a hint; otherwise submission reports an error before uploading the LPK. Other languages are not automatically identified. Historical reviews are not rewritten.

## CLI

```bash
lazycat-action run --operation check --config lazycat-action.yml --dry-run
lazycat-action run --operation publish-official \
  --config lazycat-action.yml \
  --version 1.2.3 \
  --lpk-path dist/application.lpk \
  --sha256 <sha256> \
  --changelog "Release 1.2.3"
```

`check` without `--dry-run` can copy images and build an LPK. Dry-run skips
preparation, but an adapter changelog command still executes and must be read-only.

GitHub users can call `.github/workflows/lazycat.yml@v1`. Gitee Go and other Linux runners call the same CLI or `scripts/gitee-run.sh`; the wrapper submits a newly packaged LPK to the official store by default.

See the [Chinese documentation](README.zh-CN.md) for the complete configuration examples.
