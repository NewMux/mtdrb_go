/**
 * The app's configuration for builds: app.json, plus what depends on the
 * environment a build runs in.
 *
 * A release build talks to one server for its whole life, compiled in from
 * EXPO_PUBLIC_API_URL. Without it the app falls back to localhost, which on a
 * customer's phone is the phone: every request fails and the app looks
 * permanently offline. So a production build refuses to start without an
 * https URL rather than shipping that.
 */

import type { ConfigContext, ExpoConfig } from 'expo/config';

export default ({ config }: ConfigContext): ExpoConfig => {
  const profile = process.env.EAS_BUILD_PROFILE;
  const apiUrl = process.env.EXPO_PUBLIC_API_URL;
  if (profile === 'production' && !(apiUrl && apiUrl.startsWith('https://') && !apiUrl.includes('example.com'))) {
    throw new Error(
      `A production build needs EXPO_PUBLIC_API_URL set to the https address of the API (got ${apiUrl ?? 'nothing'}). ` +
      'Set it in eas.json.');
  }

  // Set once `eas init` has created the project; see LAUNCH.md.
  const projectId = process.env.EAS_PROJECT_ID;
  const owner = process.env.EXPO_OWNER;

  return {
    ...config,
    name: config.name ?? 'CoachPulse',
    slug: config.slug ?? 'coachpulse',
    ...(owner ? { owner } : {}),
    icon: './assets/icon.png',
    // Over-the-air updates only reach builds of the same app version, so a
    // JavaScript change can never land on native code it was not built for.
    runtimeVersion: { policy: 'appVersion' },
    ...(projectId ? { updates: { url: `https://u.expo.dev/${projectId}` } } : {}),
    ios: {
      ...config.ios,
      infoPlist: {
        ...config.ios?.infoPlist,
        // Only HTTPS and the platform's own crypto: no export paperwork.
        ITSAppUsesNonExemptEncryption: false,
      },
    },
    android: {
      ...config.android,
      adaptiveIcon: { foregroundImage: './assets/adaptive-icon.png', backgroundColor: '#121212' },
    },
    web: { ...config.web, favicon: './assets/favicon.png' },
    plugins: [
      ...(config.plugins ?? []),
      ['expo-splash-screen', { image: './assets/splash-icon.png', imageWidth: 160, backgroundColor: '#121212' }],
    ],
    extra: {
      ...config.extra,
      ...(projectId ? { eas: { projectId } } : {}),
    },
  };
};
