const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');

module.exports = async function publish({ github, context, core }, env = process.env) {
  const { owner, repo } = context.repo;
  const version = env.APPLICATION_VERSION;
  const packageID = env.PACKAGE_ID;
  const commit = env.PACKAGED_COMMIT;
  const expected = env.EXPECTED_SHA256;
  if (!/^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$/.test(version || '') ||
      !/^[a-zA-Z0-9._-]+$/.test(packageID || '') || !/^[a-f0-9]{40}$/.test(commit || '') || !/^[a-f0-9]{64}$/.test(expected || '')) {
    throw new Error('Invalid release package, version, commit or SHA256');
  }
  const tag = `v${version}`;
  const name = `${packageID}-${tag}.lpk`;
  const data = await fs.readFile(env.LPK_PATH);
  if (path.basename(env.LPK_PATH) !== name || crypto.createHash('sha256').update(data).digest('hex') !== expected) {
    throw new Error('LPK filename or SHA256 differs from the validated package');
  }
  const optional = async (operation) => {
    try { return (await operation()).data; } catch (error) { if (error.status === 404) return null; throw error; }
  };
  const ref = await optional(() => github.rest.git.getRef({ owner, repo, ref: `tags/${tag}` }));
  if (ref) {
    let object = ref.object;
    for (let depth = 0; object.type === 'tag' && depth < 10; depth++) {
      object = (await github.rest.git.getTag({ owner, repo, tag_sha: object.sha })).data.object;
    }
    if (object.type !== 'commit' || object.sha !== commit) throw new Error(`Existing ${tag} targets another commit; refusing to move it`);
  } else {
    await github.rest.git.createRef({ owner, repo, ref: `refs/tags/${tag}`, sha: commit });
  }
  let release = await optional(() => github.rest.repos.getReleaseByTag({ owner, repo, tag }));
  if (!release) {
    release = (await github.rest.repos.createRelease({ owner, repo, tag_name: tag, target_commitish: commit, name: `${packageID} ${tag}`,
      body: env.RELEASE_CHANGELOG || `LazyCat LPK ${tag}. Store review is tracked separately.`, draft: false, prerelease: version.includes('-') })).data;
  }
  const assets = await github.paginate(github.rest.repos.listReleaseAssets, { owner, repo, release_id: release.id, per_page: 100 });
  const existing = assets.find(asset => asset.name === name);
  if (existing) {
    // Verify bytes even when older GitHub API responses omit the digest.
    if (existing.digest && existing.digest !== `sha256:${expected}`) throw new Error('Existing LPK asset has a different digest');
    if (!existing.digest) {
      const downloaded = await github.rest.repos.getReleaseAsset({ owner, repo, asset_id: existing.id, headers: { accept: 'application/octet-stream' } });
      if (crypto.createHash('sha256').update(Buffer.from(downloaded.data)).digest('hex') !== expected) throw new Error('Existing LPK asset has different content');
    }
  } else {
    await github.rest.repos.uploadReleaseAsset({ owner, repo, release_id: release.id, name, data, headers: { 'content-type': 'application/octet-stream', 'content-length': data.length } });
  }
  core.setOutput('release-url', release.html_url);
  await core.summary.addRaw(`LPK download: ${release.html_url}\n`).write();
};
