/**
 * A QR code, drawn as one SVG path.
 *
 * For the two-step setup screen: a trainer at a laptop scans it with the
 * authenticator app on their phone. Always black on white, with the standard
 * four-module quiet zone, whatever the theme — a scanner needs the contrast
 * and does not care that the app is in dark mode.
 */

import React, { useMemo } from 'react';
import { View } from 'react-native';
import Svg, { Path, Rect } from 'react-native-svg';
import { qrPath } from './qr-path';

export function QRCode({ value, size = 200, label }: { value: string; size?: number; label: string }) {
  const { size: modules, d } = useMemo(() => qrPath(value), [value]);
  return (
    <View accessible accessibilityRole="image" accessibilityLabel={label} style={{ width: size, height: size }}>
      <Svg width={size} height={size} viewBox={`0 0 ${modules} ${modules}`}>
        <Rect x={0} y={0} width={modules} height={modules} fill="#ffffff" />
        <Path d={d} fill="#000000" />
      </Svg>
    </View>
  );
}
