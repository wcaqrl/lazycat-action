# Application LazyCat adapter

Replace the example fields and this README with the application's actual setup.
This repository owns LazyCat configuration; upstream source stays independent.

- Pipeline: stable Git tags → matching official images → LazyCat image copy →
  LPK → independent official review submission and GitHub Release publication.
- Configure `LZC_API_TOKEN` in this repository's Actions Secrets. The developer
  must own the app or have collaborator access. Create the app listing first,
  or explicitly configure automatic creation and the required listing data.
- Actions needs Contents write access; protected branches must allow the
  selected state-update strategy. The workflow calls `lazycat-action@v1`.
- Pushes and manual runs with `dry-run` checked do not publish. Scheduled runs
  also check only until the Actions **variable** `LAZYCAT_AUTO_PUBLISH` is `true`.
  Enable it after application tests and the first authorized real run.
- `.lazycat-action.lock.yml` is generated and committed by the pipeline. Keep
  it tracked; do not copy another app's lock or fabricate submission history.
- Release assets contain the versioned `.lpk` only. A Release failure does not
  block review submission. Rejected sources are not automatically resubmitted;
  repair the adapter and explicitly use `retry-rejected` when appropriate.

Before release replace the generic PNG, package ID, URLs, image templates,
license, port, service names and persistence mounts. The starter has no
application-specific upgrade test: add and validate one for the actual app.
Do not install a package that still contains the example image or icon.

Documentation: [quick start](https://github.com/wcaqrl/lazycat-action/blob/main/docs/quickstart.zh-CN.md),
[AI guide](https://github.com/wcaqrl/lazycat-action/blob/main/docs/ai-adapter-guide.md).
