/**
 * Icons.
 *
 * SF Symbols on Apple platforms — the real ones, rendered natively, so they
 * match weight and optical size with the system text beside them — and the
 * nearest Ionicons glyph everywhere else. Screens name an icon by meaning
 * ("clients", "timer"), never by glyph, so swapping a glyph is a one-line
 * change here.
 */

import React from 'react';
import { Platform, type StyleProp, type ViewStyle } from 'react-native';
import { SymbolView, type SymbolWeight } from 'expo-symbols';
import Ionicons from '@expo/vector-icons/Ionicons';
import type { SFSymbol } from 'sf-symbols-typescript';

type IonName = React.ComponentProps<typeof Ionicons>['name'];

const ICONS = {
  today: ['sun.max.fill', 'sunny'],
  calendar: ['calendar', 'calendar'],
  clients: ['person.2.fill', 'people'],
  programs: ['dumbbell.fill', 'barbell'],
  business: ['chart.bar.xaxis', 'stats-chart'],
  plus: ['plus', 'add'],
  minus: ['minus', 'remove'],
  chevronRight: ['chevron.right', 'chevron-forward'],
  chevronLeft: ['chevron.left', 'chevron-back'],
  chevronDown: ['chevron.down', 'chevron-down'],
  check: ['checkmark', 'checkmark'],
  checkCircle: ['checkmark.circle.fill', 'checkmark-circle'],
  circle: ['circle', 'ellipse-outline'],
  close: ['xmark', 'close'],
  closeCircle: ['xmark.circle.fill', 'close-circle'],
  search: ['magnifyingglass', 'search'],
  settings: ['gearshape.fill', 'settings-sharp'],
  person: ['person.fill', 'person'],
  personAdd: ['person.crop.circle.badge.plus', 'person-add'],
  money: ['banknote.fill', 'cash'],
  wallet: ['wallet.bifold.fill', 'wallet'],
  bank: ['building.columns.fill', 'business'],
  invoice: ['doc.text.fill', 'document-text'],
  receipt: ['receipt.fill', 'receipt'],
  share: ['square.and.arrow.up', 'share-outline'],
  timer: ['timer', 'timer-outline'],
  clock: ['clock.fill', 'time'],
  flame: ['flame.fill', 'flame'],
  trophy: ['trophy.fill', 'trophy'],
  bolt: ['bolt.fill', 'flash'],
  heart: ['heart.fill', 'heart'],
  scale: ['scalemass.fill', 'speedometer'],
  trend: ['chart.line.uptrend.xyaxis', 'trending-up'],
  warning: ['exclamationmark.triangle.fill', 'warning'],
  bell: ['bell.fill', 'notifications'],
  sync: ['arrow.triangle.2.circlepath', 'sync'],
  offline: ['icloud.slash.fill', 'cloud-offline'],
  synced: ['checkmark.icloud.fill', 'cloud-done'],
  trash: ['trash', 'trash-outline'],
  edit: ['pencil', 'create-outline'],
  more: ['ellipsis', 'ellipsis-horizontal'],
  phone: ['phone.fill', 'call'],
  message: ['message.fill', 'chatbubble'],
  mail: ['envelope.fill', 'mail'],
  play: ['play.fill', 'play'],
  pause: ['pause.fill', 'pause'],
  repeat: ['arrow.counterclockwise', 'refresh'],
  swap: ['arrow.left.arrow.right', 'swap-horizontal'],
  list: ['list.bullet', 'list'],
  note: ['note.text', 'reader'],
  copy: ['doc.on.doc', 'copy-outline'],
  moon: ['moon.fill', 'moon'],
  signOut: ['rectangle.portrait.and.arrow.right', 'log-out-outline'],
  tag: ['tag.fill', 'pricetag'],
  location: ['mappin.and.ellipse', 'location'],
  star: ['star.fill', 'star'],
  plates: ['circle.grid.cross.fill', 'disc'],
  sparkles: ['sparkles', 'sparkles'],
  info: ['info.circle', 'information-circle-outline'],
  lock: ['lock.fill', 'lock-closed'],
  template: ['square.on.square', 'albums'],
  strength: ['figure.strengthtraining.traditional', 'barbell'],
  cart: ['cart.fill', 'cart'],
  up: ['arrow.up.right', 'trending-up'],
  down: ['arrow.down.right', 'trending-down'],
  photo: ['photo.on.rectangle', 'images'],
  heartPulse: ['waveform.path.ecg', 'pulse'],
  target: ['target', 'locate'],
  ruler: ['ruler.fill', 'resize'],
  bandage: ['bandage.fill', 'medkit'],
  shield: ['checkmark.shield.fill', 'shield-checkmark'],
  globe: ['globe', 'globe-outline'],
  hourglass: ['hourglass', 'hourglass-outline'],
  run: ['figure.run', 'walk'],
  layers: ['square.stack.3d.up.fill', 'layers'],
  export: ['square.and.arrow.down', 'download-outline'],
  link: ['link', 'link'],
  sun: ['sun.max', 'sunny-outline'],
  circleHalf: ['circle.lefthalf.filled', 'contrast'],
} as const satisfies Record<string, readonly [SFSymbol, IonName]>;

export type IconName = keyof typeof ICONS;

export function Icon({
  name, size = 20, color, weight = 'semibold', style,
}: {
  name: IconName;
  size?: number;
  color: string;
  weight?: SymbolWeight;
  style?: StyleProp<ViewStyle>;
}) {
  const [sf, ion] = ICONS[name];
  const fallback = <Ionicons name={ion} size={size} color={color} />;
  if (Platform.OS !== 'ios') return fallback;
  return (
    <SymbolView
      name={sf}
      size={size}
      tintColor={color}
      weight={weight}
      type="monochrome"
      resizeMode="scaleAspectFit"
      fallback={fallback}
      style={[{ width: size, height: size }, style]}
    />
  );
}
