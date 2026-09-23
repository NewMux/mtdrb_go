/**
 * The translator.
 *
 * Messages are nested objects. A leaf is either a string or a set of plural
 * forms, and `{name}` placeholders are filled from the params. Keys are typed
 * from the English dictionary, so a misspelt key fails `tsc`, and the Arabic
 * dictionary is typed to the same shape, so a missing translation does too.
 */

import { pluralCategory, type Locale, type PluralCategory } from './plural';

/** A leaf that varies with a count. `other` is always required. */
export type PluralForms = Partial<Record<PluralCategory, string>> & { other: string };

/** The shape every dictionary must have: English's, with any string allowed. */
export type Widen<T> = T extends string
  ? string
  : T extends { other: string }
    ? PluralForms
    : { [K in keyof T]: Widen<T[K]> };

/** Every dot-path to a leaf: "today.title", "clients.count". */
export type MessageKey<T, Prefix extends string = ''> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : T[K] extends { other: string }
      ? `${Prefix}${K}`
      : MessageKey<T[K], `${Prefix}${K}.`>;
}[keyof T & string];

export type Params = Record<string, string | number>;

export type Translate<Key extends string> = (key: Key, params?: Params) => string;

function lookup(messages: unknown, key: string): unknown {
  let node: unknown = messages;
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return node;
}

function isPlural(node: unknown): node is PluralForms {
  return typeof node === 'object' && node !== null && typeof (node as PluralForms).other === 'string';
}

/** Fills `{name}` from params. A placeholder with no value stays visible rather than vanishing. */
export function interpolate(template: string, params: Params = {}, formatNumber?: (n: number) => string): string {
  return template.replace(/\{(\w+)\}/g, (whole, name: string) => {
    const value = params[name];
    if (value === undefined) return whole;
    if (typeof value === 'number' && formatNumber) return formatNumber(value);
    return String(value);
  });
}

/**
 * Builds a translator for one locale, falling back to English for any key the
 * locale lacks — which typing prevents, but a key read from data could not.
 */
export function createTranslator<Key extends string>(
  locale: Locale,
  messages: unknown,
  fallback: unknown,
  formatNumber?: (n: number) => string,
): Translate<Key> {
  return (key, params) => {
    let node = lookup(messages, key);
    if (node === undefined) node = lookup(fallback, key);
    if (typeof node === 'string') return interpolate(node, params, formatNumber);
    if (isPlural(node)) {
      const count = Number(params?.count ?? 0);
      const form = node[pluralCategory(locale, count)] ?? node.other;
      return interpolate(form, params, formatNumber);
    }
    // A missing key shows as itself: visibly wrong in review, never blank.
    return key;
  };
}
