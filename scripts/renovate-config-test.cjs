const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const renovate = path.resolve(process.argv[2]);
const { getConfig } = require(path.join(renovate, 'config/defaults.js'));
const { mergeChildConfig } = require(path.join(renovate, 'config/utils.js'));
const { resolveConfigPresets } = require(path.join(renovate, 'config/presets/index.js'));
const { applyPackageRules } = require(path.join(renovate, 'util/package-rules/index.js'));
const { getDatasourceFor, getDefaultVersioning } = require(path.join(renovate, 'modules/datasource/common.js'));
const { get: getVersioning } = require(path.join(renovate, 'modules/versioning/index.js'));
const { lookupUpdates } = require(path.join(renovate, 'workers/repository/process/lookup/index.js'));
const { filterInternalChecks } = require(path.join(renovate, 'workers/repository/process/lookup/filter-checks.js'));
const { flattenUpdates } = require(path.join(renovate, 'workers/repository/updates/flatten.js'));
const { Vulnerabilities } = require(path.join(renovate, 'workers/repository/process/vulnerabilities.js'));
const actions = require(path.join(renovate, 'modules/manager/github-actions/index.js'));
const regex = require(path.join(renovate, 'modules/manager/custom/regex/index.js'));
require(path.join(renovate, 'config/global.js')).GlobalConfig.set({ platform: 'github' });

