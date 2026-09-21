---
name: lazycat-github-action
description: Configure a repository-external Git or OCI source pipeline that builds and submits an LPK to the LazyCat official application store.
---

# LazyCat official source pipelines

Use this skill when a project needs to monitor a GitHub, Gitee, or OCI source and publish a LazyCat LPK. The source project remains independent; put the LazyCat files and `version: 2` pipeline recipe in a separate adapter project.

Only the LazyCat official application store is supported. Do not generate private-store configuration, credentials, operations, or Release URL plumbing.

## Required inspection

1. Inspect `package.yml`, `lzc-build.yml`, `lzc-manifest.yml`, Dockerfiles, build scripts, and `.gitignore`.
2. Identify one authoritative source: OCI digest, stable Git tag, or branch commit SHA.
3. Use `branch: auto` when the remote default branch should be discovered. Never hardcode GitHub `main` or Gitee `master` without repository evidence.
4. Keep private source credentials behind `auth_ref`. The YAML must never contain an SSH private key or Token.
5. Use `LZC_API_TOKEN` only for LazyCat image copying and official review submission.

## Pipeline configuration

Start from [assets/lazycat-action.yml](assets/lazycat-action.yml). Choose one preparation mode:

- `passthrough`: copy an OCI source image unchanged.
- `dockerfile`: checkout an exact Git revision or pin an OCI digest, build and push a custom image.
- `command`: run a trusted adapter script that may build content or an image.

The update fingerprint combines the immutable source revision with the Dockerfile, command, build arguments, and configured build context. Persist it in `.lazycat-action.lock.yml` (`lazycat.lock` state). A matching `submitted` state is a no-op. A `packaged` state is resumable after an interrupted official submission.

For private Git sources use a logical reference such as:

```yaml
source:
  kind: git
  url: git@gitee.com:example/private-app.git
  auth_ref: source
  select:
    strategy: branch-head
    branch: auto
```

GitHub maps its Secret to `LAZYCAT_AUTH_SOURCE_SSH_KEY`; Gitee does the same in its encrypted pipeline variables. Also supply verified known-host content through `LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS` when the Runner has no trusted system entry.

## Official publication

Use the reusable GitHub workflow in [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml), or run the Linux CLI on Gitee:

```bash
lazycat-action run --operation check --config lazycat-action.yml
lazycat-action run --operation publish-official \
  --config lazycat-action.yml \
  --version 1.2.3 \
  --lpk-path dist/app.lpk \
  --sha256 <sha256> \
  --changelog "Release 1.2.3"
```

The official step must use the locally validated LPK and SHA256. Check the current online version and pending review before creating another submission. Never expose the developer PAT to an upstream Dockerfile, source buildscript, or pull-request job.

See [references/configuration.md](references/configuration.md) and [references/workflows.md](references/workflows.md).
