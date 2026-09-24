# Configuration reference

`version: 2` adds an external `source`, resumable `state`, and `build.prepare` phase. Git sources support `branch-head`, `tag`, `release`, and `semver-tag`; `release` uses the matching immutable Git tag. OCI sources select a target-platform digest.

`auth_ref: source` resolves these environment variables without putting credentials in YAML:

- `LAZYCAT_AUTH_SOURCE_SSH_KEY`
- `LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS`
- `LAZYCAT_AUTH_SOURCE_TOKEN`
- `LAZYCAT_AUTH_SOURCE_USERNAME`

`dockerfile` mode injects `SOURCE_IMAGE`, `SOURCE_REF`, `SOURCE_REVISION`, and `SOURCE_VERSION` as build arguments. `command` mode receives the same values as `LAZYCAT_*` environment variables plus `LAZYCAT_SOURCE_DIR`, `LAZYCAT_OUTPUT_IMAGE`, and `LAZYCAT_TARGET_PLATFORM`.

`images` mode coordinates multiple runtime images for one source release. Configure a source template on each image binding. Templates accept `{tag}`, `{source_version}`, `{version}`, and `{revision}`. Delivery completes for the whole set before one Manifest update, and the lock records every source digest and runtime reference.

`delivery.staging_image` adds an immutable intermediate registry reference and also accepts `{id}`. The reusable workflow must set `enable-image-staging: true`; it installs pinned `crane`, verifies that the staged target-platform digest matches the source, and only then calls LazyCat copy-image.

`delivery.copy_source` points at an anonymous proxy and accepts the same placeholders. The target-platform digest must exactly match the original source. It is mutually exclusive with `staging_image`.

Only `stores.official` is valid. Use `create_if_missing`, `skip_if_version_exists`, localized changelogs, screenshots, and retry policy as required by the official developer platform.
