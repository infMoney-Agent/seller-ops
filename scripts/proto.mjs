import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const mode = process.argv[2] || 'generate';
const windows = process.platform === 'win32';
const bin = path.join(root, '.cache/proto/bin');
const npmBin = path.join(root, 'apps/web/node_modules/.bin');
const env = { ...process.env, GOBIN: bin, PATH: [bin, npmBin, process.env.PATH].join(path.delimiter) };
const go = process.env.GO_BINARY || 'go';
const buf = path.join(npmBin, windows ? 'buf.cmd' : 'buf');
function run(command, args) { execFileSync(command, args, { cwd: root, env, stdio: 'inherit', shell: windows && command.endsWith('.cmd') }); }
if (!['generate', 'check', 'breaking', 'lint'].includes(mode)) throw new Error('Unknown proto command');
if (mode === 'breaking') {
  const base = process.env.PROTO_BASE_REF || 'origin/dev';
  if (!/^[a-zA-Z0-9_./-]+$/.test(base)) throw new Error('Invalid base ref');
  execFileSync('git', ['rev-parse', '--verify', base + '^{commit}'], { cwd: root, stdio: 'ignore' });
  let exists = true;
  try { execFileSync('git', ['cat-file', '-e', base + ':proto/buf.yaml'], { cwd: root, stdio: 'ignore' }); } catch { exists = false; }
  if (!exists) { console.log('Proto bootstrap: base has no contracts; lint/generation checks still apply.'); process.exit(0); }
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'seller-proto-base-'));
  try {
    const archive = path.join(temp, 'baseline.tar');
    execFileSync('git', ['archive', '--format=tar', '--output', archive, base, 'proto'], { cwd: root, stdio: 'inherit' });
    execFileSync('tar', ['-xf', archive, '-C', temp], { stdio: 'inherit' });
    run(buf, ['breaking', 'proto', '--against', path.join(temp, 'proto')]);
  } finally { fs.rmSync(temp, { recursive: true, force: true }); }
} else {
  run(buf, ['lint', 'proto']);
  if (mode === 'lint') process.exit(0);
  fs.mkdirSync(bin, { recursive: true });
  run(go, ['install', 'google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6']);
  run(go, ['install', 'connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1']);
  run(buf, ['generate', '--template', 'proto/buf.gen.yaml']);
  if (mode === 'check') {
    const outputs = ['proto/gen/go', 'apps/web/src/gen'];
    run('git', ['diff', '--exit-code', '--', ...outputs]);
    const extra = execFileSync('git', ['ls-files', '--others', '--exclude-standard', '--', ...outputs], { cwd: root, encoding: 'utf8' }).trim();
    if (extra) throw new Error('Untracked generated files: ' + extra);
  }
}
