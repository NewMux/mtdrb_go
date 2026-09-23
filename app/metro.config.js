/**
 * Metro's config.
 *
 * Two things, both about what ends up in the bundle.
 *
 * expo-sqlite runs SQLite as WebAssembly on the web, and Metro does not treat
 * `.wasm` as an asset by default, so the bundle fails to resolve it. Native
 * builds never reach this code.
 *
 * And the demo's modules — an in-page SQLite engine and half a megabyte of
 * recorded practice — resolve to nothing unless EXPO_PUBLIC_DEMO=1. The app
 * only requires them behind that flag, but Metro collects every require it
 * can see before any dead branch is dropped, so the flag alone kept them in
 * every build.
 */

const { getDefaultConfig } = require('expo/metro-config');

const config = getDefaultConfig(__dirname);
config.resolver.assetExts.push('wasm');

// The flag is inlined into the code at transform time, and Metro's cache
// does not know about environment variables: a normal build made after a
// demo build on the same machine reused the demo's transforms, took the demo
// branch, and found its modules resolved to nothing — a white page. The flag
// is part of the cache's identity instead.
config.cacheVersion = `demo-${process.env.EXPO_PUBLIC_DEMO === '1' ? '1' : '0'}`;

const DEMO_ONLY = new Set(['@/demo/sqljs', '@/demo/replay', '@/demo/fixtures/recording.json']);

if (process.env.EXPO_PUBLIC_DEMO !== '1') {
  const upstream = config.resolver.resolveRequest;
  config.resolver.resolveRequest = (context, moduleName, platform) => {
    if (DEMO_ONLY.has(moduleName)) return { type: 'empty' };
    return (upstream ?? context.resolveRequest)(context, moduleName, platform);
  };
}

module.exports = config;
