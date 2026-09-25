/**
 * Where the tokens live on the device.
 *
 * The refresh token is the one credential worth stealing — it mints access
 * tokens for as long as the family survives — so on a phone it goes in the
 * keychain, not in AsyncStorage next to the cached roster.
 *
 * A browser has no keychain, and localStorage is readable by any script on
 * the page. So the web build never holds the refresh token at all: the server
 * sets it as an httpOnly cookie (see COOKIE_REFRESH in src/api/client.ts), and
 * all this store remembers is *that* there is one — a flag, useless to a
 * script that steals it — so a reload knows to try the cookie rather than
 * showing the sign-in screen. The access token stays in memory and is minted
 * afresh from the cookie after a reload.
 */

import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import { COOKIE_REFRESH, type TokenStore } from '@/api/client';

const ACCESS_KEY = 'coachpulse.access_token';
const REFRESH_KEY = 'coachpulse.refresh_token';
/** localStorage: "this browser has a refresh cookie". Never the token. */
const COOKIE_SESSION_KEY = 'coachpulse.cookie_session';

export const isWeb = Platform.OS === 'web';

const memory = new Map<string, string>();

function cookieSession(): boolean {
  try {
    return globalThis.localStorage?.getItem(COOKIE_SESSION_KEY) === '1';
  } catch {
    return false;
  }
}

function setCookieSession(on: boolean): void {
  try {
    if (on) globalThis.localStorage?.setItem(COOKIE_SESSION_KEY, '1');
    else globalThis.localStorage?.removeItem(COOKIE_SESSION_KEY);
  } catch {
    // A browser refusing storage signs in again after a reload; nothing worse.
  }
}

async function read(key: string): Promise<string | null> {
  if (isWeb) return memory.get(key) ?? null;
  try {
    return await SecureStore.getItemAsync(key);
  } catch {
    // A locked or unavailable keychain reads as signed out rather than
    // crashing the app on launch.
    return null;
  }
}

async function write(key: string, value: string | null): Promise<void> {
  if (isWeb) {
    if (value === null) memory.delete(key);
    else memory.set(key, value);
    return;
  }
  if (value === null) await SecureStore.deleteItemAsync(key);
  else await SecureStore.setItemAsync(key, value);
}

export const secureTokens: TokenStore = {
  accessToken: () => read(ACCESS_KEY),
  async refreshToken() {
    const held = await read(REFRESH_KEY);
    if (held) return held;
    return isWeb && cookieSession() ? COOKIE_REFRESH : null;
  },
  async save(access, refresh) {
    await write(ACCESS_KEY, access);
    if (isWeb && refresh === COOKIE_REFRESH) {
      setCookieSession(true);
      return;
    }
    await write(REFRESH_KEY, refresh);
  },
  async clear() {
    await write(ACCESS_KEY, null);
    await write(REFRESH_KEY, null);
    if (isWeb) setCookieSession(false);
  },
};
