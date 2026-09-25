/**
 * Things that open over a screen: sheets, the details drawer, confirmations
 * and toasts.
 *
 * Built on React Native's own Modal, which renders on the web too, rather
 * than on Alert. `Alert.alert` is a silent no-op in react-native-web: on the
 * web build, a trainer tapping Completed for a client with no credits used to
 * see nothing at all, and the session was not marked. A confirmation here is
 * a component, so every platform shows the same question.
 *
 * On a phone a sheet rises from the bottom, where the thumb is; on a desk it
 * is a centred dialog. The drawer slides in from the trailing edge — right in
 * English, left in Arabic.
 */

import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import {
  Animated, Modal, Pressable, ScrollView, Text, View, useWindowDimensions, type StyleProp, type ViewStyle,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useT } from '@/i18n';
import { Button, Heading, IconButton, Row, Spacer, Body } from './components';
import { Icon } from './icon';
import { radius, space, WIDE_BREAKPOINT } from './theme';
import { makeStyles, useTheme } from './theming';

export function Sheet({
  visible, onClose, title, children, footer, width = 520,
}: {
  visible: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
  /** The dialog's width on a wide screen. */
  width?: number;
}) {
  const styles = useStyles();
  const { width: screenWidth, height } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const { t } = useT();
  const wide = screenWidth >= 720;

  return (
    <Modal visible={visible} transparent animationType={wide ? 'fade' : 'slide'} onRequestClose={onClose}>
      <View style={[styles.backdrop, wide ? styles.backdropCentred : styles.backdropBottom]}>
        <Pressable style={styles.dismiss} onPress={onClose} accessibilityLabel={t('common.close')} accessibilityRole="button" />
        <View
          accessibilityViewIsModal
          style={[
            styles.sheet,
            wide ? [styles.dialog, { width: Math.min(width, screenWidth - space.xxl * 2) }] : styles.bottom,
            { maxHeight: height * (wide ? 0.86 : 0.92), paddingBottom: wide ? space.lg : insets.bottom + space.lg },
          ]}
        >
          <Row style={styles.header}>
            <View style={{ flex: 1 }}><Heading>{title}</Heading></View>
            <IconButton icon="close" label={t('common.close')} onPress={onClose} tone="quiet" size={36} />
          </Row>
          <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
            {children}
          </ScrollView>
          {footer ? <View style={styles.footer}>{footer}</View> : null}
        </View>
      </View>
    </Modal>
  );
}

/**
 * The slide-in details pane — a session's attendees, an invoice's lines.
 *
 * Full screen on a phone, where there is no room beside the list; a side
 * panel on a desk, where the list it came from stays in view.
 */
