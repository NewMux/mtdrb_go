import { newId } from '@/lib/id';

describe('newId', () => {
  it('produces a well-formed UUIDv7', () => {
    const id = newId();
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });

  it('sorts in creation order', () => {
    // The whole reason for v7: a week of offline rows arriving at once should
    // land in index order rather than scattering across a B-tree.
    const earlier = newId(new Date('2026-05-01T09:00:00Z').getTime());
    const later = newId(new Date('2026-05-01T10:00:00Z').getTime());
    expect(earlier < later).toBe(true);
  });

  it('is unique across a burst', () => {
    const ids = new Set<string>();
    for (let i = 0; i < 5000; i++) ids.add(newId());
    expect(ids.size).toBe(5000);
  });

  it('embeds the timestamp it was given', () => {
    const when = new Date('2026-05-01T09:00:00Z').getTime();
    const hex = newId(when).replace(/-/g, '').slice(0, 12);
    expect(parseInt(hex, 16)).toBe(when);
  });
});
