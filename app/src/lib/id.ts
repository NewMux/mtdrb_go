/**
 * UUIDv7 generation.
 *
 * v7 rather than v4 because these ids are minted on a device and inserted into
 * server tables: the embedded timestamp keeps them in index order, so a week of
 * offline rows arriving at once does not scatter writes across a B-tree.
 *
 * Hand-rolled rather than pulled from a package: it is twenty lines, and the
 * server's ids.New() produces the same layout.
 */

const HEX: string[] = [];
for (let i = 0; i < 256; i++) HEX.push((i + 0x100).toString(16).slice(1));

/** Random bytes, preferring the platform CSPRNG. */
function randomBytes(n: number): Uint8Array {
  const bytes = new Uint8Array(n);
  const g = globalThis as { crypto?: { getRandomValues?: (a: Uint8Array) => Uint8Array } };
  if (g.crypto?.getRandomValues) {
    g.crypto.getRandomValues(bytes);
    return bytes;
  }
  // Only reachable in an environment with no crypto at all. Ids would still be
  // unique enough not to collide, but this should not happen on a real device.
  for (let i = 0; i < n; i++) bytes[i] = Math.floor(Math.random() * 256);
  return bytes;
}

/** Generates a UUIDv7. */
export function newId(now: number = Date.now()): string {
  const bytes = randomBytes(16);

  // 48-bit big-endian millisecond timestamp.
  bytes[0] = (now / 2 ** 40) & 0xff;
  bytes[1] = (now / 2 ** 32) & 0xff;
  bytes[2] = (now / 2 ** 24) & 0xff;
  bytes[3] = (now / 2 ** 16) & 0xff;
  bytes[4] = (now / 2 ** 8) & 0xff;
  bytes[5] = now & 0xff;

  // Version 7 and the RFC 4122 variant.
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;

  const h = (i: number) => HEX[bytes[i] ?? 0] ?? '00';
  return (
    h(0) + h(1) + h(2) + h(3) + '-' +
    h(4) + h(5) + '-' +
    h(6) + h(7) + '-' +
    h(8) + h(9) + '-' +
    h(10) + h(11) + h(12) + h(13) + h(14) + h(15)
  );
}
