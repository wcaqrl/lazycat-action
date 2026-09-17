# Using LazyCat GitHub Action with Gitea Actions and Forgejo Actions

[简体中文](gitea-forgejo-actions.zh-CN.md)

The composite Action in this repository can run on Gitea Actions and Forgejo Actions. Both platforms support composite actions, fully qualified action URLs, the `github` context, and the `GITHUB_*` environment aliases used by this project.

Use the composite Action directly. The reusable workflow at `.github/workflows/lazycat.yml` contains GitHub-specific pull request, Artifact, Release, and API operations and is not portable as-is.

## Supported operations

| Operation | Gitea Actions | Forgejo Actions | Notes |
|---|---|---|---|
| `check` | Supported | Supported | Updates managed files in the job workspace. Creating a pull request requires platform-specific workflow steps. |
| `build` | Supported | Supported | Produces a validated LPK and SHA256. Artifact upload is left to the calling workflow. |
| `publish-official` | Supported | Supported | Publishes an existing validated LPK to the LazyCat official platform. |
| `publish-private` | Limited | Limited | The current client accepts only a real `https://github.com/.../releases/download/...` asset URL. Native Gitea or Forgejo Release URLs are rejected. |
| `auto` | Supported with caveats | Supported with caveats | The runners expose GitHub-compatible event and ref variables, but explicit operations are easier to audit across platforms. |

## Runner requirements

The job must run on:

- Linux amd64 or arm64.
- A runner image or host with Bash, curl, tar, grep, and sha256sum.
- A runner with Node.js 24 Action runtime support for current official GitHub Actions.
- A workspace checked out before the Action runs.
- A runner that can reach the source registries, LazyCat services, and GitHub Releases when the default bootstrap is used.

The composite Action does not install project toolchains. If the configured `buildscript` needs Go, Node.js, Rust, Docker, or another compiler, install it in the calling workflow or include it in the runner image.

Keep the project configuration at `.github/lazycat-action.yml` if you want to use the default path. The workflow file itself belongs in `.gitea/workflows/` on Gitea or `.forgejo/workflows/` on Forgejo.

## Gitea Actions

Create `.gitea/workflows/lazycat.yml` in the application repository:

```yaml
name: Build LazyCat LPK

on:
  push:
    tags:
      - "v*"

jobs:
  build:
    # Replace this label with a Linux runner label configured on your instance.
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Build LPK
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: build
          config: .github/lazycat-action.yml
          version: ${{ github.ref_name }}

      - name: Show build result
        env:
          LPK_PATH: ${{ steps.lazycat.outputs.lpk-path }}
          LPK_SHA256: ${{ steps.lazycat.outputs.sha256 }}
        run: |
          test -f "${LPK_PATH}"
          echo "LPK: ${LPK_PATH}"
          echo "SHA256: ${LPK_SHA256}"
```

Gitea downloads unqualified actions from the instance's configured default action host. The fully qualified URLs above make the source explicit and work even when that default is set to the Gitea instance itself.

## Forgejo Actions

Create `.forgejo/workflows/lazycat.yml` in the application repository:

```yaml
name: Build LazyCat LPK

on:
  push:
    tags:
      - "v*"

jobs:
  build:
    # Replace this label with a Linux runner label configured on your instance.
    runs-on: docker
    steps:
      - name: Checkout
        uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Build LPK
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: build
          config: .github/lazycat-action.yml
          version: ${{ github.ref_name }}

      - name: Show build result
        env:
          LPK_PATH: ${{ steps.lazycat.outputs.lpk-path }}
          LPK_SHA256: ${{ steps.lazycat.outputs.sha256 }}
        run: |
          test -f "${LPK_PATH}"
          echo "LPK: ${LPK_PATH}"
          echo "SHA256: ${LPK_SHA256}"
```

Forgejo normally resolves unqualified action names against its configured default action host. Use fully qualified URLs when an action must come from GitHub.

## Scheduled image checks

The following job works on either platform after placing it in the platform's workflow directory and selecting a valid Linux runner label:

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Check image versions
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: check
          config: .github/lazycat-action.yml
          dry-run: "false"

      - name: Report changes
        env:
          CHANGED: ${{ steps.lazycat.outputs.changed }}
          VERSION: ${{ steps.lazycat.outputs.version }}
        run: echo "changed=${CHANGED} version=${VERSION}"
```

`check` can update `package.yml` and the configured Manifest inside the workspace. The Action does not commit, push, or open a pull request when called directly. Add Git commands or a platform-native pull request action if you want to persist those changes.

Set `dry-run: "true"` when the workflow should calculate the update without changing workspace files or remote state.

## Publishing to the LazyCat official platform

The Action configuration must enable direct publishing and the official store:

```yaml
update:
  strategy: publish

stores:
  official:
    enabled: true
