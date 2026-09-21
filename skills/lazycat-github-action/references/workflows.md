# Workflow reference

GitHub callers use:

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      config: lazycat-action.yml
      operation: auto
      toolchains: docker
    secrets: inherit
```

Configure repository Secrets `LZC_API_TOKEN` and, when needed, `SOURCE_SSH_KEY`, `SOURCE_KNOWN_HOSTS`, `SOURCE_TOKEN`, registry username, and registry password.

Gitee Go calls `scripts/gitee-run.sh` or the platform-neutral `lazycat-action run` command. The source provider and Runner provider are independent: a GitHub Runner may read Gitee, and a Gitee Runner may read GitHub.
