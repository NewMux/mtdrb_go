/**
 * Metro's config.
 *
 * One line of it, and only for the web build: expo-sqlite runs SQLite as
 * WebAssembly there, and Metro does not treat `.wasm` as an asset by default,
 * so the bundle fails to resolve it. Native builds never reach this code.
 */

const { getDefaultConfig } = require('expo/metro-config');

const config = getDefaultConfig(__dirname);
config.resolver.assetExts.push('wasm');

module.exports = config;
