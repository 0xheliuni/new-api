/*
Guards the billing-formula i18n keys used by web/classic/src/helpers/render.jsx.

Why this exists: a previous attempt to fix the image-input formula added the new
key to zh.json only. i18next then falls back to echoing the raw Chinese source
string for every other language, so the bug looked fixed in Chinese and stayed
broken everywhere else. This check fails when a formula key is missing from any
locale, or when a translation drops/renames a {{placeholder}} (which would
render as a literal "{{price}}" in the UI).

Run: node web/classic/scripts/check-billing-i18n-keys.mjs
*/

import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const localesDir = join(
  dirname(fileURLToPath(import.meta.url)),
  '..',
  'src',
  'i18n',
  'locales',
);

// Formula keys that must resolve in every language. Keep in sync with the
// buildBillingText/buildBillingPriceText calls in helpers/render.jsx.
const REQUIRED_KEYS = [
  '(输入 {{input}} tokens / 1M tokens * {{symbol}}{{price}}',
  '(输入 {{nonCacheInput}} tokens / 1M tokens * {{symbol}}{{price}} + 缓存 {{cacheInput}} tokens / 1M tokens * {{symbol}}{{cachePrice}}',
  '(输入 {{nonImageInput}} tokens / 1M tokens * {{symbol}}{{price}} + 图片输入 {{imageInput}} tokens / 1M tokens * {{symbol}}{{imagePrice}}',
  '(输入 {{nonAudioInput}} tokens / 1M tokens * {{symbol}}{{price}} + 音频输入 {{audioInput}} tokens / 1M tokens * {{symbol}}{{audioPrice}}',
  '输出 {{completion}} tokens / 1M tokens * {{symbol}}{{compPrice}}) * {{ratioType}} {{ratio}}',
];

const placeholders = (s) => (s.match(/\{\{(\w+)\}\}/g) ?? []).sort();

// Locale JSONs are flat maps, but tolerate one level of namespacing.
function flatten(obj, out = {}) {
  for (const [k, v] of Object.entries(obj)) {
    if (typeof v === 'string') out[k] = v;
    else if (v && typeof v === 'object') flatten(v, out);
  }
  return out;
}

const localeFiles = readdirSync(localesDir).filter((f) => f.endsWith('.json'));
if (localeFiles.length === 0) {
  console.error(`no locale files found in ${localesDir}`);
  process.exit(1);
}

const problems = [];

for (const file of localeFiles) {
  const messages = flatten(
    JSON.parse(readFileSync(join(localesDir, file), 'utf8')),
  );
  for (const key of REQUIRED_KEYS) {
    const value = messages[key];
    if (value === undefined) {
      problems.push(`${file}: MISSING key\n    ${key}`);
      continue;
    }
    const want = placeholders(key);
    const got = placeholders(value);
    if (want.join() !== got.join()) {
      problems.push(
        `${file}: placeholder mismatch\n    key   ${want.join(' ')}\n    value ${got.join(' ')}`,
      );
    }
  }
}

console.log(
  `checked ${REQUIRED_KEYS.length} formula keys across ${localeFiles.length} locales: ${localeFiles.join(', ')}`,
);

if (problems.length > 0) {
  console.error(`\n${problems.length} problem(s):\n`);
  for (const p of problems) console.error(`  ${p}\n`);
  process.exit(1);
}

console.log('all formula keys present with matching placeholders');
