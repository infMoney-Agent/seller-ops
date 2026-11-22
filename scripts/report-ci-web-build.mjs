import fs from 'node:fs';
const log = fs.readFileSync(process.argv[2], 'utf8').replace(/\u001b\[[0-9;]*m/g, '');
const lines = log.split(/\r?\n/);
const selected = new Set();
for (let i = 0; i < lines.length; i++) {
  if (/error:|build error|failed to|failed compiling|module not found|queries have|next\/font|cannot find|no space left|panic/i.test(lines[i])) {
    for (let j = Math.max(0, i - 1); j <= Math.min(lines.length - 1, i + 3); j++) selected.add(j);
  }
}
const detail = [...selected].sort((a,b)=>a-b).map(i=>lines[i]).join('\n').slice(-6000)
  || 'Web build failed. See the seller-web-build.log artifact for details.';
const safe = detail.replace(/https?:\/\/[^\s<>"']+/g, raw => {
  try { const url = new URL(raw); return url.origin + url.pathname; } catch { return '[URL]'; }
});
const escaped = safe.replaceAll('%', '%25').replaceAll('\r', '%0D').replaceAll('\n', '%0A');
console.log('::error title=Web build failed::' + escaped);