async function main() {
  const repo = path.resolve(__dirname, '..');
  const { config: recommended } = await resolveConfigPresets({ extends: ['config:recommended'] });
  assert(recommended.packageRules?.length, 'config:recommended must include package rules');
  let config = mergeChildConfig(getConfig(), recommended);
  assert(process.argv[3], 'Pass the shared renovate-config checkout as the third argument');
  for (const preset of ['default', 'go']) {
    const shared = JSON.parse(fs.readFileSync(path.join(process.argv[3], `${preset}.json`)));
    config = mergeChildConfig(config, { ...shared, extends: [] });
  }
  config = mergeChildConfig(config, {
    ...JSON.parse(fs.readFileSync(path.join(repo, 'renovate.json'))),
    extends: [],
    semanticCommits: 'disabled',
  });
  assert.equal(config.prConcurrentLimit, 10);
  assert.equal(config.prHourlyLimit, 5);
  assert.equal(config.osvVulnerabilityAlerts, true);
  assert.equal(config.minimumReleaseAge, '7 days');
  assert.equal(config.internalChecksFilter, 'strict');
  const freshReleaseTimestamp = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
  const matureReleaseTimestamp = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000).toISOString();
  for (const [name, manager, depType, datasource, currentVersion, newVersion] of [
    ['github.com/stretchr/testify', 'gomod', 'require', 'go', 'v1.0.0', 'v1.0.1'],
    ['actions/checkout', 'github-actions', 'action', 'github-tags', 'v1.0.0', 'v1.0.1'],
    ['go', 'gomod', 'golang', 'golang-version', '1.26.8', '1.26.9'],
    ['goreleaser/goreleaser', 'custom.regex', undefined, 'github-releases', 'v1.25.1', 'v1.25.2'],
  ]) {
    const policy = await applyPackageRules({
      ...config, depName: name, packageName: name, manager, depType, datasource,
      currentVersion, currentValue: currentVersion, updateType: 'patch',
    });
    const immediate = name === 'go' || name === 'goreleaser/goreleaser';
    const versioning = getVersioning(policy.versioning ?? getDefaultVersioning(datasource));
    for (const releaseTimestamp of [freshReleaseTimestamp, matureReleaseTimestamp, undefined]) {
      const result = await filterInternalChecks(policy, versioning, 'patch', [{ version: newVersion, releaseTimestamp }]);
      assert.equal(result.pendingChecks, !immediate && releaseTimestamp !== matureReleaseTimestamp, name);
    }
    for (const security of [{ isVulnerabilityAlert: true }, { vulnerabilityFixVersion: newVersion }]) {
      const securityPolicy = await applyPackageRules({ ...policy, ...security });
      assert.equal(securityPolicy.minimumReleaseAge, null, name);
      const result = await filterInternalChecks(securityPolicy, versioning, 'patch', [
        { version: newVersion, releaseTimestamp: freshReleaseTimestamp },
      ]);
      assert.equal(result.pendingChecks, false, name);
    }
    const routinePolicy = await applyPackageRules({ ...policy, isVulnerabilityAlert: false });
    assert.equal(routinePolicy.minimumReleaseAge, immediate ? null : '7 days', name);
  }
  getDatasourceFor('go').getReleases = async () => ({
    releases: [
      ['v1.0.0', '1.18.0'],
      ['v1.0.1', '1.25.0'],
      ['v1.1.0', '1.26.7'],
      ['v1.2.0', '1.27.0'],
    ].map(([version, go]) => ({
      version,
      constraints: { '%goMod': [go] },
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
    assert.equal(patch.branchName, 'renovate/patch-go-libraries');
    assert.equal(patch.automerge, true);
    assert.equal(patch.rebaseWhen, 'behind-base-branch');
    assert.equal(minor.automerge, true);
    assert.equal(minor.branchName, 'renovate/go-libraries');
    assert.equal(minor.platformAutomerge, true);
    assert.equal(minor.rebaseWhen, 'behind-base-branch');
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
      ['renovate/patch-go-libraries', 'renovate/go-libraries'],
    );
    assert.equal(libraries.filter(branch => branch.automerge).length, 3);
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
          { updateType: 'major', newValue: 'v8.0.0', newVersion: 'v8.0.0', newMajor: 8 },
        ],
      }, {
        depName: 'actions/setup-go', packageName: 'actions/setup-go',
        depType: 'action', datasource: 'github-tags', currentValue: 'v6.0.0',
        updates: [{ updateType: 'major', newValue: 'v7.0.0', newVersion: 'v7.0.0', newMajor: 7 }],
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
      ? `renovate/${action.depName.replace('/', '-')}-${action.newMajor}.x` : 'renovate/github-actions');
    assert.equal(action.rebaseWhen, action.updateType === 'major'
      ? 'conflicted' : 'behind-base-branch');
    if (action.automerge) {
      assert.equal(action.platformAutomerge, true);
      assert.equal(action.automergeType, 'pr');
    }
  }
  assert.equal(actionBranches.filter(branch => branch.branchName === 'renovate/github-actions').length, 5);
  assert.equal(actionBranches.find(branch => branch.manager === 'gomod').branchName, 'renovate/patch-go-libraries');
  for (const depType of ['github-runner', 'docker', 'uses-with']) {
    const policy = await applyPackageRules({
      ...config, manager: 'github-actions', depType, updateType: 'patch',
    });
    assert.equal(policy.automerge, false);
  }
  const modules = [
    ['github.com/aws/aws-sdk-go-v2', 'minor', 'aws-go-modules'],
    ['github.com/aws/aws-sdk-go-v2/config', 'minor', 'aws-go-modules'],
    ['github.com/aws/smithy-go', 'minor', 'aws-go-modules'],
    ['github.com/aws/smithy-go', 'patch', 'aws-go-modules'],
    ['k8s.io/api', 'minor', 'kubernetes-go-modules'],
    ['k8s.io/apimachinery', 'minor', 'kubernetes-go-modules'],
    ['k8s.io/client-go', 'minor', 'kubernetes-go-modules'],
    ['k8s.io/kops', 'minor', 'kubernetes-go-modules'],
    ['sigs.k8s.io/controller-runtime', 'minor', 'kubernetes-go-modules'],
    ['sigs.k8s.io/structured-merge-diff/v6', 'minor', 'kubernetes-go-modules'],
    ['k8s.io/api', 'patch', 'kubernetes-go-modules'],
    ['sigs.k8s.io/controller-runtime', 'patch', 'kubernetes-go-modules'],
    ['github.com/ClickHouse/clickhouse-go/v2', 'minor', 'go-libraries'],
    ['github.com/ClickHouse/ch-go', 'minor', 'go-libraries'],
    ['github.com/elastic/go-elasticsearch/v8', 'minor', 'go-libraries'],
    ['github.com/elastic/elastic-transport-go/v8', 'minor', 'go-libraries'],
    ['go.opentelemetry.io/otel', 'minor', 'go-libraries'],
    ['go.opentelemetry.io/otel/trace', 'minor', 'go-libraries'],
    ['github.com/go-openapi/jsonpointer', 'minor', 'go-libraries'],
    ['github.com/go-openapi/swag', 'minor', 'go-libraries'],
    ['golang.org/x/net', 'minor', 'go-libraries'],
    ['golang.org/x/sys', 'minor', 'go-libraries'],
    ['github.com/aws/smithy-go', 'major', null],
    ['sigs.k8s.io/structured-merge-diff/v6', 'major', null],
    ['github.com/elastic/go-elasticsearch/v8', 'major', null],
    ['github.com/go-openapi/jsonpointer', 'major', null],
    ['github.com/stretchr/testify', 'minor', 'go-libraries'],
  ];
  const moduleBranches = await flattenUpdates(config, {
    gomod: [{
      packageFile: 'go.mod',
      deps: modules.map(([name, type]) => ({
        depName: name, packageName: name, depType: 'require', datasource: 'go',
        currentValue: 'v1.0.0',
        updates: [{
          updateType: type, newValue: type === 'major' ? 'v2.0.0' : type === 'patch' ? 'v1.0.1' : 'v1.1.0',
          newVersion: type === 'major' ? 'v2.0.0' : type === 'patch' ? 'v1.0.1' : 'v1.1.0', newMajor: type === 'major' ? 2 : 1,
        }],
      })),
    }],
  });
  for (const branch of moduleBranches) {
    const group = modules.find(([name, type]) =>
      name === branch.depName && type === branch.updateType)[2];
    const automerge = branch.updateType === 'patch' || (branch.updateType === 'minor' && group !== 'kubernetes-go-modules');
    assert.equal(branch.automerge, automerge);
    assert.equal(branch.rebaseWhen, automerge ? 'behind-base-branch' : 'conflicted');
    if (branch.automerge) {
      assert.equal(branch.automergeType, 'pr');
      assert.equal(branch.platformAutomerge, true);
    }
    if (group) assert.equal(branch.branchName, `renovate/${branch.updateType === 'patch' ? 'patch-' : ''}${group}`);
    else assert.equal(branch.groupName, null);
  }
  const osv = new Vulnerabilities({
    getVulnerabilities: async (ecosystem, name) => [{
      id: 'GO-2099-0001',
      affected: [{
        package: { ecosystem, name },
        ranges: [{ type: 'SEMVER', events: [{ introduced: '0' }, { fixed: '1.0.1' }] }],
      }],
    }],
  });
  for (const name of ['github.com/stretchr/testify', 'github.com/aws/smithy-go', 'k8s.io/api']) {
    const securityConfig = { ...config, packageRules: [...config.packageRules] };
    const packageFiles = {
      gomod: [{
        packageFile: 'go.mod',
        deps: [{
          depName: name, packageName: name, depType: 'require', datasource: 'go', currentValue: 'v1.0.0',
          updates: [
            { updateType: 'patch', newValue: 'v1.0.1', newVersion: 'v1.0.1' },
            { updateType: 'minor', newValue: 'v1.1.0', newVersion: 'v1.1.0' },
            { updateType: 'major', newValue: 'v2.0.0', newVersion: 'v2.0.0', newMajor: 2 },
          ],
        }],
      }],
    };
    await osv.appendVulnerabilityPackageRules(securityConfig, packageFiles);
    assert(securityConfig.packageRules.length > config.packageRules.length, name);
    const securityBranches = await flattenUpdates(securityConfig, packageFiles);
    assert.equal(securityBranches.length, 3, name);
    for (const branch of securityBranches) {
      assert.equal(branch.isVulnerabilityAlert, true, name);
      assert.equal(branch.minimumReleaseAge, null, name);
      assert.equal(branch.groupName, null, name);
      assert.equal(branch.automerge, branch.updateType === 'patch'
        || (branch.updateType === 'minor' && name !== 'k8s.io/api'), name);
    }
  }
  const workflow = fs.readFileSync(path.join(repo, '.github/workflows/ci.yml'), 'utf8');
  const extracted = [
    ...actions.extractPackageFile(workflow, '.github/workflows/ci.yml', config).deps,
    ...config.customManagers.flatMap(manager =>
      regex.extractPackageFile(workflow, '.github/workflows/ci.yml', manager)?.deps ?? []),
  ];
  for (const name of [
    'golangci/golangci-lint', 'goreleaser/goreleaser',
    'github.com/google/go-licenses', 'golang.org/x/vuln',
  ]) {
    const dep = extracted.find(dep => (dep.packageName ?? dep.depName) === name);
    assert(dep, `Missing CI tool extraction: ${name}`);
    const manager = name === 'golangci/golangci-lint' ? 'github-actions' : 'custom.regex';
    for (const type of ['patch', 'minor', 'major']) {
      const policy = await applyPackageRules({
        ...config, ...dep, packageName: name, manager, updateType: type,
      });
      const builderPatch = name === 'goreleaser/goreleaser' && type === 'patch';
      assert.equal(policy.automerge, builderPatch, name);
      assert.equal(policy.rebaseWhen, builderPatch ? 'behind-base-branch' : 'conflicted', name);
      assert.equal(policy.groupName, type === 'major' ? null : builderPatch ? 'Go build environment' : 'CI tools', name);
      assert.equal(policy.minimumReleaseAge, builderPatch ? null : '7 days', name);
    }
  }
  const builder = extracted.find(dep => (dep.packageName ?? dep.depName) === 'goreleaser/goreleaser');
  const buildBranches = await flattenUpdates(config, {
    'custom.regex': [{
      packageFile: '.github/workflows/ci.yml',
      deps: [{
        ...builder,
        packageName: builder.packageName ?? builder.depName,
        updates: [{ updateType: 'patch', newValue: 'v1.25.2', newVersion: 'v1.25.2' }],
      }],
    }],
    gomod: [{
      packageFile: 'go.mod',
      deps: ['golang', 'toolchain'].map(depType => ({
        depName: 'go', packageName: 'go', depType, datasource: 'golang-version', currentValue: '1.26.8',
        updates: [{ updateType: 'patch', newValue: '1.26.9', newVersion: '1.26.9' }],
      })),
    }],
  });
  assert.equal(buildBranches.length, 3);
  assert.deepEqual([...new Set(buildBranches.map(branch => branch.branchName))], ['renovate/go-build-environment']);
  for (const branch of buildBranches) {
    assert.equal(branch.automerge, true);
    assert.equal(branch.platformAutomerge, true);
    assert.equal(branch.automergeType, 'pr');
    assert.equal(branch.minimumReleaseAge, null);
    assert.equal(branch.rebaseWhen, 'behind-base-branch');
  }
  console.log('PASS: module patches and minors automerge in separate branches');
  console.log('PASS: compatible GitHub Actions updates automerge separately; major actions stay manual');
  console.log('PASS: Kubernetes minors, module majors and non-builder CI tools stay manual; other module minors and all patches automerge');
  console.log('PASS: unsupported Go versions are filtered and PR creation is limited');
  console.log('PASS: routine releases wait seven days; known vulnerability fixes and build-environment patches remain immediate');
  console.log('PASS: OSV rules bypass the release delay without automerging Kubernetes minors or majors');
  console.log('PASS: extracted GoReleaser patches and both Go compiler directives share one automerge branch');
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
