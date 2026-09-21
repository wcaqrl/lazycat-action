# lazycat-action

[中文](README.zh-CN.md)

`lazycat-action` is a platform-neutral pipeline for the LazyCat official application store. It discovers GitHub, Gitee, or OCI revisions, optionally builds a customized image, copies runtime images through the official developer API, builds and validates an LPK, and submits it for official review.

Source repositories remain unaware of LazyCat packaging. Keep `package.yml`, `lzc-build.yml`, `lzc-manifest.yml`, Dockerfiles, assets, and the version 2 pipeline recipe in a separate application adapter repository.

Only the LazyCat official application store is supported.

For a concrete three-repository setup, see [poster-adapter](https://github.com/wcaqrl/poster-adapter): Poster publishes tagged GHCR images, its adapter owns the LazyCat metadata and daily schedule, and this repository supplies the reusable workflow. Add the developer PAT as `LZC_API_TOKEN` in the adapter's Actions secrets and grant its workflow write access so it can record submitted versions. Test a manual dry run before publishing; the adapter's README describes the full setup.

## Source identities

- OCI sources compare the selected platform image digest.
- Git tags compare the immutable commit behind the highest matching stable SemVer tag.
- Branch sources compare the branch HEAD commit. `branch: auto` resolves the remote symbolic `HEAD` instead of assuming `main` or `master`.
- The final fingerprint also includes the preparation command, Dockerfile, build arguments, and build-context contents.

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

Private source credentials are referenced by `auth_ref` and supplied through environment variables such as `LAZYCAT_AUTH_SOURCE_SSH_KEY`; they never belong in YAML.

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
