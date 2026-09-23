/**
 * Icons.
 *
 * Named by what they mean in this app, not by what they depict, so a screen
 * asks for `billing` and the drawing can change in one place. Each is imported
 * individually so the bundle carries only these.
 *
 * Icons accompany labels; they never replace one. A trainer in a dim gym reads
 * "Billing" faster than they decode a receipt glyph, which is why the phone's
 * tab bar keeps its words.
 */

import React from 'react';
import {
  Activity, ArrowLeft, ArrowRight, Building2, CalendarDays, CalendarPlus, ChartColumn, Check,
  ChevronDown, ChevronLeft, ChevronRight, CircleAlert, CircleCheck, Clock, CloudOff, Copy,
  CreditCard, Crown, Download, Dumbbell, Ellipsis, FileText, Languages, LayoutDashboard,
  ListChecks, Lock, LogOut, Mail, MapPin, Menu, Minus, Moon, Package, Pencil, Percent, Phone,
  Plus, Receipt, RefreshCw, ScanBarcode, Search, Settings, Share2, ShoppingBag, SlidersHorizontal,
  Sparkles, Sun, Target, Trash2, TrendingDown, TrendingUp, TriangleAlert, Upload, User, Users,
  Wallet, X, type LucideIcon,
} from 'lucide-react-native';

import { useTheme } from './theming';

const ICONS = {
  today: Sun,
  dashboard: LayoutDashboard,
  clients: Users,
  calendar: CalendarDays,
  calendarAdd: CalendarPlus,
  programs: Dumbbell,
  billing: Receipt,
  money: Wallet,
  shop: ShoppingBag,
  packages: Package,
  analytics: ChartColumn,
  insights: Sparkles,
  tasks: ListChecks,
  locations: MapPin,
  settings: Settings,
  more: Ellipsis,
  menu: Menu,
  add: Plus,
  remove: Minus,
  search: Search,
  filter: SlidersHorizontal,
  next: ChevronRight,
  previous: ChevronLeft,
  forward: ArrowRight,
  backward: ArrowLeft,
  expand: ChevronDown,
  close: X,
  check: Check,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleAlert,
  sync: RefreshCw,
  offline: CloudOff,
  user: User,
  signOut: LogOut,
  download: Download,
  upload: Upload,
  delete: Trash2,
  edit: Pencil,
  up: TrendingUp,
  down: TrendingDown,
  light: Sun,
  dark: Moon,
  language: Languages,
  time: Clock,
  email: Mail,
  phone: Phone,
  card: CreditCard,
  barcode: ScanBarcode,
  locked: Lock,
  pro: Crown,
  copy: Copy,
  share: Share2,
  document: FileText,
  tax: Percent,
  business: Building2,
  target: Target,
  activity: Activity,
} satisfies Record<string, LucideIcon>;

export type IconName = keyof typeof ICONS;

/**
 * Icons that point somewhere. Under right-to-left "next" is leftward, so these
 * mirror; a clock or a receipt does not.
 */
const DIRECTIONAL = new Set<IconName>(['next', 'previous', 'forward', 'backward', 'signOut']);

export function Icon({
  name, size = 20, color, strokeWidth = 2,
}: {
  name: IconName;
  size?: number;
  color?: string;
  strokeWidth?: number;
}) {
  const theme = useTheme();
  const Glyph = ICONS[name];
  const mirror = theme.isRTL && DIRECTIONAL.has(name);
  return (
    <Glyph
      size={size}
      color={color ?? theme.colors.ink}
      strokeWidth={strokeWidth}
      style={mirror ? { transform: [{ scaleX: -1 }] } : undefined}
    />
  );
}
