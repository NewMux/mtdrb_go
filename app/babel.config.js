/**
 * Metro's Babel config.
 *
 * `babel-preset-expo` carries the expo-router plugin and the JSX transform;
 * nothing else is needed, because the `@/` alias comes from tsconfig paths,
 * which Metro reads directly.
 */

module.exports = function (api) {
  api.cache(true);
  return { presets: ['babel-preset-expo'] };
};
