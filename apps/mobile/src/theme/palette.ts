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
 * Do not hard-code these hex values anywhere else; `palette.ts` is the one place
 * they are allowed to appear (enforced by the token parity test).
 */
export const palette = {
  light: {
    canvas: '#FFFFFF',
    paper: '#FAFAFB',
    sunken: '#F5F5F7',
    line: '#E8EAEE',
    ink900: '#0F172A',
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
  dark: {
    canvas: '#0D0F16',
    paper: '#14171F',
    sheet: '#161A24',
    sunken: '#1A1E29',
    line: '#232733',
    ink900: '#F1F3F8',
    ink700: '#C5CBD8',
    ink500: '#98A2B3',
    ink400: '#6B7487',
    indigoText: '#9D8BFF',
    indigoFill: '#6D4DF0',
    indigoOnTint: '#B6A9FF',
    indigo100: '#2A2350',
    indigo50: '#1E1B38',
    heritageText: '#E5A07A',
    heritageFill: '#2B1D16',
    wonderText: '#6BC4D8',
    wonderFill: '#12262C',
    danger: '#F0786D',
  },
} as const;
