import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, lstatSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const lockPath = resolve(root, 'docs/modern-web-guidance.lock.json');
const skillPath = '.agents/skills/modern-web-guidance';
const lock = JSON.parse(readFileSync(lockPath, 'utf8'));
assert.equal(lock.version, 1, 'Unsupported guidance lock format');
assert.equal(lock.source, 'https://github.com/GoogleChrome/modern-web-guidance');
assert.match(lock.commit, /^[a-f0-9]{40}$/, 'Pin a complete upstream commit');
assert.equal(lock.skillPath, skillPath);
assert.match(lock.cliVersion, /^\d+\.\d+\.\d+$/, 'Pin a complete CLI release');
assert.ok(Number(process.versions.node.split('.')[0]) >= 22, 'Node.js 22+ required');

const files = {};
function walk(directory) {
  assert.ok(!lstatSync(resolve(root, directory)).isSymbolicLink(), 'Vendored directories must be real files');
  for (const entry of readdirSync(resolve(root, directory), {withFileTypes: true}).toSorted((a,b) => a.name.localeCompare(b.name))) {
    const name = directory + '/' + entry.name;
    if (entry.isDirectory()) walk(name);
    else {
      assert.ok(entry.isFile(), 'Unexpected link or special file: ' + name);
      files[name] = createHash('sha256').update(readFileSync(resolve(root, name))).digest('hex');
    }
  }
}
walk(skillPath);
if (process.argv.includes('--write-lock')) {
  lock.files = files;
  writeFileSync(lockPath, JSON.stringify(lock, null, 2) + '\n');
}
assert.deepEqual(files, lock.files, 'Guidance snapshot changed; review upstream version and update its lock intentionally');
const count = Object.keys(files).filter(name => name.startsWith(skillPath + '/guides/') && name.endsWith('.md')).length;
assert.ok(count > 0, 'No local guides installed');
assert.ok(files[skillPath + '/SKILL.md'], 'Skill entry is missing');
assert.ok(files[skillPath + '/LICENSE'], 'Upstream license is missing');
const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
assert.equal(pkg.scripts.guidance, 'npx --yes modern-web-guidance@' + lock.cliVersion, 'CLI version must match the project lock');
assert.equal(pkg.scripts['guidance:check'], 'node scripts/check-web-guidance.mjs');
for (const name of ['AGENTS.md','CONTRIBUTING.md','CLAUDE.md','.github/copilot-instructions.md','.cursor/rules/web-guidance.mdc','docs/web-development-guidance.md']) {
  assert.ok(existsSync(resolve(root,name)), 'Missing team instructions: ' + name);
  assert.ok(readFileSync(resolve(root,name),'utf8').includes('modern-web-guidance'), 'Missing skill reference in ' + name);
}
assert.ok(readFileSync(resolve(root,'.github/pull_request_template.md'),'utf8').includes('## Modern Web Guidance'), 'PR review section missing');
assert.ok(readFileSync(resolve(root,'.github/workflows/dev-ci.yaml'),'utf8').includes('run: npm run guidance:check'), 'CI integrity step missing');
console.log('Modern Web Guidance: ' + count + ' guides verified; CLI ' + lock.cliVersion + '; upstream ' + lock.commit.slice(0,12));
console.log('This checks integration integrity. UI behavior and guide applicability require code review.');
