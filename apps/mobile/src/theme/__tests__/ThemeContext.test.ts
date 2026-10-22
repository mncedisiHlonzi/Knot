import { getColors } from '../colors';
import { darkTheme, lightTheme, resolveInitialTheme, themeFor } from '../ThemeContext';
import { ThemeProvider } from '../ThemeContext';
import type { ThemeContextValue } from '../ThemeContext';
import { saveTheme } from '../themeStorage';

// The provider owns state and persistence, and these suites have no renderer, so
// its hooks are scripted: `useState` returns the lazy initial value (which is
// what the app would compute on launch), and everything else is called straight
// through.
const mockSetState = jest.fn();

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useMemo: (factory: () => unknown) => factory(),
  useState: (initial: unknown) => [
    typeof initial === 'function' ? (initial as () => unknown)() : initial,
    mockSetState,
  ],
  useContext: () => jest.requireActual('../ThemeContext').lightTheme,
}));

jest.mock('../themeStorage', () => ({
  loadTheme: jest.fn(async () => null),
  saveTheme: jest.fn(async () => undefined),
}));

/** The value the provider handed the context on its last render. */
function providedValue(systemTheme?: 'light' | 'dark' | null): ThemeContextValue {
  const element = ThemeProvider({ children: undefined, systemTheme });
  return element.props.value as ThemeContextValue;
}

describe('resolveInitialTheme', () => {
  it('prefers a stored choice over the system appearance', () => {
    expect(resolveInitialTheme('light', 'dark')).toBe('light');
    expect(resolveInitialTheme('dark', 'light')).toBe('dark');
  });

  it('follows the system appearance when nothing is stored', () => {
    expect(resolveInitialTheme(null, 'dark')).toBe('dark');
    expect(resolveInitialTheme(null, 'light')).toBe('light');
  });

  it('falls back to light when the system appearance is unknown', () => {
    expect(resolveInitialTheme(null, null)).toBe('light');
    expect(resolveInitialTheme(null, undefined)).toBe('light');
  });
});

describe('themeFor', () => {
  it('pairs each name with its own roles', () => {
    expect(themeFor('light')).toBe(lightTheme);
    expect(themeFor('dark')).toBe(darkTheme);
  });

  it('uses the shared, cached colour objects so stylesheets can be memoised', () => {
    expect(lightTheme.colors).toBe(getColors('light'));
    expect(darkTheme.colors).toBe(getColors('dark'));
  });
});

describe('ThemeProvider', () => {
  beforeEach(() => {
    mockSetState.mockClear();
    jest.mocked(saveTheme).mockClear();
  });

  it('starts on the light theme when the system appearance is unknown', () => {
    const value = providedValue();

    expect(value.theme).toBe('light');
    expect(value.colors).toBe(getColors('light'));
  });

  it('starts on the dark theme when the system is dark', () => {
    const value = providedValue('dark');

    expect(value.theme).toBe('dark');
    expect(value.colors).toBe(getColors('dark'));
  });

  it('applies a chosen theme and persists it', () => {
    const value = providedValue();

    value.setTheme('dark');

    expect(mockSetState).toHaveBeenCalledWith('dark');
    expect(saveTheme).toHaveBeenCalledWith('dark');
  });

  it('toggles from light to dark', () => {
    const value = providedValue('light');

    value.toggleTheme();

    expect(mockSetState).toHaveBeenCalledWith('dark');
    expect(saveTheme).toHaveBeenCalledWith('dark');
  });

  it('toggles from dark back to light', () => {
    const value = providedValue('dark');

    value.toggleTheme();

    expect(mockSetState).toHaveBeenCalledWith('light');
    expect(saveTheme).toHaveBeenCalledWith('light');
  });
});

describe('the dark role set', () => {
  const dark = getColors('dark');
  const light = getColors('light');

  it('paints the dark canvas, paper and sheet from the dark palette', () => {
    expect(dark.bg.canvas).toBe('#0D0F16');
    expect(dark.bg.paper).toBe('#14171F');
    expect(dark.bg.sheet).toBe('#161A24');
    expect(dark.bg.canvas).not.toBe(light.bg.canvas);
  });

  it('inverts the inks so text stays readable', () => {
    expect(dark.text.primary).toBe('#F1F3F8');
    expect(dark.text.secondary).toBe('#C5CBD8');
    expect(dark.text.tertiary).toBe('#98A2B3');
  });

  it('resolves the brand accent from the dark palette names', () => {
    // Dark has no indigo600/700/500: the accent used as ink is `indigoText` and
    // the pressed state BRIGHTENS to `indigoOnTint`.
    expect(dark.brand.primary).toBe('#9D8BFF');
    expect(dark.brand.pressed).toBe('#B6A9FF');
    expect(dark.brand.tint).toBe('#2A2350');
    expect(dark.pillar.heritage.text).toBe('#E5A07A');
  });

  it('draws `text.onBrand` in the dark canvas ink, which reads on the light accent', () => {
    expect(dark.text.onBrand).toBe(dark.bg.canvas);
  });

  it('keeps the divider one step off the hairline in both themes', () => {
    expect(dark.border.divider).not.toBe(dark.border.hairline);
    expect(light.border.divider).not.toBe(light.border.hairline);
    expect(dark.border.divider).not.toBe(light.border.divider);
  });

  it('caches one object per theme, so repeated reads are identity-stable', () => {
    expect(getColors('dark')).toBe(dark);
    expect(getColors('light')).toBe(light);
  });
});
