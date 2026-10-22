/**
 * Global Jest setup (KNOT-020i).
 *
 * Components now read the active theme through `useTheme()`, which is
 * `React.useContext`. This suite has no renderer — component tests CALL the
 * component directly (see the component-test pattern in `docs/DEVELOPMENT.md`)
 * — and outside a render there is no React dispatcher, so `useContext` throws
 * "Invalid hook call" before any assertion runs.
 *
 * It is therefore replaced here, once, for every suite: reading the context
 * returns the default (light) theme, which is exactly what the context falls
 * back to when a component renders outside a provider. Everything else comes
 * from the real `react`, so suites that script their own hooks are unaffected —
 * a suite's own `jest.mock('react', …)` replaces this one wholesale, and those
 * factories must then supply `useContext` themselves.
 *
 * `requireActual` is called lazily, inside the returned function, so the theme
 * module (which imports `react` itself) is only loaded when a component actually
 * asks for the theme.
 */
jest.mock('react', () => {
  const actual = jest.requireActual('react');

  return {
    ...actual,
    useContext: () => jest.requireActual('./src/theme/ThemeContext').lightTheme,
  };
});
