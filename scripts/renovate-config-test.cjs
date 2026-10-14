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
  const actionBranches = await flattenUpdates(config, {
    'github-actions': [{
      packageFile: '.github/workflows/ci.yml',
      deps: [{
        depName: 'actions/checkout',
        packageName: 'actions/checkout',
        depType: 'action',
        datasource: 'github-tags',
        currentValue: 'v7.0.0',
        updates: [
          { updateType: 'patch', newValue: 'v7.0.1', newVersion: 'v7.0.1' },
          { updateType: 'minor', newValue: 'v7.1.0', newVersion: 'v7.1.0' },
          { updateType: 'digest', newValue: 'v7.0.0', newDigest: 'a'.repeat(40) },
          { updateType: 'pin', newValue: 'v7.0.0', newVersion: 'v7.0.0' },
          { updateType: 'pinDigest', newValue: 'v7.0.0', newDigest: 'a'.repeat(40) },
          { updateType: 'major', newValue: 'v8.0.0', newVersion: 'v8.0.0' },
        ],
      }],
    }],
    gomod: [{
      packageFile: 'go.mod',
      deps: [{
        depName: 'example.com/library',
        packageName: 'example.com/library',
        depType: 'require',
        datasource: 'go',
        currentValue: 'v1.0.0',
        updates: [{ updateType: 'patch', newValue: 'v1.0.1', newVersion: 'v1.0.1' }],
      }],
    }],
  });
  for (const action of actionBranches.filter(branch => branch.manager === 'github-actions')) {
    assert.equal(action.automerge, action.updateType !== 'major');
    assert.equal(action.branchName, action.updateType === 'major'
      ? 'renovate/major-github-actions' : 'renovate/github-actions');
    if (action.automerge) {
      assert.equal(action.platformAutomerge, true);
      assert.equal(action.automergeType, 'pr');
    }
  }
  assert.equal(actionBranches.filter(branch => branch.branchName === 'renovate/github-actions').length, 5);
  assert.equal(actionBranches.find(branch => branch.manager === 'gomod').branchName, 'renovate/go-libraries');
  for (const depType of ['github-runner', 'docker', 'uses-with']) {
    const policy = await applyPackageRules({
      ...config, manager: 'github-actions', depType, updateType: 'patch',
    });
    assert.equal(policy.automerge, false);
  }
  console.log('PASS: available module patches automerge independently of manual minors');
  console.log('PASS: compatible GitHub Actions updates automerge separately; major actions stay manual');
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
