import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

import { ar } from '@/i18n/ar';
import { en } from '@/i18n/en';
import { i18nFor } from '@/i18n';
import { pluralCategory } from '@/i18n/plural';

describe('plural rules', () => {
  it('uses all six Arabic forms', () => {
    expect([0, 1, 2, 3, 10, 11, 99, 100, 101, 102, 103, 111].map((n) => pluralCategory('ar', n))).toEqual([
      'zero', 'one', 'two', 'few', 'few', 'many', 'many', 'other', 'other', 'other', 'few', 'many',
    ]);
    expect(pluralCategory('ar', 2.5)).toBe('other');
  });

  it('keeps English to one and other', () => {
    expect([0, 1, 2].map((n) => pluralCategory('en', n))).toEqual(['other', 'one', 'other']);
  });
});

describe('translation', () => {
  it('interpolates and picks the plural form for the count', () => {
    const { t } = i18nFor('en', 'latn');
    expect(t('today.greeting', { name: 'Sam' })).toBe('Hello Sam');
    expect(t('common.credits', { count: 1 })).toBe('1 credit');
    expect(t('common.credits', { count: 3 })).toBe('3 credits');

    const arabic = i18nFor('ar', 'latn');
    expect(arabic.t('common.sessions', { count: 2 })).toBe('جلستان');
    expect(arabic.t('common.sessions', { count: 11 })).toBe('11 جلسة');
  });

  it('writes counts in Arabic-Indic numerals when chosen', () => {
    const { t } = i18nFor('ar', 'arab');
    expect(t('common.sessions', { count: 5 })).toBe('٥ جلسات');
  });

  it('says how overdue an invoice is, in either language', () => {
    const today = new Date('2026-05-10T12:00:00Z');
    const english = i18nFor('en', 'latn');
    expect(english.due('2026-05-07', today)).toBe('3 days overdue');
    expect(english.due('2026-05-10', today)).toBe('due today');
    expect(english.due('2026-05-15', today)).toBe('due in 5 days');
    expect(english.due('2026-05-09', today)).toBe('1 day overdue');
    expect(english.due(null, today)).toBe('');
    expect(i18nFor('ar', 'latn').due('2026-05-08', today)).toBe('متأخرة يومين');
  });

  it('formats money in the currency\'s precision, in both scripts', () => {
    expect(i18nFor('en', 'latn').amount(50000, 'AED')).toBe('500.00');
    expect(i18nFor('en', 'latn').amount(12500, 'KWD')).toBe('12.500');
    expect(i18nFor('ar', 'arab').amount(50000, 'AED')).toBe('٥٠٠٫٠٠');
    expect(i18nFor('en', 'latn').money(50000, 'AED')).toContain('500.00');
  });
});

type Tree = { [key: string]: string | Tree };

function leaves(tree: Tree, prefix = ''): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string') out.set(path, [value]);
    else if (typeof value.other === 'string') out.set(path, Object.values(value) as string[]);
    else for (const [k, v] of leaves(value, path)) out.set(k, v);
  }
  return out;
}

const placeholders = (texts: string[]) =>
  [...new Set(texts.flatMap((text) => text.match(/\{\w+\}/g) ?? []))].sort();

describe('the Arabic dictionary', () => {
  it('uses the same placeholders as English for every key', () => {
    const english = leaves(en as unknown as Tree);
    const arabic = leaves(ar as unknown as Tree);
    const mismatched: string[] = [];
    for (const [key, texts] of english) {
      const other = arabic.get(key);
      if (!other) { mismatched.push(`${key}: missing`); continue; }
      // A plural form may drop {count} ("جلستان" is two sessions), so Arabic
      // must use a subset of English's placeholders, never an invented one.
      const extra = placeholders(other).filter((p) => !placeholders(texts).includes(p));
      if (extra.length) mismatched.push(`${key}: ${extra.join(', ')}`);
    }
    expect(mismatched).toEqual([]);
  });
});

/**
 * No screen may carry a hard-coded English string: it would render in English
 * in the middle of an Arabic screen and never be translated. Scans for JSX text
 * and for user-facing string props written as literals.
 */
describe('screens', () => {
  const root = join(__dirname, '../..');
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) walk(path);
      else if (path.endsWith('.tsx')) files.push(path);
    }
  };
  walk(join(root, 'app'));
  walk(join(root, 'src/ui'));

  it('carry no hard-coded user-facing text', () => {
    const offenders: string[] = [];
    const jsxText = /(?<!=)>\s*([A-Za-z][A-Za-z ,.'’!?-]{2,})\s*</g;
    const literalProp = /\b(title|label|placeholder|hint|detail|message|accessibilityLabel)="([^"]*[A-Za-z]{2,}[^"]*)"/g;
    for (const file of files) {
      const source = readFileSync(file, 'utf8')
        // Comments may say anything.
        .replace(/\/\*[\s\S]*?\*\//g, '')
        .replace(/^\s*\/\/.*$/gm, '')
        .replace(/\{\/\*[\s\S]*?\*\/\}/g, '');
      for (const match of source.matchAll(jsxText)) offenders.push(`${relative(root, file)}: >${match[1]}<`);
      for (const match of source.matchAll(literalProp)) offenders.push(`${relative(root, file)}: ${match[1]}="${match[2]}"`);
    }
    expect(offenders).toEqual([]);
  });
});
