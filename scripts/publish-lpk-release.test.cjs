const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const publish = require('./publish-lpk-release.cjs');

async function setup(t, options = {}) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'lazycat-release-test-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const data = Buffer.from('validated-lpk');
  const env = { APPLICATION_VERSION: '1.4.2', PACKAGE_ID: 'dev.example.app', PACKAGED_COMMIT: 'a'.repeat(40), EXPECTED_SHA256: crypto.createHash('sha256').update(data).digest('hex'), LPK_PATH: path.join(directory, 'dev.example.app-v1.4.2.lpk'), RELEASE_CHANGELOG: 'Only this release' };
  await fs.writeFile(env.LPK_PATH, data);
  const calls = [];
  const missing = async () => { throw Object.assign(new Error('not found'), { status: 404 }); };
  const release = { id: 9, html_url: 'https://github.com/example/adapter/releases/tag/v1.4.2' };
  const github = {
    rest: {
      git: {
        getRef: options.ref ? async () => ({ data: options.ref }) : missing,
        createRef: async (request) => { calls.push(['tag', request]); return { data: {} }; },
      },
      repos: {
        getReleaseByTag: options.existing ? async () => ({ data: release }) : missing,
        createRelease: async (request) => { calls.push(['release', request]); return { data: release }; },
        listReleaseAssets: async () => ({ data: options.assets || [] }),
        uploadReleaseAsset: async (request) => { calls.push(['asset', request]); return { data: {} }; },
      },
    },
    paginate: async (method, args) => (await method(args)).data,
  };
  const core = { setOutput: () => {}, summary: { addRaw: () => ({ write: async () => {} }) } };
  return { args: { github, context: { repo: { owner: 'example', repo: 'adapter' } }, core }, env, calls };
}

test('creates a tag and Release with exactly one versioned LPK', async (t) => {
  const { args, env, calls } = await setup(t);
  await publish(args, env);
  assert.deepEqual(calls.map(call => call[0]), ['tag', 'release', 'asset']);
  assert.equal(calls[0][1].sha, env.PACKAGED_COMMIT);
  assert.equal(calls[1][1].body, env.RELEASE_CHANGELOG);
  assert.equal(calls[2][1].name, 'dev.example.app-v1.4.2.lpk');
});
test('existing tag and identical asset are idempotent', async (t) => {
  const sample = crypto.createHash('sha256').update('validated-lpk').digest('hex');
  const { args, env, calls } = await setup(t, { ref: { object: { type: 'commit', sha: 'a'.repeat(40) } }, existing: true, assets: [{ name: 'dev.example.app-v1.4.2.lpk', digest: `sha256:${sample}` }] });
  await publish(args, env);
  assert.equal(calls.length, 0);
});
test('never moves an existing tag to a different commit', async (t) => {
  const { args, env, calls } = await setup(t, { ref: { object: { type: 'commit', sha: 'b'.repeat(40) } } });
  await assert.rejects(publish(args, env), /refusing to move/);
  assert.equal(calls.length, 0);
});
test('never replaces a conflicting LPK asset', async (t) => {
  const { args, env, calls } = await setup(t, { ref: { object: { type: 'commit', sha: 'a'.repeat(40) } }, existing: true, assets: [{ name: 'dev.example.app-v1.4.2.lpk', digest: 'sha256:wrong' }] });
  await assert.rejects(publish(args, env), /different digest/);
  assert.equal(calls.length, 0);
});
test('corrupt artifact fails before any GitHub write', async (t) => {
  const { args, env, calls } = await setup(t);
  await fs.writeFile(env.LPK_PATH, 'corrupt');
  await assert.rejects(publish(args, env), /SHA256/);
  assert.equal(calls.length, 0);
});
