/**
 * Raw palette values — the source of truth for every colour in the app.
 *
 * These are the literal swatches from `docs/design-reference/tokens.json`, for
 * both themes. Nothing in the UI reads them directly: screens use the semantic
 * aliases in `colors.ts`, which map a role (`bg.canvas`, `text.primary`) onto a
 * palette value. Keeping the raw values separate from the roles is what lets
 * KNOT-020i activate the dark theme without touching a single screen — it swaps
 * which palette `colors.ts` reads.
 *
 * Knot is light-first (KNOT-020a): `colors.ts` reads `palette.light` today. The
 * dark palette is defined here now so the token layer supports both, but dark
 * activation is deferred to KNOT-020i.
 *
 * Primary text (`ink900`) is pure: `#000000` on the light canvas and `#FFFFFF` on
 * the dark one, so a story body reads at maximum crispness the way X's does rather
 * than as a softened ink (KNOT-020k-fix19).
 *
 * Do not hard-code these hex values anywhere else; `palette.ts` is the one place
 * they are allowed to appear (enforced by the token parity test).
 */
export const palette = {
  light: {
    canvas: '#FFFFFF',
    paper: '#FAFAFB',
    sunken: '#F5F5F7',
    line: '#E8EAEE',
    ink900: '#000000',
    ink700: '#334155',
    ink500: '#64748B',
    ink400: '#94A3B8',
    indigo600: '#5B35E8',
    indigo700: '#4723C4',
    indigo500: '#7152F0',
    indigo100: '#E9E4FD',
    indigo50: '#F5F3FE',
    heritageText: '#9A4A1F',
    heritageFill: '#FBF1EA',
    wonderText: '#0F5F73',
    wonderFill: '#E8F3F6',
    danger: '#C2362B',
  },
  /**
   * Classic, neutral dark mode (KNOT-020k-fix18).
   *
   * Surfaces are pure or near-pure grey — black canvas, a lifted neutral panel,
   * a recessed field tone, a neutral hairline — with no blue or violet cast. The
   * previous ramp carried a navy cast in every surface, which is exactly what
   * the founder asked to replace with the neutral dark theme of X/Twitter and
   * LinkedIn.
   *
   * The accent roles are deliberately UNCHANGED (`indigoText` / `indigoFill` /
   * `indigoOnTint`): the brand purple is an accent, not a surface, and in a dark
   * theme it has to stay legible both as ink on black and as a fill under the
   * black `text.onBrand` label. Light-mode `indigo600` (`#5B35E8`) is only about
   * 3.1:1 against black, so it remains the light theme's fill.
   */
  dark: {
    canvas: '#000000',
    paper: '#16181C',
    sheet: '#22252A',
    sunken: '#1E2024',
    line: '#2F3336',
    ink900: '#FFFFFF',
    ink700: '#71767B',
    ink500: '#536471',
    ink400: '#3F4448',
    indigoText: '#9D8BFF',
    indigoFill: '#6D4DF0',
    indigoOnTint: '#B6A9FF',
    indigo100: '#2A2350',
    indigo50: '#1E1B38',
    heritageText: '#E5A07A',
    heritageFill: '#2B1D16',
    wonderText: '#6BC4D8',
    wonderFill: '#12262C',
    danger: '#F4212E',
  },
} as const;
