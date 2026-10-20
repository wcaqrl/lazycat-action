# Workflow reference

The adapter owns the schedule and triggers; the executor supplies the reusable
workflow. Copy the [complete workflow](../assets/adapter/.github/workflows/update.yml)
rather than an unguarded scheduler. The minimum call shape is:

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      config: lazycat-action.yml
      operation: auto
      dry-run: true
    secrets:
      LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
```

Configure repository Secrets `LZC_API_TOKEN` and, when needed, `SOURCE_SSH_KEY`, `SOURCE_KNOWN_HOSTS`, `SOURCE_TOKEN`, registry username, and registry password.

The complete template includes `contents: write`, `pull-requests: write`,
concurrency and explicit forwarding of optional Secrets. Protected branches and
organization policy can still prevent state commits; select an allowed direct
commit or PR strategy rather than disabling protections.

## Starter controls

| Trigger | Default behavior |
|---|---|
| Push of adapter configuration | Dry-run |
| Manual, `dry-run` checked | Dry-run |
| Manual, `dry-run` unchecked | Real copy/build/submission |
| Schedule, no enable variable | Dry-run |
| Schedule, `LAZYCAT_AUTO_PUBLISH=true` Actions variable | Real update when a new source exists |

`LAZYCAT_AUTO_PUBLISH` is an adapter-level template choice, not an executor
setting. Existing workflows keep their behavior. The variable belongs in
Actions **Variables**, not Secrets. `retry-rejected` is a separate, default-off
manual input and requires a real run. Scheduling uses UTC and the default branch;
GitHub may delay execution. The template does not guess the default branch name.

The reusable workflow writes packaged metadata/state, then submits review and
publishes a downloadable GitHub Release as independent jobs. Review success
writes the returned review ID/status to the tracked lock. Release uploads only
the versioned LPK; its failure is non-blocking. Do not add Release as a
prerequisite of submission or delete the state lock to force a retry.

## Toolchains and credentials

For `dockerfile` or Docker-based command preparation, add `toolchains: docker`
under the reusable call's `with`. Other optional toolchains are Go, Node and
Rust; configure their version inputs/files when selecting them. The basic
published-image recipe needs no language compiler.

Set `REGISTRY`, `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` only when pushing a
custom image. Configure target registry visibility so the official copy service
can pull, or select a verified anonymous copy/staging source. A private Git
SSH key and the LazyCat developer PAT do not grant Docker pull/push permission.

## Local CLI debugging

The documented publication path uses GitHub Actions. For local inspection use
`lazycat-action run --operation check --config lazycat-action.yml --dry-run`.
Omitting `--dry-run` can deliver images and build an LPK. Explicit CLI publication
uses `--operation publish-official` with a verified LPK path, version and SHA256;
GitHub's reusable workflow normally handles that step.
