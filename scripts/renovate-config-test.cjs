const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const renovate = path.resolve(process.argv[2]);
const { getConfig } = require(path.join(renovate, 'config/defaults.js'));
const { mergeChildConfig } = require(path.join(renovate, 'config/utils.js'));
const { applyPackageRules } = require(path.join(renovate, 'util/package-rules/index.js'));
const { getDatasourceFor } = require(path.join(renovate, 'modules/datasource/common.js'));
const { lookupUpdates } = require(path.join(renovate, 'workers/repository/process/lookup/index.js'));
const { flattenUpdates } = require(path.join(renovate, 'workers/repository/updates/flatten.js'));
require(path.join(renovate, 'config/global.js')).GlobalConfig.set({ platform: 'github' });

async function main() {
  const repo = path.resolve(__dirname, '..');
  const config = mergeChildConfig(getConfig(), {
    ...JSON.parse(fs.readFileSync(path.join(repo, 'renovate.json'))),
    extends: [],
    semanticCommits: 'disabled',
  });
  getDatasourceFor('go').getReleases = async () => ({
    releases: ['v1.0.0', 'v1.0.1', 'v1.1.0'].map(version => ({
      version,
      releaseTimestamp: '2020-01-01T00:00:00Z',
    })),
  });

  for (const depType of ['require', 'indirect', 'replace']) {
    const dep = {
      depName: `example.com/${depType}`,
      packageName: `example.com/${depType}`,
      depType,
      datasource: 'go',
      currentValue: 'v1.0.0',
    };
    const policy = await applyPackageRules({
      ...config, ...dep, manager: 'gomod', packageFile: 'go.mod',
    });
    const { val, err } = (await lookupUpdates(policy)).unwrap();
    assert.equal(err, undefined);
    assert.deepEqual(val.updates.map(update => update.newVersion).sort(), ['v1.0.1', 'v1.1.0']);
    const branches = await flattenUpdates(config, {
      gomod: [{ packageFile: 'go.mod', deps: [{ ...dep, updates: val.updates }] }],
    });
    const patch = branches.find(branch => branch.updateType === 'patch');
    const minor = branches.find(branch => branch.updateType === 'minor');
    assert.equal(patch.branchName, 'renovate/go-libraries');
    assert.equal(patch.automerge, true);
    assert.equal(minor.automerge, false);
    assert.notEqual(patch.branchName, minor.branchName);
    const libraries = await flattenUpdates(config, {
      gomod: [{
        packageFile: 'go.mod',
        deps: [
          { ...dep, updates: val.updates },
          {
            ...dep,
            depName: 'golang.org/x/net',
            packageName: 'golang.org/x/net',
            updates: [{ updateType: 'minor', newValue: 'v1.1.0', newVersion: 'v1.1.0' }],
          },
        ],
      }],
    });
    assert.deepEqual(
      [...new Set(libraries.filter(branch => branch.automerge).map(branch => branch.branchName))],
      ['renovate/go-libraries'],
    );
    assert.equal(libraries.filter(branch => branch.automerge).length, 2);
  }
  console.log('PASS: available module patches automerge independently of manual minors');
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
