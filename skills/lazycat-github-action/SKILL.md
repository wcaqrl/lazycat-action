---
name: lazycat-github-action
description: Create or update a separate application adapter that monitors GitHub, Gitee, or OCI releases, builds a LazyCat LPK, and submits it to the official store. Use for adapter setup, custom image preparation, multi-image releases, and publication troubleshooting.
---

# LazyCat application adapters

Keep the upstream project independent. Put `version: 2` configuration, LazyCat
metadata, assets and application-specific scripts in the user's adapter
repository. Reuse `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1`;
forking the executor is normally unnecessary. Only the official store is supported.

## Inspect and choose

For an empty adapter, read the [AI guide](references/adapter-guide.md).
The public [quick start](https://github.com/wcaqrl/lazycat-action/blob/main/docs/quickstart.zh-CN.md)
covers human setup; the skill folder itself contains all essential instructions.
Obtain the source URL, adapter location, package ID, app name, target platform,
existing LPK if any, customization requirements and publication scope.
Inspect deployment docs, stable tag conventions, image platforms, ports and
data paths. For existing apps inspect the actual LPK and image metadata before
changing service names, persistence mounts or installation parameters.

Start from the complete [adapter directory](assets/adapter/), including hidden
files. Replace example fields and the placeholder icon. Use the store version
as the baseline for an existing app; `0.0.0` is only for an unpublished app.

| Situation | Source | Preparation |
|---|---|---|
| Git tag with official version images | Git stable tag | `images` |
| Official versioned image is authoritative | OCI stable tags | `passthrough` |
| Several images form one release | Git stable tag | `images`, all bindings |
| Customize a base image | Git or OCI | `dockerfile` or `command` |
| No published image | Git tag, or branch if needed | `dockerfile` or `command` |

Use [recipes](assets/recipes/) and the
[scenario guide](references/scenarios.zh-CN.md) as needed. A Git tag
does not require a published image if the adapter supplies a real source build.
Do not substitute an unrelated `latest` image. For a source without tags,
discover the actual default branch rather than assuming `main` or `master`:

```yaml
source:
  kind: git
  url: https://github.com/example/application.git
  select:
    strategy: branch-head
    branch: auto
```

Read [configuration](references/configuration.md) for private-source `auth_ref`,
staging, numeric versions and preparation outputs. Private Git credentials use
Secrets, not YAML values. A store PAT is not a Git or Docker login.

## Validate and publish

1. Validate YAML and image/service bindings. Inspect the packaged metadata,
   icon, Go templates and deployment parameters. Building an archive does not
   demonstrate that the application runs.
2. Use `lazycat-action run --operation check --config lazycat-action.yml --dry-run`
   for a read-only plan. Without `--dry-run`, `check` can copy images and build
   an LPK. Changelog commands also run during dry-run and should be read-only.
3. Add application-specific startup/persistence tests and, for an existing app,
   a meaningful old-to-new upgrade test. Test the selected digest, then verify
   that packaging delivers the same digest.
4. Put `LZC_API_TOKEN` in the adapter's Actions Secrets and verify ownership or
   collaborator access. For a new app, create its listing first or configure
   `create_if_missing` with the required listing data.
5. Follow the user's authorization for commit, push and real submission.
   The starter defaults manual runs to dry-run; schedules remain read-only
   until the Actions variable `LAZYCAT_AUTO_PUBLISH=true` is configured.

The [workflow reference](references/workflows.md) covers permissions, Secrets
and manual controls. Keep store credentials out of upstream builds. Keep
temporary files, secrets and `.lpk` files out of Git.

## Preserve state and verify outcomes

Commit `.lazycat-action.lock.yml` (the resumable `lazycat.lock` state); never
fabricate it or delete it to force publication. `packaged` can resume an
interrupted submission. History suppresses automatic resubmission of a
processed source, including rejected sources, even if only the recipe changes.
A newer source can replace an older pending review. `retry-rejected` explicitly
rebuilds a repaired rejected source with a new application patch version.

After an authorized real run, verify the review ID/status and state commit.
GitHub Release publication is independent and uploads only the versioned LPK;
its failure must not block submission. Report source/app versions, tested and
official image references, test evidence, run URL and LPK download.
