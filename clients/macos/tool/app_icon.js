// The app icon: the critter in its propeller hat on a near-black rounded square.
// Run from the repository root: node clients/macos/tool/app_icon.js
// Draws 1024 px with 4x4 samples per pixel for the corner, then halves it down.
const fs = require('fs'), zlib = require('zlib');
const N = 1024, BG = [0x0A, 0x0A, 0x0A], ORANGE = [0xFF, 0x4D, 0x00];
const HAT = { a: [0x6F, 0x8F, 0xD8], b: [0x4A, 0x63, 0xA3], c: [0xF7, 0xF4, 0xEC], d: [0x7A, 0x77, 0x6F] };
const px = new Float64Array(N * N * 4);
// the plate: macOS's icon grid, 824 px in a 1024 px canvas
const P0 = 100, P1 = 924, R = 185;
const inside = (x, y) => {
  if (x < P0 || x > P1 || y < P0 || y > P1) return false;
  const cx = x < P0 + R ? P0 + R : x > P1 - R ? P1 - R : x, cy = y < P0 + R ? P0 + R : y > P1 - R ? P1 - R : y;
  return (x - cx) ** 2 + (y - cy) ** 2 <= R * R;
};
for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) {
  let c = 0;
  for (let j = 0; j < 4; j++) for (let i = 0; i < 4; i++) if (inside(x + (i + .5) / 4, y + (j + .5) / 4)) c++;
  const a = c / 16, o = (y * N + x) * 4;
  px[o] = BG[0] * a; px[o + 1] = BG[1] * a; px[o + 2] = BG[2] * a; px[o + 3] = 255 * a;   // premultiplied
}
const U = 46, X0 = (N - 14 * U) / 2, Y0 = Math.round((N - 11.6 * U) / 2) + 4 * U;
const rect = (x, y, w, h, col) => {
  const x0 = Math.round(X0 + x * U), y0 = Math.round(Y0 + y * U), x1 = Math.round(X0 + (x + w) * U), y1 = Math.round(Y0 + (y + h) * U);
  for (let yy = y0; yy < y1; yy++) for (let xx = x0; xx < x1; xx++) { const o = (yy * N + xx) * 4; px[o] = col[0]; px[o + 1] = col[1]; px[o + 2] = col[2]; px[o + 3] = 255; }
};
rect(1, 0, 12, 6, ORANGE);                                   // body
rect(0, 2, 1, 2, ORANGE); rect(13, 2, 1, 2, ORANGE);         // arms
[2, 4, 9, 11].forEach((x) => rect(x, 6, 1, 1.6, ORANGE));    // legs
rect(4, 1.5, 1, 2, BG); rect(9, 1.5, 1, 2, BG);              // eyes
['..cccc.aaaa...', '.......d......', '....ababab....', '...aaaaaaaa...'].forEach((row, r) =>
  [...row].forEach((ch, c) => { if (HAT[ch]) rect(c, -4 + r, 1, 1, HAT[ch]); }));

const png = (w, data) => {
  const raw = Buffer.alloc((w * 4 + 1) * w);
  for (let y = 0; y < w; y++) for (let x = 0; x < w; x++) {
    const i = (y * w + x) * 4, o = y * (w * 4 + 1) + 1 + x * 4, a = data[i + 3];
    raw[o] = a ? Math.round(data[i] * 255 / a) : 0; raw[o + 1] = a ? Math.round(data[i + 1] * 255 / a) : 0;
    raw[o + 2] = a ? Math.round(data[i + 2] * 255 / a) : 0; raw[o + 3] = Math.round(a);
  }
  const chunk = (t, d) => { const b = Buffer.alloc(12 + d.length); b.writeUInt32BE(d.length, 0); b.write(t, 4, 'ascii'); d.copy(b, 8); b.writeUInt32BE(zlib.crc32(b.subarray(4, 8 + d.length)), 8 + d.length); return b; };
  const hdr = Buffer.alloc(13); hdr.writeUInt32BE(w, 0); hdr.writeUInt32BE(w, 4); hdr[8] = 8; hdr[9] = 6;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', hdr), chunk('IDAT', zlib.deflateSync(raw, { level: 9 })), chunk('IEND', Buffer.alloc(0))]);
};
const half = (w, d) => { const h = w / 2, o = new Float64Array(h * h * 4);
  for (let y = 0; y < h; y++) for (let x = 0; x < h; x++) for (let k = 0; k < 4; k++)
    o[(y * h + x) * 4 + k] = (d[(2 * y * w + 2 * x) * 4 + k] + d[(2 * y * w + 2 * x + 1) * 4 + k] + d[((2 * y + 1) * w + 2 * x) * 4 + k] + d[((2 * y + 1) * w + 2 * x + 1) * 4 + k]) / 4;
  return o; };
const set = 'clients/macos/macos/Runner/Assets.xcassets/AppIcon.appiconset/';
let w = N, d = px;
for (;;) {
  if ([1024, 512, 256, 128, 64, 32, 16].includes(w)) fs.writeFileSync(set + 'app_icon_' + w + '.png', png(w, d));
  if (w === 256) fs.writeFileSync('clients/macos/linux/runner/app_icon.png', png(w, d));
  if (w === 16) break;
  d = half(w, d); w /= 2;
}
console.log('icons written');
