/**
 * Names a signed-in device the way its owner would: "Safari on iPhone", not
 * a forty-token user-agent string.
 *
 * Deliberately coarse. The trainer's question on the devices list is "is
 * that one mine?", and the browser and the kind of machine answer it; a
 * version number does not.
 */

export interface DeviceName {
  /** Null when the device is the CoachPulse app itself. */
  browser: string | null;
  system: string | null;
  kind: 'phone' | 'computer';
}

export function describeUserAgent(ua: string): DeviceName | null {
  if (!ua.trim()) return null;
  const system =
    /iPhone/.test(ua) ? 'iPhone' :
    /iPad/.test(ua) ? 'iPad' :
    /Android/.test(ua) ? 'Android' :
    /Mac OS X|Macintosh/.test(ua) ? 'macOS' :
    /Windows/.test(ua) ? 'Windows' :
    /Linux/.test(ua) ? 'Linux' : null;
  const kind: DeviceName['kind'] = /iPhone|Android|Mobile/.test(ua) ? 'phone' : 'computer';

  // The native app talks through the platform's HTTP stack, which names
  // itself rather than a browser.
  if (/Expo|okhttp|CFNetwork|CoachPulse/.test(ua)) return { browser: null, system, kind: 'phone' };

  const browser =
    /Edg\//.test(ua) ? 'Edge' :
    /OPR\//.test(ua) ? 'Opera' :
    /Firefox\//.test(ua) ? 'Firefox' :
    /Chrome\//.test(ua) ? 'Chrome' :
    /Safari\//.test(ua) ? 'Safari' : null;
  if (!browser && !system) return null;
  return { browser, system, kind };
}
