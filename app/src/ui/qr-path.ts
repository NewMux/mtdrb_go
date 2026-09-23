/**
 * The geometry of a QR code, apart from any drawing of it, so it can be
 * tested without a renderer (see qr.tsx).
 */

import qrcode from 'qrcode-generator';

/** The standard quiet zone, in modules. */
export const QUIET = 4;

/** The dark modules of a QR code for text, as an SVG path in module units. */
export function qrPath(text: string): { size: number; d: string } {
  // Type 0 picks the smallest version that fits; M recovers from 15% damage,
  // plenty for a screen.
  const qr = qrcode(0, 'M');
  qr.addData(text);
  qr.make();
  const count = qr.getModuleCount();
  let d = '';
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (qr.isDark(row, col)) d += `M${col + QUIET} ${row + QUIET}h1v1h-1z`;
    }
  }
  return { size: count + QUIET * 2, d };
}
