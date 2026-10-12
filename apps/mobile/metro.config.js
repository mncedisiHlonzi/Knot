const { getDefaultConfig, mergeConfig } = require('@react-native/metro-config');

const defaultConfig = getDefaultConfig(__dirname);
const { assetExts, sourceExts } = defaultConfig.resolver;

/**
 * Metro configuration
 * https://reactnative.dev/docs/metro
 *
 * `react-native-svg-transformer` (KNOT-020b-fix) turns an imported `.svg` into a
 * React component. It needs two changes:
 *
 *   - the Babel transformer is swapped for the SVG-aware one, which rewrites an
 *     SVG file into a `react-native-svg` component tree, and
 *   - `svg` moves out of `assetExts` (binary assets, copied verbatim) into
 *     `sourceExts` (JS modules), so an `import` of a `.svg` is transformed rather
 *     than treated as an opaque asset.
 *
 * This only affects bundling; TypeScript is told about `.svg` imports by
 * `src/svg.d.ts`, and Jest maps them to a stub in `jest.config.js`.
 *
 * @type {import('metro-config').MetroConfig}
 */
const config = {
  transformer: {
    babelTransformerPath: require.resolve('react-native-svg-transformer/react-native'),
  },
  resolver: {
    assetExts: assetExts.filter((ext) => ext !== 'svg'),
    sourceExts: [...sourceExts, 'svg'],
  },
};

module.exports = mergeConfig(defaultConfig, config);
