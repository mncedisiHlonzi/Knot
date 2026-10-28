/**
 * Raw palette values — the source of truth for every colour in the app.
 *
 * ## The monochrome system (KNOT-020k)
 *
 * Knot is monochrome: pure black and pure white anchor each mode, and a neutral
 * grey ramp carries everything between them. There is no accent colour. The
 * indigo design reference is retired for colour — `docs/design-reference/tokens.json`
 * still pins geometry, type, elevation and motion, but it no longer describes the
 * palette (KNOT-ADR-096).
 *
 * {@link grey} is the primitive ramp: nine documented steps, light to dark. Most
 * semantic values are one of those steps. A handful of values the contract names
 * sit BETWEEN two steps (the dark body ink `#EDEDED`, the pressed fills, the
 * tints); those are declared once in {@link between} so the specified value stays
 * exact, and they are listed beside the ramp in `docs/DESIGN_SYSTEM.md`.
 *
 * Nothing in the UI reads this file directly: screens ask for a semantic role
 * (`text.secondary`, `bg.canvas`) and `colors.ts` resolves it for the active mode.
 * Keeping the raw values separate from the roles is what lets a mode switch
 * repaint the whole app without touching a screen (KNOT-020i).
 *
 * Do not hard-code these hex values anywhere else; `palette.ts` is the one place
 * they are allowed to appear (enforced by the token parity test).
 */
export const grey = {
  50: '#FFFFFF',
  100: '#F5F5F5',
  200: '#E5E5E5',
  300: '#BDBDBD',
  400: '#8A8A8A',
  500: '#555555',
  600: '#111111',
  700: '#1F1F1F',
  950: '#000000',
} as const;

/**
 * The neutral values the contract names that fall between two grey steps.
 *
 * Keeping them named (rather than inline in the two modes) makes the exception
 * countable: five for dark surfaces and inks, four for the brand's pressed and
 * tint states. `DESIGN_SYSTEM.md` lists them as the ramp's sub-steps, and a value
 * may only reach the UI through a semantic role.
 */
const between = {
  /** Dark body ink: between `grey.100` (#F5F5F5) and `grey.50` (#FFFFFF). */
  darkBody: '#EDEDED',
  /** Dark secondary ink: between `grey.300` (#BDBDBD) and `grey.400` (#8A8A8A). */
  darkSecondary: '#A8A8A8',
  /** Dark tertiary ink: between `grey.400` (#8A8A8A) and `grey.500` (#555555). */
  darkTertiary: '#6E6E6E',
  /** Dark strong rule: between `grey.500` (#555555) and `grey.600` (#111111). */
  darkStrong: '#3D3D3D',
  /** Dark sunken surface: between `grey.600` (#111111) and `grey.950` (#000000). */
  darkSunken: '#0A0A0A',
  /** Light pressed fill: between `grey.500` (#555555) and `grey.600` (#111111). */
  lightPressed: '#333333',
  /** Light tint surface: between `grey.100` (#F5F5F5) and `grey.50` (#FFFFFF). */
  lightTint: '#F0F0F0',
  /** Dark pressed fill: between `grey.50` (#FFFFFF) and `grey.100` (#F5F5F5). */
  darkPressed: '#E0E0E0',
  /** Dark tint surface: between `grey.700` (#1F1F1F) and `grey.600` (#111111). */
  darkTint: '#1A1A1A',
} as const;
/**
 * The two modes' raw values.
 *
 * Every neutral value is a {@link grey} step unless it appears in
 * {@link between}. The state colours are the only chromatic values in the system:
 * danger, success and warning must stay recognisable even in a monochrome app,
 * and each is tuned to its mode's canvas.
 */
export const palette = {
  /** Light mode: white canvas, black ink. */
  light: {
    canvas: grey[50],
    sunken: grey[100],
    raised: grey[50],
    scrim: 'rgba(0, 0, 0, 0.5)',
    inkPrimary: grey[950],
    inkBody: grey[600],
    inkSecondary: grey[500],
    inkTertiary: grey[400],
    inkInverse: grey[50],
    line: grey[200],
    lineStrong: grey[300],
    brand: grey[950],
    brandPressed: between.lightPressed,
    brandTint: between.lightTint,
    danger: '#B00020',
    success: '#1B5E20',
    warning: '#8A5300',
  },
  /** Dark mode: black canvas, white ink. */
  dark: {
    canvas: grey[950],
    sunken: between.darkSunken,
    raised: grey[600],
    scrim: 'rgba(0, 0, 0, 0.7)',
    inkPrimary: grey[50],
    inkBody: between.darkBody,
    inkSecondary: between.darkSecondary,
    inkTertiary: between.darkTertiary,
    inkInverse: grey[950],
    line: grey[700],
    lineStrong: between.darkStrong,
    brand: grey[50],
    brandPressed: between.darkPressed,
    brandTint: between.darkTint,
    danger: '#FF6B6B',
    success: '#66BB6A',
    warning: '#FFB74D',
  },
} as const;
