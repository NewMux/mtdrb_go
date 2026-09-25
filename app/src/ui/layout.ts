/**
 * How much room there is.
 *
 * Wide means a desk: a sidebar, tables instead of cards, a list and its detail
 * side by side. The same screens render narrow on a phone, so this is read at
 * render time rather than branched on platform — a tablet in landscape is a
 * desk, and a narrow browser window is a phone.
 */

import { useWindowDimensions } from 'react-native';
import { space, WIDE_BREAKPOINT } from './theme';

export function useLayout() {
  const { width, height } = useWindowDimensions();
  const wide = width >= WIDE_BREAKPOINT;
  return {
    width,
    height,
    wide,
    /** Side padding for a screen's content. */
    gutter: wide ? space.xxl : space.lg,
    /** Content is capped on very wide screens; a 2560px-wide table row is unreadable. */
    maxContentWidth: 1280,
  };
}
