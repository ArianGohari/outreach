// Render the vector master at native icon sizes. Requires Playwright Chromium.
import { createRequire } from 'node:module';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
const root = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(join(root, 'frontend/package.json'));
const { chromium } = require('@playwright/test');
const svg = await readFile(join(root, 'frontend/public/brand/app-icon.svg'), 'utf8');
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  const icons = [];
  await mkdir(join(root, 'build/windows'), { recursive: true });
  for (const size of [16, 24, 32, 48, 64, 128, 256, 1024]) {
    await page.setViewportSize({ width: size, height: size });
    await page.setContent(`<style>body{margin:0}svg{display:block;width:100vw;height:100vh}</style>${svg}`);
    const png = await page.screenshot({ omitBackground: true });
    if (size === 1024) await writeFile(join(root, 'build/appicon.png'), png);
    else icons.push({ size, png });
  }
  // ICO directory entries point to PNG payloads; supported by Windows Vista and later.
  const header = Buffer.alloc(6 + icons.length * 16);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(icons.length, 4);
  let offset = header.length;
  icons.forEach(({ size, png }, i) => {
    const at = 6 + i * 16;
    header[at] = header[at + 1] = size === 256 ? 0 : size;
    header.writeUInt16LE(1, at + 4);
    header.writeUInt16LE(32, at + 6);
    header.writeUInt32LE(png.length, at + 8);
    header.writeUInt32LE(offset, at + 12);
    offset += png.length;
  });
  await writeFile(join(root, 'build/windows/icon.ico'), Buffer.concat([header, ...icons.map(({ png }) => png)]));
  console.log('Updated build/appicon.png and build/windows/icon.ico');
} finally {
  await browser.close();
}
