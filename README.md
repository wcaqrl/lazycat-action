# lazycat-action

[中文](README.zh-CN.md)

`lazycat-action` is a platform-neutral pipeline for the LazyCat official application store. It discovers GitHub, Gitee, or OCI revisions, optionally builds a customized image, copies runtime images through the official developer API, builds and validates an LPK, and submits it for official review.

Source repositories remain unaware of LazyCat packaging. Keep `package.yml`, `lzc-build.yml`, `lzc-manifest.yml`, Dockerfiles, assets, and the version 2 pipeline recipe in a separate application adapter repository.

Only the LazyCat official application store is supported.

For a concrete three-repository setup, see [poster-adapter](https://github.com/wcaqrl/poster-adapter): Poster publishes tagged GHCR images, its adapter owns the LazyCat metadata and daily schedule, and this repository supplies the reusable workflow. Add the developer PAT as `LZC_API_TOKEN` in the adapter's Actions secrets and grant its workflow write access so it can record submitted versions. Test a manual dry run before publishing; the adapter's README describes the full setup.

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

Preparation modes are `passthrough`, `dockerfile`, and `command`. A Dockerfile build receives the pinned `SOURCE_IMAGE`, `SOURCE_REF`, `SOURCE_REVISION`, and `SOURCE_VERSION`. A command build also receives `LAZYCAT_SOURCE_DIR`, `LAZYCAT_OUTPUT_IMAGE`, and the target platform.

Use `build.prepare.mode: images` for a Git release that coordinates multiple runtime images. Each `images[].source` may use `{tag}`, `{source_version}`, `{version}`, or `{revision}`. All images are pinned and delivered before the Manifest is updated as one change; build and state failures restore the previous image set and package version.

Set `images[].delivery.staging_image` when the official copy service cannot reliably read the upstream registry. The Action uses `crane` to mirror the immutable platform image, verifies the staged digest, and copies from that reference. Enable `enable-image-staging` on the reusable workflow.

Use `images[].delivery.copy_source` when an anonymous registry proxy already serves the image. The Action verifies that its target-platform digest exactly matches the original source before copying. `copy_source` and `staging_image` are mutually exclusive.

Private source credentials are referenced by `auth_ref` and supplied through environment variables such as `LAZYCAT_AUTH_SOURCE_SSH_KEY`; they never belong in YAML.

## Upstream changelog for official review

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

The generated notes appear in dry-run results, GitHub Action outputs, and the saved pipeline lock so retries use the same text. The workflow passes them to the official store for every configured `changelog_locales`; it does not translate them. An explicit workflow `changelog` input or CLI `--changelog` overrides automatic generation. Without `changelog.git_url`, the previous generic release text remains the fallback.

## CLI

```bash
lazycat-action run --operation check --config lazycat-action.yml
lazycat-action run --operation publish-official \
  --config lazycat-action.yml \
  --version 1.2.3 \
  --lpk-path dist/application.lpk \
  --sha256 <sha256> \
  --changelog "Release 1.2.3"
```

GitHub users can call `.github/workflows/lazycat.yml@v1`. Gitee Go and other Linux runners call the same CLI or `scripts/gitee-run.sh`; the wrapper submits a newly packaged LPK to the official store by default.

See the [Chinese documentation](README.zh-CN.md) for the complete configuration examples.
