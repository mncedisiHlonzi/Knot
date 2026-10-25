import { NativeModules, Platform } from 'react-native';

/**
 * The bridge to the app-local Android module that paints the system navigation
 * bar (`SystemBarsModule.kt`).
 *
 * React Native's `StatusBar` reaches the status bar only, so the navigation bar —
 * which the Android window paints from the native theme — cannot follow a theme
 * the user chooses inside the app. The module is deliberately app-local: it is a
 * few API-gated lines, and adding a dependency for it would be more surface than
 * the problem deserves (KNOT-020j).
 */
type SystemBarsNativeModule = {
  /** Paints the bar `color` (`#RRGGBB`) with dark icons when `lightIcons` is true. */
  readonly setNavigationBar: (color: string, lightIcons: boolean) => void;
  /**
   * Forces the status bar's icon contrast AND its background colour (`true` =
   * white icons) on the Activity and on every open modal dialog.
   *
   * Optional because a JavaScript reload against a build that predates the method
   * must not throw.
   */
  readonly setStatusBarIcons?: (lightContent: boolean, background: string) => void;
  /** Hands the status bar back to React Native's own `StatusBar`. */
  readonly clearStatusBarIcons?: () => void;
};

const nativeModule = NativeModules.KnotSystemBars as SystemBarsNativeModule | undefined;

/**
 * Repaints the Android navigation bar to match the active theme.
 *
 * A no-op on iOS, where there is no such bar, and on a build that predates the
 * module — so callers can invoke it unconditionally from a theme effect.
 *
 * @param color the theme's canvas colour, `colors.bg.canvas`.
 * @param lightIcons true when the canvas is light and the bar needs dark icons.
 */
export function setNavigationBarColor(color: string, lightIcons: boolean): void {
  if (Platform.OS !== 'android' || nativeModule === undefined) {
    return;
  }
  nativeModule.setNavigationBar(color, lightIcons);
}

/**
 * Forces the status bar's icons to white (`true`) or dark (`false`) **and** paints
 * its background, on the Activity and on every open modal dialog.
 *
 * React Native's `StatusBar` reaches the Activity window only. A `Modal` is a real
 * Android `Dialog` with its own window, and React Native copies the Activity's
 * status-bar appearance into that window once, when the dialog is shown — so a
 * `<StatusBar barStyle="light-content" />` declared *inside* a modal cannot turn
 * the icons white on the dialog that is already up. The full-screen media viewer
 * needs exactly that: white icons over its black room, whether the page is an
 * image or a video, in either app theme (KNOT-020k-fix8).
 *
 * The background matters just as much, and it is why this takes a colour: the
 * viewer used to force only the icon appearance, leaving the bar's fill to the
 * window's theme — `@color/knot_canvas` (white) on any screen that had not already
 * declared a transparent status bar of its own. Painting the colour here makes the
 * result independent of the screen the viewer was opened from (KNOT-020k-fix15).
 *
 * A no-op on iOS, where the modal is a view controller and `StatusBar` already
 * applies app-wide, and on a build that predates the module.
 */
export function setStatusBarIcons(lightContent: boolean, background: string): void {
  if (Platform.OS !== 'android' || nativeModule === undefined) {
    return;
  }
  const setter = nativeModule.setStatusBarIcons;
  if (typeof setter !== 'function') {
    return;
  }
  setter(lightContent, background);
}

/**
 * Hands the status bar back to React Native's own `StatusBar`.
 *
 * This is what the media viewer calls on close: the icons become React Native's
 * own again, and the module repaints the bar in the `canvas` colour JavaScript has
 * already restored for the navigation bar — so nothing is left black behind the
 * viewer (KNOT-020k-fix8, KNOT-020k-fix10, KNOT-020k-fix15).
 *
 * It is a separate call rather than a `null` argument because the bridge cannot
 * marshal a null into the native `boolean` parameter.
 *
 * A no-op on iOS and on a build that predates the module.
 */
export function clearStatusBarIcons(): void {
  if (Platform.OS !== 'android' || nativeModule === undefined) {
    return;
  }
  const clear = nativeModule.clearStatusBarIcons;
  if (typeof clear !== 'function') {
    return;
  }
  clear();
}
