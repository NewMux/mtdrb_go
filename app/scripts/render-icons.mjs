// Renders the store and web icons from assets/icon.svg. Run after changing
// the mark: node scripts/render-icons.mjs (needs Playwright's Chromium).
//
//   icon.png            1024², the iOS icon and the fallback everywhere
//   adaptive-icon.png   1024², Android's foreground: the mark alone, inside
//                       the safe zone, on transparent (the background colour
//                       is set in app.config.ts)
//   splash-icon.png     the mark alone for the launch screen
//   favicon.png         48², the web tab
import { readFileSync } from 'node:fs';
import { execSync } from 'node:child_process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
let playwright;
try { playwright = require('playwright'); } catch {
  playwright = require(execSync('npm root -g').toString().trim() + '/playwright');
}

const svg = readFileSync(new URL('../assets/icon.svg', import.meta.url), 'utf8');
// The same mark without its background, shrunk into Android's 66% safe zone.
const markOnly = (scale) => svg
  .replace(/<rect[^>]*\/>/, '')
  .replace('translate(152 152) scale(30)', `translate(${512 - 12 * scale} ${512 - 12 * scale}) scale(${scale})`);

const outputs = [
  { file: 'icon.png', size: 1024, svg },
  { file: 'adaptive-icon.png', size: 1024, svg: markOnly(22) },
  { file: 'splash-icon.png', size: 1024, svg: markOnly(30) },
  { file: 'favicon.png', size: 48, svg },
];

const browser = await playwright.chromium.launch();
for (const { file, size, svg: source } of outputs) {
  const page = await browser.newPage({ viewport: { width: size, height: size } });
  await page.setContent(
    `<html><body style="margin:0;background:transparent">${source.replace('<svg ', `<svg width="${size}" height="${size}" `)}</body></html>`);
  await page.screenshot({ path: new URL(`../assets/${file}`, import.meta.url).pathname, omitBackground: true });
  await page.close();
}
await browser.close();
console.log('rendered', outputs.map((o) => o.file).join(', '));
