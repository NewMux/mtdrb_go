/**
 * Where the tokens live on the device.
 *
 * The refresh token is the one credential worth stealing — it mints access
 * tokens for as long as the family survives — so it goes in the keychain, not
 * in AsyncStorage next to the cached roster.
 *
 * SecureStore has no implementation on web. Rather than pretend, the fallback
 * is explicitly in-memory and explicitly lost on reload: a trainer on the web
 * build signs in again after a refresh, which is honest, where silently
 * writing a refresh token into localStorage would not be.
 */

import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import type { TokenStore } from '@/api/client';

const ACCESS_KEY = 'coachpulse.access_token';
const REFRESH_KEY = 'coachpulse.refresh_token';

const memory = new Map<string, string>();

const usable = Platform.OS !== 'web';

async function read(key: string): Promise<string | null> {
  if (!usable) return memory.get(key) ?? null;
  try {
    return await SecureStore.getItemAsync(key);
  } catch {
    // A locked or unavailable keychain reads as signed out rather than
    // crashing the app on launch.
    return null;
  }
}

async function write(key: string, value: string | null): Promise<void> {
  if (!usable) {
    if (value === null) memory.delete(key);
    else memory.set(key, value);
    return;
  }
  if (value === null) await SecureStore.deleteItemAsync(key);
  else await SecureStore.setItemAsync(key, value);
}

export const secureTokens: TokenStore = {
  accessToken: () => read(ACCESS_KEY),
  refreshToken: () => read(REFRESH_KEY),
  async save(access, refresh) {
    await write(ACCESS_KEY, access);
    await write(REFRESH_KEY, refresh);
  },
  async clear() {
    await write(ACCESS_KEY, null);
    await write(REFRESH_KEY, null);
  },
};
