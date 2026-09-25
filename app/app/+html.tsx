/**
 * The HTML shell of the web build.
 *
 * Every page is pre-rendered before any preference is known, so without this
 * a trainer who chose Arabic and dark would watch the page paint English,
 * left to right and white, then jump. The inline script runs before the
 * bundle: it reads the preferences the app mirrors to localStorage (see
 * WEB_PREFS_KEY in src/state/preferences.tsx) and sets direction, language
 * and background on the document first.
 */

import React from 'react';
import { ScrollViewStyleReset } from 'expo-router/html';

const PREPAINT = `
(function () {
  try {
    var prefs = JSON.parse(localStorage.getItem('coachpulse.prefs') || '{}');
    var lang = prefs.locale || ((navigator.language || 'en').slice(0, 2) === 'ar' ? 'ar' : 'en');
    var dark = prefs.theme === 'dark' || (prefs.theme !== 'light' && !(window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches));
    var root = document.documentElement;
    root.lang = lang;
    root.dir = lang === 'ar' ? 'rtl' : 'ltr';
    root.style.colorScheme = dark ? 'dark' : 'light';
    root.style.backgroundColor = dark ? '#121212' : '#f6f6f3';
  } catch (e) {}
})();
`;

export default function Root({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta httpEquiv="X-UA-Compatible" content="IE=edge" />
        <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no" />
        <meta name="theme-color" content="#121212" />
        <ScrollViewStyleReset />
        <script dangerouslySetInnerHTML={{ __html: PREPAINT }} />
        <style dangerouslySetInnerHTML={{ __html: 'html,body{background-color:inherit}body{margin:0}' }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
