/**
 * React Native CLI configuration.
 *
 * `assets` points the CLI's asset linker (`npx react-native-asset`) at the
 * bundled font files (Inter and Plus Jakarta Sans, KNOT-020a). Running that
 * command copies each `.ttf` into the native projects (Android
 * `app/src/main/assets/fonts`, iOS `Resources`); the fonts are then addressable
 * from `fontFamily` by their file name, which is why the theme uses bare names
 * like `'Inter'` and `'Plus Jakarta Sans'`.
 *
 * Linking is a one-time, founder-side step followed by a native rebuild — see
 * docs/DESIGN_SYSTEM.md and docs/DEVELOPMENT.md.
 */
module.exports = {
  assets: ['./src/assets/fonts'],
};
