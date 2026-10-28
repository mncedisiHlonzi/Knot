import { getColors } from '../colors';
import {
  darkTheme,
  lightTheme,
  resolveInitialTheme,
  resolveTheme,
  themeFor,
} from '../ThemeContext';
import { ThemeProvider } from '../ThemeContext';
import type { ThemeContextValue } from '../ThemeContext';
import { saveAppearance } from '../appearanceStorage';

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

jest.mock('../appearanceStorage', () => ({
  loadAppearance: jest.fn(async () => null),
  saveAppearance: jest.fn(async () => undefined),
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

describe('resolveTheme', () => {
  it('follows the device appearance in system mode', () => {
    expect(resolveTheme('system', 'dark')).toBe('dark');
    expect(resolveTheme('system', 'light')).toBe('light');
    expect(resolveTheme('system', null)).toBe('light');
    expect(resolveTheme('system', undefined)).toBe('light');
  });

  it('lets an explicit choice win over the device appearance', () => {
    expect(resolveTheme('light', 'dark')).toBe('light');
    expect(resolveTheme('dark', 'light')).toBe('dark');
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
    jest.mocked(saveAppearance).mockClear();
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
    expect(saveAppearance).toHaveBeenCalledWith('dark');
  });

  it('applies an explicit appearance and persists it', () => {
    const value = providedValue();

    value.setMode('light');

    expect(mockSetState).toHaveBeenCalledWith('light');
    expect(saveAppearance).toHaveBeenCalledWith('light');
  });

  it('can return to system mode, which is a stored choice of its own', () => {
    const value = providedValue();

    value.setMode('system');

    expect(mockSetState).toHaveBeenCalledWith('system');
    expect(saveAppearance).toHaveBeenCalledWith('system');
  });

  it('reports system mode on the context', () => {
    expect(providedValue('dark').mode).toBe('system');
  });

  it('toggles from light to dark', () => {
    const value = providedValue('light');

    value.toggleTheme();

    expect(mockSetState).toHaveBeenCalledWith('dark');
    expect(saveAppearance).toHaveBeenCalledWith('dark');
  });

  it('toggles from dark back to light', () => {
    const value = providedValue('dark');

    value.toggleTheme();

    expect(mockSetState).toHaveBeenCalledWith('light');
    expect(saveAppearance).toHaveBeenCalledWith('light');
  });
});

describe('the dark role set', () => {
  const dark = getColors('dark');
  const light = getColors('light');

  it('paints the dark canvas, sunken and raised surfaces from the dark contract', () => {
    expect(dark.bg.canvas).toBe('#000000');
    expect(dark.bg.sunken).toBe('#0A0A0A');
    expect(dark.bg.raised).toBe('#111111');
    expect(dark.bg.canvas).not.toBe(light.bg.canvas);
  });

  it('inverts the inks so text stays readable', () => {
    expect(dark.text.primary).toBe('#FFFFFF');
    expect(dark.text.body).toBe('#EDEDED');
    expect(dark.text.secondary).toBe('#A8A8A8');
    expect(dark.text.tertiary).toBe('#6E6E6E');
    expect(dark.text.inverse).toBe('#000000');
  });

  it('resolves the brand to the mode ink, with no accent left', () => {
    expect(dark.brand.primary).toBe('#FFFFFF');
    expect(dark.brand.pressed).toBe('#E0E0E0');
    expect(dark.brand.tint).toBe('#1A1A1A');
    expect(dark.pillar.heritage).toBe(dark.text.primary);
    expect(light.pillar.wonder).toBe(light.text.primary);
  });

  it('draws `text.onBrand` in the inverse ink, which reads on the brand fill', () => {
    expect(dark.text.onBrand).toBe(dark.text.inverse);
    expect(light.text.onBrand).toBe(light.text.inverse);
  });

  it('keeps the strong rule one step off the hairline in both modes', () => {
    expect(dark.border.strong).not.toBe(dark.border.hairline);
    expect(light.border.strong).not.toBe(light.border.hairline);
    expect(dark.border.strong).not.toBe(light.border.strong);
  });

  it('caches one object per theme, so repeated reads are identity-stable', () => {
    expect(getColors('dark')).toBe(dark);
    expect(getColors('light')).toBe(light);
  });
});
