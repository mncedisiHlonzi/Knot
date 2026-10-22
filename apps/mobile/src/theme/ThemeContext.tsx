/**
 * The active theme: light or dark (KNOT-020i).
 *
 * `ThemeProvider` owns the choice, persists it, and hands every screen the role
 * set for the active theme. Components never import the static `colors` object:
 * a stylesheet built at module scope would freeze whichever palette was loaded
 * first, so styles are built per theme through {@link useThemedStyles} and
 * one-off colours (an icon tint, an ActivityIndicator) are read from
 * {@link useTheme}.
 *
 * The default is the system appearance on first launch; an explicit choice by
 * the user is stored and wins from then on (KNOT-ADR-077).
 */
import React from 'react';

import { getColors, type ThemeColors, type ThemeName } from './colors';
import { loadTheme, saveTheme } from './themeStorage';

/**
 * The device appearance, as React Native's `Appearance.getColorScheme()` words
 * it: `'light'`, `'dark'`, `null`, or missing entirely.
 *
 * Deliberately declared here rather than imported from `react-native`: this
 * module stays free of React Native so the theme barrel can be imported by a
 * pure module or a unit test without a React Native mock. The root reads the
 * appearance and passes it down.
 */
export type SystemColorScheme = 'light' | 'dark' | null | undefined;

/** A theme name paired with the roles it resolves to. */
export type Theme = {
  readonly theme: ThemeName;
  readonly colors: ThemeColors;
};

/** What {@link useTheme} returns: the active theme plus the controls for it. */
export type ThemeContextValue = Theme & {
  /** Applies and persists a theme. */
  readonly setTheme: (theme: ThemeName) => void;
  /** Applies and persists the other theme. */
  readonly toggleTheme: () => void;
};

/** The light theme. Its `colors` are the same object as the legacy export. */
export const lightTheme: Theme = { theme: 'light', colors: getColors('light') };

/** The dark theme. */
export const darkTheme: Theme = { theme: 'dark', colors: getColors('dark') };

/** Pairs a theme name with its roles. */
export function themeFor(theme: ThemeName): Theme {
  return theme === 'dark' ? darkTheme : lightTheme;
}

/**
 * Resolves the theme to start from: the stored choice, else the system
 * appearance, else light.
 *
 * Split out as a pure function so the rule can be tested without a provider, and
 * so `ThemeProvider` takes the whole decision as one argument.
 */
export function resolveInitialTheme(
  stored: ThemeName | null,
  system: SystemColorScheme,
): ThemeName {
  if (stored !== null) {
    return stored;
  }

  return system === 'dark' ? 'dark' : 'light';
}

/**
 * The context defaults to the light theme with no-op controls.
 *
 * That default IS the fallback the migration needs: a component rendered
 * outside the provider (a test, a preview, a screen mounted before the provider
 * exists) gets a complete, correct light role set and a toggle that does
 * nothing, rather than an undefined dereference.
 */
const ThemeContext = React.createContext<ThemeContextValue>({
  ...lightTheme,
  setTheme: () => undefined,
  toggleTheme: () => undefined,
});

/** Reads the active theme. Available anywhere under `ThemeProvider`. */
export function useTheme(): ThemeContextValue {
  return React.useContext(ThemeContext);
}

/**
 * Stylesheet caches, keyed by the stylesheet factory and then by theme.
 *
 * `StyleSheet.create` is cheap but not free, and a screen stylesheet has dozens
 * of entries, so it is built once per theme and reused. The factory is a
 * module-scope constant in every file that uses this, so it is a stable key.
 */
const styleCaches = new WeakMap<object, WeakMap<object, unknown>>();

/**
 * Builds (once per theme) and returns the stylesheet for the active theme.
 *
 * Usage is a drop-in for a module-scope stylesheet:
 *
 * ```ts
 * const createStyles = (colors: ThemeColors) => StyleSheet.create({ … });
 * const styles = useThemedStyles(createStyles);
 * ```
 *
 * The returned object is the SAME object on every render while the theme is
 * unchanged, so style arrays keep their identity and nothing re-registers.
 */
export function useThemedStyles<T>(create: (colors: ThemeColors) => T): T {
  const { colors } = useTheme();

  let perTheme = styleCaches.get(create);
  if (perTheme === undefined) {
    perTheme = new WeakMap<object, unknown>();
    styleCaches.set(create, perTheme);
  }

  const cached = perTheme.get(colors);
  if (cached !== undefined) {
    return cached as T;
  }

  const built = create(colors);
  perTheme.set(colors, built);
  return built;
}

/**
 * Owns the active theme for the whole app.
 *
 * The stored preference is read once on mount and applied when it arrives, so a
 * dark-mode user does not have to see a light frame first — the provider starts
 * from the system appearance, which is already dark for them. An explicit choice
 * writes through immediately; storage failure is logged and swallowed by
 * `themeStorage`, so the toggle never blocks on disk.
 */
export function ThemeProvider({
  children,
  systemTheme,
}: {
  readonly children: React.ReactNode;
  /** The device appearance on launch; see {@link resolveInitialTheme}. */
  readonly systemTheme?: SystemColorScheme;
}): React.ReactElement {
  const [theme, setThemeState] = React.useState<ThemeName>(() =>
    resolveInitialTheme(null, systemTheme),
  );

  React.useEffect(() => {
    let cancelled = false;

    void loadTheme().then((stored) => {
      if (!cancelled && stored !== null) {
        setThemeState(stored);
      }
    });

    return () => {
      cancelled = true;
    };
  }, []);

  const setTheme = React.useCallback((next: ThemeName) => {
    setThemeState(next);
    void saveTheme(next);
  }, []);

  const value = React.useMemo<ThemeContextValue>(
    () => ({
      ...themeFor(theme),
      setTheme,
      toggleTheme: () => {
        setTheme(theme === 'dark' ? 'light' : 'dark');
      },
    }),
    [theme, setTheme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
