# AI adapter implementation guide

This guide covers GitHub upstream repositories and separate GitHub adapter
repositories executed by GitHub Actions, not changes to the upstream project
or the generic executor. Official OCI images may use GHCR or Docker Hub. Use the
[complete starter](../assets/adapter/) and read only the relevant
[scenarios](scenarios.zh-CN.md), [configuration](configuration.md) and
[workflow](workflows.md) references. These paths stay within the skill package.

## Inputs and inspection

Resolve the upstream URL, adapter repository/path, package ID, app name,
target architecture, existing store LPK, customization needs and publication
scope. Inspect available files before asking for missing information. Credentials
are supplied as Secrets; their values do not belong in generated artifacts.

Inspect the upstream's own deployment docs and release mechanism. Record:

- Stable version pattern, exact Git tag prefix and prerelease exclusions.
- Official images, target-platform availability, and exact Git-to-image mapping.
- Ports, environment variables, command/entrypoint, service dependencies,
  persistent directories, privileges and external authentication requirements.
- A version-specific changelog source. GitHub Release text, Git commit subjects
  and a read-only vendor command are different modes; choose deliberately.

For an existing store app inspect its LPK, installation parameters, metadata,
service names, domain routes and binds. Inspect image labels/digests to identify
the actual baseline version. Preserve these contracts during upgrades. Do not
assume the LPK version always equals the embedded upstream version.

## Choose the source and preparation mode

Prefer stable Git tags when they define releases. Use OCI version tags if the
official image release is the authoritative source. Branch monitoring is an
explicit fallback, with `branch: auto` resolving remote HEAD. A new Git tag
without an image is supported only when the adapter supplies a working build;
it is not permission to package an unrelated latest image.

Start with the Git-tag/images starter. Replace its example IDs, URLs, icon,
image templates, port and persistence mount. For another mode, replace the
whole recipe with a file from [recipes](../assets/recipes/), then adapt it.
The multi-image recipe also supplies its Manifest. The custom-image recipe
needs the included image directory copied to `image/`. A source build needs
an actual upstream Dockerfile or an application-specific adapter build script.

The starter version `0.0.0` is for an unpublished app. Existing apps use the
current store version and original runtime references. Application versions
can diverge from source versions; retain the full source identity in state.
Confirm whether image tags use `{tag}` or `{source_version}` rather than guessing.

Keep the package's public URL based on `{{ .S.AppDomain }}` where appropriate.
Do not add invented environment variables to force an upstream app to use it.
Login, OIDC/injects, public API paths and unsupported launcher platforms are
application-specific decisions to verify against current official documentation.

## Wire the Runner

The adapter owns scheduling and manual inputs. It calls the public reusable
workflow at `@v1`; it does not need to fork or run a permanent polling service.
Copy hidden files from the starter too. Keep the dry-run and retry controls,
write permissions, concurrency and state handling unless adapting them to an
explicit repository policy. Discover the GitHub repository's actual default
branch rather than assuming it is main.

For a new application either arrange a listing in the developer platform or
configure `create_if_missing: true` and actual listing information/screenshots.
For an existing app keep it false and verify developer collaborator access.
Do not request Docker login using the store PAT. Image-copy sources must be
readable by the server; configure a verified proxy or staging registry if needed.
Use the returned official image address, not a hardcoded personal namespace.

## Verification gates

1. **Local configuration:** load recipes with the current executor schema;
   validate image bindings against Manifest services, declared output paths,
   package IDs and YAML. Preserve Go template expressions. Check actual PNG
   content and explicitly included deployment parameters, not just filenames.
2. **Read-only discovery:** use `--operation check --dry-run`. Confirm source
   revision, version mapping and changelog. `check` without `--dry-run` can copy
   images/build an LPK. A changelog command runs even in dry-run: keep it read-only.
   Dry-run skips preparation and does not prove image availability or app health.
3. **Application behavior:** test startup and a meaningful page/API plus
   persistence across recreation. For existing apps test the old official
   baseline to the selected new release using disposable representative data.
   Include migrations, user login, stored content and relevant attachments or
   settings. Container health or arbitrary marker files alone may be inadequate.
4. **Immutable delivery:** bind tests to the selected candidate and platform
   digest. A separate independently discovered latest image can race with
   packaging. Use pinned OCI command preparation or an adapter guard comparing
   the tested digest with the final delivered source digest before packaging.
5. **Real publication, when authorized:** verify completed image copying,
   extracted LPK content, actual official review response/history and state commit.
   Verify the downloadable Release asset and its hash separately. A green build
   is not evidence of review submission; submission is not approval.

Use normal Runner isolation and disposable test data. Do not install into the
user's microserver or run tests on production data unless that is requested.
If a gate fails, fix the cause and repeat the affected checks before proceeding.

## State and repair rules

Keep `.lazycat-action.lock.yml` tracked. The executor records source identity,
application version, runtime image set, LPK hash, packaging time and review
history. Do not manufacture a lock or mark an unsubmitted version submitted.

A previously processed source is suppressed even if only the build recipe
changes. For a rejected source, an explicitly authorized `retry-rejected`
allocates a new application patch. New upstream releases can replace an older
pending review. Preserve old review history and never delete a lock to retry.

GitHub Release and official submission are independent parallel jobs after
packaging. Upload only the versioned LPK, not a checksum asset. GitHub's automatic
source archives are not uploaded by this workflow. Release failure cannot block
submission and conflicting existing assets must not be overwritten.

## Handoff

The starter schedules checks only until the developer sets the Actions variable
`LAZYCAT_AUTO_PUBLISH=true`. Explain that this is a starter workflow control,
not a new executor requirement and not an automatic change to existing adapters.
Respect any user instruction that already authorizes actual publication; do not
invent a new approval step. Commit, push, install and submit only within that scope.

Report what files changed, required Secrets/variables and app permissions,
selected source/application versions, tested and official image references,
test evidence and limitations. For an authorized real run include the run URL,
review ID/status, state commit and LPK download. Distinguish generated artifacts,
tests passed and remote actions completed; leave missing evidence explicit.
