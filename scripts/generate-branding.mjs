import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { deflateSync } from 'node:zlib';

// Approved Classic ring, in a 64-unit rounded square. Keep the SVG and
// antialiased RGB raster geometry together; no fonts or rendering dependencies.
const root = new URL('../', import.meta.url);
const sage = [0xb4, 0xd6, 0xa3];
const ink = [0x18, 0x21, 0x1a];
const backdrop = [0x18, 0x1b, 0x1a];
const radius = 19 * .84375;
const halfStroke = 2 * .84375;
const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" fill="none"><rect width="64" height="64" rx="15" fill="#b4d6a3"/><g transform="translate(5 5) scale(.84375)"><circle cx="32" cy="32" r="19" stroke="#18211a" stroke-width="4"/></g></svg>\n';

function crc(bytes) {
 let value = 0xffffffff;
 for (const byte of bytes) {
  value ^= byte;
  for (let bit = 0; bit < 8; bit++) value = (value >>> 1) ^ ((value & 1) ? 0xedb88320 : 0);
 }
 return (value ^ 0xffffffff) >>> 0;
}
function chunk(type, bytes) {
 const result = Buffer.alloc(bytes.length + 12);
 result.writeUInt32BE(bytes.length);
 result.write(type, 4);
 bytes.copy(result, 8);
 result.writeUInt32BE(crc(result.subarray(4, -4)), result.length - 4);
 return result;
}
function png(size) {
 const data = Buffer.alloc(size * (1 + size * 3));
 const samples = 4;
 for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) {
  const sum = [0, 0, 0];
  for (let sy = 0; sy < samples; sy++) for (let sx = 0; sx < samples; sx++) {
   const px = (x + (sx + .5) / samples) * 64 / size;
   const py = (y + (sy + .5) / samples) * 64 / size;
   const dx = Math.max(Math.abs(px - 32) - 17, 0);
   const dy = Math.max(Math.abs(py - 32) - 17, 0);
   const distance = Math.hypot(px - 32, py - 32);
   const color = Math.hypot(dx, dy) > 15 ? backdrop : Math.abs(distance - radius) <= halfStroke ? ink : sage;
   for (let channel = 0; channel < 3; channel++) sum[channel] += color[channel];
  }
  for (let channel = 0; channel < 3; channel++) data[y * (1 + size * 3) + 1 + x * 3 + channel] = Math.round(sum[channel] / (samples * samples));
 }
 const header = Buffer.alloc(13);
 header.writeUInt32BE(size);
 header.writeUInt32BE(size, 4);
 header[8] = 8;
 header[9] = 2; // RGB8, matching the existing Windows ICO generator.
 return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]), chunk('IHDR', header), chunk('IDAT', deflateSync(data, { level: 9 })), chunk('IEND', Buffer.alloc(0))]);
}
const large = png(512);
const outputs = new Map([
 ['pwa/icons/agent.svg', Buffer.from(svg)],
 ['pwa/icons/icon-192.png', png(192)],
 ['pwa/icons/icon-512.png', large],
 ['pwa/icons/apple-touch-icon.png', png(180)],
 ['apps/android/app/src/main/res/drawable-nodpi/agent_icon.png', large],
 ['apps/windows/Agent.App/Assets/Agent.png', large],
]);
const check = process.argv.includes('--check');
for (const [path, bytes] of outputs) {
 if (check) {
  if (!readFileSync(new URL(path, root)).equals(bytes)) throw new Error(`Stale Classic ring artwork: ${path}`);
 } else writeFileSync(new URL(path, root), bytes);
}
const windowsGenerator = fileURLToPath(new URL('apps/windows/generate-icons.mjs', root));
if (check) {
 const temporary = mkdtempSync(join(tmpdir(), 'agent-branding-'));
 try {
  execFileSync(process.execPath, [windowsGenerator, temporary]);
  if (!readFileSync(join(temporary, 'Agent.ico')).equals(readFileSync(new URL('apps/windows/Agent.App/Assets/Agent.ico', root)))) throw new Error('Stale Classic ring Windows ICO');
 } finally { rmSync(temporary, { recursive: true, force: true }); }
} else execFileSync(process.execPath, [windowsGenerator], { stdio: 'inherit' });
console.log(`${check ? 'Verified' : 'Generated'} Classic ring SVG, web/PWA, Android and Windows artwork.`);