export function Drawer({
  visible, onClose, title, children, footer, subtitle,
}: {
  visible: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  const styles = useStyles();
  const theme = useTheme();
  const { width } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const { t } = useT();
  const wide = width >= WIDE_BREAKPOINT;
  const panelWidth = wide ? Math.min(480, width * 0.4) : width;

  const offset = useRef(new Animated.Value(1)).current;
  useEffect(() => {
    Animated.timing(offset, { toValue: visible ? 0 : 1, duration: 180, useNativeDriver: false }).start();
  }, [visible, offset]);

  // Slides from the trailing edge, which is the left in right-to-left.
  const sign = theme.isRTL ? -1 : 1;
  const translateX = offset.interpolate({ inputRange: [0, 1], outputRange: [0, sign * panelWidth] });

  return (
    <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
      <View style={[styles.backdrop, styles.drawerBackdrop]}>
        <Pressable style={styles.dismiss} onPress={onClose} accessibilityLabel={t('common.close')} accessibilityRole="button" />
        <Animated.View
          accessibilityViewIsModal
          style={[styles.drawer, { width: panelWidth, paddingTop: insets.top, transform: [{ translateX }] }]}
        >
          <Row style={styles.header}>
            <View style={{ flex: 1 }}>
              <Heading>{title}</Heading>
              {subtitle ? <Text style={styles.subtitle}>{subtitle}</Text> : null}
            </View>
            <IconButton icon="close" label={t('common.close')} onPress={onClose} tone="quiet" size={36} />
          </Row>
          <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
            {children}
          </ScrollView>
          {footer ? <View style={[styles.footer, { paddingBottom: insets.bottom + space.lg }]}>{footer}</View> : null}
        </Animated.View>
      </View>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Confirmations
// ---------------------------------------------------------------------------

export interface ConfirmAction<T extends string> {
  value: T;
  label: string;
  tone?: 'primary' | 'danger' | 'default';
}

interface ConfirmRequest {
  title: string;
  message: string;
  actions: ConfirmAction<string>[];
  resolve: (value: string | null) => void;
}

type Confirm = <T extends string>(options: {
  title: string;
  message: string;
  actions: ConfirmAction<T>[];
}) => Promise<T | null>;

const ConfirmContext = createContext<Confirm | null>(null);

/**
 * Asks a question and resolves with the chosen action, or null if dismissed.
 *
 *   const choice = await confirm({ title, message, actions: [...] });
 */
export function useConfirm(): Confirm {
  const confirm = useContext(ConfirmContext);
  if (!confirm) throw new Error('useConfirm outside OverlayProvider');
  return confirm;
}

// ---------------------------------------------------------------------------
// Toasts
// ---------------------------------------------------------------------------

type ToastTone = 'success' | 'danger' | 'muted';
interface Toast { id: number; message: string; tone: ToastTone }

const ToastContext = createContext<((message: string, tone?: ToastTone) => void) | null>(null);

/** A short line confirming something happened, gone after a few seconds. */
export function useToast(): (message: string, tone?: ToastTone) => void {
  const toast = useContext(ToastContext);
  if (!toast) throw new Error('useToast outside OverlayProvider');
  return toast;
}

export function OverlayProvider({ children }: { children: React.ReactNode }) {
  const styles = useStyles();
  const { colors } = useTheme();
  const insets = useSafeAreaInsets();
  const [request, setRequest] = useState<ConfirmRequest | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(1);

  const confirm = useCallback<Confirm>((options) => new Promise((resolve) => {
    setRequest({
      title: options.title,
      message: options.message,
      actions: options.actions,
      resolve: resolve as (value: string | null) => void,
    });
  }), []);

  const answer = (value: string | null) => {
    request?.resolve(value);
    setRequest(null);
  };

  const toast = useCallback((message: string, tone: ToastTone = 'success') => {
    const id = nextId.current++;
    setToasts((current) => [...current, { id, message, tone }]);
    setTimeout(() => setToasts((current) => current.filter((x) => x.id !== id)), 3200);
  }, []);

  const confirmValue = useMemo(() => confirm, [confirm]);

  return (
    <ConfirmContext.Provider value={confirmValue}>
      <ToastContext.Provider value={toast}>
        {children}

        <Sheet visible={request !== null} onClose={() => answer(null)} title={request?.title ?? ''} width={440}>
          <Body muted>{request?.message ?? ''}</Body>
          <Spacer size={space.lg} />
          {request?.actions.map((action) => (
            <View key={action.value} style={{ marginBottom: space.sm }}>
              <Button
                label={action.label}
                tone={action.tone === 'danger' ? 'danger' : action.tone === 'primary' ? 'primary' : 'default'}
                onPress={() => answer(action.value)}
              />
            </View>
          ))}
        </Sheet>

        <View pointerEvents="none" style={[styles.toasts, { bottom: insets.bottom + 96 }]}>
          {toasts.map((item) => (
            <View key={item.id} style={styles.toast} accessibilityLiveRegion="polite" accessibilityRole="alert">
              <Icon
                name={item.tone === 'danger' ? 'error' : item.tone === 'muted' ? 'sync' : 'success'}
                size={16}
                color={item.tone === 'danger' ? colors.danger : item.tone === 'muted' ? colors.inkMuted : colors.success}
              />
              <Text style={styles.toastText}>{item.message}</Text>
            </View>
          ))}
        </View>
      </ToastContext.Provider>
    </ConfirmContext.Provider>
  );
}

/** A labelled row of content inside a sheet or drawer: "Location — Studio". */
export function DetailRow({ label, value, style }: { label: string; value: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const styles = useStyles();
  return (
    <View style={[styles.detail, style]}>
      <Text style={styles.detailLabel}>{label}</Text>
      {typeof value === 'string' ? <Text style={styles.detailValue}>{value}</Text> : value}
    </View>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  backdrop: { flex: 1, backgroundColor: colors.scrim },
  backdropBottom: { justifyContent: 'flex-end' },
  backdropCentred: { justifyContent: 'center', alignItems: 'center' },
  drawerBackdrop: { flexDirection: 'row', justifyContent: 'flex-end' },
  dismiss: { position: 'absolute', top: 0, bottom: 0, start: 0, end: 0 },

  sheet: { backgroundColor: colors.surface, overflow: 'hidden' },
  bottom: { borderTopLeftRadius: radius.xl, borderTopRightRadius: radius.xl, width: '100%' },
  dialog: { borderRadius: radius.xl },
  drawer: { height: '100%', backgroundColor: colors.surface, borderStartWidth: 1, borderStartColor: colors.border },

  header: { paddingHorizontal: space.lg, paddingTop: space.lg, paddingBottom: space.sm },
  subtitle: { ...type.caption, color: colors.inkMuted, marginTop: 2 },
  content: { paddingHorizontal: space.lg, paddingBottom: space.lg },
  footer: { paddingHorizontal: space.lg, paddingTop: space.md, borderTopWidth: 1, borderTopColor: colors.border },

  toasts: { position: 'absolute', start: 0, end: 0, alignItems: 'center', gap: space.sm },
  toast: {
    flexDirection: 'row', alignItems: 'center', gap: space.sm,
    backgroundColor: colors.surfaceRaised, borderRadius: radius.pill,
    paddingHorizontal: space.lg, paddingVertical: space.md, maxWidth: 480,
    borderWidth: 1, borderColor: colors.border,
  },
  toastText: { ...type.small, color: colors.ink },

  detail: { paddingVertical: space.sm, borderBottomWidth: 1, borderBottomColor: colors.border, gap: 2 },
  detailLabel: { ...type.caption, color: colors.inkMuted },
  detailValue: { ...type.body, color: colors.ink },
}));
