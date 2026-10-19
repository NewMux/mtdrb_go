/**
 * Message templates.
 *
 * Trainers already talk to clients on WhatsApp and SMS, and a client app is
 * not something they asked for. So rather than a messaging backend, the app
 * writes the message — with the client's name, balance and next session filled
 * in — and hands it to the app the trainer already uses.
 */

import { Linking, Platform } from 'react-native';

export interface MessageContext {
  firstName: string;
  trainerName: string;
  creditsRemaining: number;
  nextSession?: string | null;
  owed?: string | null;
}

export interface MessageTemplate {
  key: string;
  label: string;
  build: (ctx: MessageContext) => string;
}

export const MESSAGE_TEMPLATES: MessageTemplate[] = [
  {
    key: 'renewal',
    label: 'Pack running low',
    build: (c) => c.creditsRemaining > 0
      ? `Hi ${c.firstName}! Just a heads-up that you have ${c.creditsRemaining} session${c.creditsRemaining === 1 ? '' : 's'} left on your pack. Want me to set up the next one so we keep the momentum going? — ${c.trainerName}`
      : `Hi ${c.firstName}! Your training pack is all used up — great work getting through it. Shall I set up the next one so we can keep going? — ${c.trainerName}`,
  },
  {
    key: 'reminder',
    label: 'Session reminder',
    build: (c) => c.nextSession
      ? `Hi ${c.firstName}, looking forward to our session ${c.nextSession}. See you then! — ${c.trainerName}`
      : `Hi ${c.firstName}, when suits you for our next session this week? — ${c.trainerName}`,
  },
  {
    key: 'missed',
    label: 'Missed you',
    build: (c) => `Hi ${c.firstName}, missed you at training! Everything OK? Let me know when you'd like to get back on the schedule. — ${c.trainerName}`,
  },
  {
    key: 'invoice',
    label: 'Payment reminder',
    build: (c) => `Hi ${c.firstName}, a friendly reminder that ${c.owed ?? 'your invoice'} is still outstanding. The payment details are on the invoice link I sent. Thanks! — ${c.trainerName}`,
  },
  {
    key: 'progress',
    label: 'Great progress',
    build: (c) => `Hi ${c.firstName}, just wanted to say — your progress lately has been brilliant. Proud of the work you're putting in! — ${c.trainerName}`,
  },
];

/** Digits only, keeping a leading plus — the form wa.me and sms: both accept. */
function normalisePhone(phone: string): string {
  const trimmed = phone.trim();
  return (trimmed.startsWith('+') ? '+' : '') + trimmed.replace(/\D/g, '');
}

export async function sendMessage(channel: 'whatsapp' | 'sms', phone: string | null, text: string): Promise<boolean> {
  const number = phone ? normalisePhone(phone) : '';
  const url = channel === 'whatsapp'
    ? `https://wa.me/${number.replace('+', '')}?text=${encodeURIComponent(text)}`
    : `sms:${number}${Platform.OS === 'ios' ? '&' : '?'}body=${encodeURIComponent(text)}`;
  try {
    await Linking.openURL(url);
    return true;
  } catch {
    return false;
  }
}