```

Build the LPK first, then pass the verified path and SHA256 to a second Action step:

```yaml
- name: Build LPK
  id: lazycat-build
  uses: https://github.com/wcaqrl/lazycat-action@v1
  with:
    operation: build
    config: .github/lazycat-action.yml
    version: ${{ github.ref_name }}

- name: Publish to the LazyCat official platform
  id: lazycat-publish
  uses: https://github.com/wcaqrl/lazycat-action@v1
  env:
    LZC_API_HOST: ${{ secrets.LZC_API_HOST }}
    LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
  with:
    operation: publish-official
    config: .github/lazycat-action.yml
    version: ${{ steps.lazycat-build.outputs.version }}
    changelog: Release ${{ steps.lazycat-build.outputs.version }}
    lpk-path: ${{ steps.lazycat-build.outputs.lpk-path }}
    sha256: ${{ steps.lazycat-build.outputs.sha256 }}
```

`LZC_API_TOKEN` is the preferred LazyCat developer-platform PAT and must be stored in the platform's secret manager. Existing automation may instead pass its lzc-cli session token as `LAZYCAT_TOKEN`; the two names select different authentication protocols. `LZC_API_TOKEN` wins when both are set. `LZC_API_HOST` optionally overrides the PAT API production default.

## Bootstrap downloads and private networks

By default, `scripts/run-action.sh` downloads these files from the Action's GitHub Release:

```text
lazycat-action_linux_amd64.tar.gz
lazycat-action_linux_arm64.tar.gz
checksums.txt
```

Fetching the Action repository and downloading its release binary are separate network operations. A fully isolated instance must mirror the Action repository to Gitea or Forgejo and use that repository URL:

```yaml
- uses: https://forge.example.com/ci/lazycat-github-action@v1
```

Mirror `actions/checkout` or replace it with a local checkout step as well.

Mirror the release files and set the base URL on the Action step when the job environment cannot reach GitHub Releases:

```yaml
- uses: https://forge.example.com/ci/lazycat-github-action@v1
  env:
    LAZYCAT_ACTION_RELEASE_BASE_URL: https://downloads.example.com/lazycat-action/v1
  with:
    operation: build
    config: .github/lazycat-action.yml
    version: ${{ github.ref_name }}
```

The download directory must contain the archive for the runner architecture and the original `checksums.txt`. Replace those files when the floating `v1` tag moves to a new release.

A self-hosted runner can also provide a preinstalled binary:

```yaml
env:
  LAZYCAT_ACTION_BINARY: /opt/lazycat/bin/lazycat-action
```

The path must identify an executable regular file and must not be a symbolic link.

## Known limitations

### Do not call the GitHub reusable workflow as-is

The workflow at `.github/workflows/lazycat.yml` uses GitHub-specific actions and APIs for Artifacts, pull requests, Releases, Release Asset digests, and repository content updates. Gitea and Forgejo can parse many GitHub-style workflows, but those remote APIs and action implementations are not interchangeable.

Recreate the orchestration with platform-native steps and keep the LazyCat business logic in direct composite Action calls.

### Native Release Assets cannot be used for private-store publishing

`publish-private` currently validates that `download-url` points to `https://github.com/<owner>/<repo>/releases/download/...`. A Gitea or Forgejo Release Asset URL fails validation even when the file and SHA256 are valid.

The operation can run from a Gitea or Forgejo runner only when the supplied asset is hosted in a real GitHub Release. Supporting native Release URLs requires a code change in this project.

### Artifacts and pull requests are caller responsibilities

Direct Action calls return `lpk-path`, `sha256`, changed file paths, and the other outputs listed in the main README. Use the target platform's artifact, release, repository, and pull request APIs for subsequent operations.

## Troubleshooting

- `lazycat-action supports Linux runners only`: select a Linux runner.
- `unsupported runner architecture`: use amd64 or arm64.
- A download fails before the binary starts: allow access to GitHub Releases or configure `LAZYCAT_ACTION_RELEASE_BASE_URL` or `LAZYCAT_ACTION_BINARY`.
- The configuration file is missing: check out the repository first and confirm the `config` path.
- A project buildscript cannot find Go, Node.js, Rust, or Docker: install the required toolchain before the Action step.
- An automatic operation selects an unexpected path: set `operation` and `version` explicitly.
- Step outputs are empty on an old runner: update the Gitea act runner or Forgejo Runner so its `GITHUB_OUTPUT` compatibility alias is available.
- A private-store publish rejects the download URL: use a GitHub Release Asset URL or disable that publication target.

## Platform references

- [Gitea Actions overview](https://docs.gitea.com/usage/actions/overview)
- [Gitea differences from GitHub Actions](https://docs.gitea.com/usage/actions/comparison)
- [Gitea Actions variables](https://docs.gitea.com/usage/actions/actions-variables)
- [Forgejo Actions reference](https://forgejo.org/docs/latest/user/actions/)
