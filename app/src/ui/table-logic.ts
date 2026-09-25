/**
 * Sorting and paging for DataTable, kept apart from the component so it is
 * tested in Node like the rest of the app's logic.
 */

export type SortDirection = 'asc' | 'desc';
export type SortValue = string | number | null | undefined;

/**
 * Stable sort by one column. Empty values sort last in both directions:
 * a client with no email should not jump to the top when the order flips.
 */
export function sortRows<T>(
  rows: readonly T[],
  value: (row: T) => SortValue,
  direction: SortDirection,
  collator?: Intl.Collator,
): T[] {
  const compareText = collator
    ? (a: string, b: string) => collator.compare(a, b)
    : (a: string, b: string) => a.localeCompare(b);
  return rows
    .map((row, index) => ({ row, index, key: value(row) }))
    .sort((a, b) => {
      const aEmpty = a.key === null || a.key === undefined || a.key === '';
      const bEmpty = b.key === null || b.key === undefined || b.key === '';
      if (aEmpty || bEmpty) return aEmpty === bEmpty ? a.index - b.index : aEmpty ? 1 : -1;
      const order = typeof a.key === 'number' && typeof b.key === 'number'
        ? a.key - b.key
        : compareText(String(a.key), String(b.key));
      if (order !== 0) return direction === 'asc' ? order : -order;
      return a.index - b.index;
    })
    .map((entry) => entry.row);
}

/** The next sort state when a header is tapped: asc, then desc, then off. */
export function nextSort(
  current: { key: string; direction: SortDirection } | null,
  key: string,
): { key: string; direction: SortDirection } | null {
  if (!current || current.key !== key) return { key, direction: 'asc' };
  if (current.direction === 'asc') return { key, direction: 'desc' };
  return null;
}
