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
