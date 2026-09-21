# Configuration reference

`version: 2` adds an external `source`, resumable `state`, and `build.prepare` phase. Git sources support `branch-head`, `tag`, `release`, and `semver-tag`; `release` uses the matching immutable Git tag. OCI sources select a target-platform digest.

`auth_ref: source` resolves these environment variables without putting credentials in YAML:

- `LAZYCAT_AUTH_SOURCE_SSH_KEY`
- `LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS`
- `LAZYCAT_AUTH_SOURCE_TOKEN`
- `LAZYCAT_AUTH_SOURCE_USERNAME`

`dockerfile` mode injects `SOURCE_IMAGE`, `SOURCE_REF`, `SOURCE_REVISION`, and `SOURCE_VERSION` as build arguments. `command` mode receives the same values as `LAZYCAT_*` environment variables plus `LAZYCAT_SOURCE_DIR`, `LAZYCAT_OUTPUT_IMAGE`, and `LAZYCAT_TARGET_PLATFORM`.

Only `stores.official` is valid. Use `create_if_missing`, `skip_if_version_exists`, localized changelogs, screenshots, and retry policy as required by the official developer platform.
