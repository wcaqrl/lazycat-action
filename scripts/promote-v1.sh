#!/usr/bin/env bash
set -euo pipefail

release_tag="${1:?usage: promote-v1.sh RELEASE_TAG}"
# Fixed prereleases validate workflow → Action → binary without changing the
# version used by existing adapters. Only plain stable v1.x.y releases promote.
if [[ ! "${release_tag}" =~ ^v1\.[0-9]+\.[0-9]+$ ]]; then
  echo "Skipping floating v1 promotion for ${release_tag}"
  exit 0
fi
git -c user.name='github-actions[bot]' \
    -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
    tag --force --annotate --message 'LazyCat GitHub Action v1' v1 "${release_tag}^{commit}"
git push origin refs/tags/v1 --force
