import { readFileSync } from 'fs';
import { join } from 'path';

import {
  colors,
  elevation,
  fonts,
  getColors,
  grey,
  motion,
  palette,
  radius,
  spacing,
  typeScale,
} from '../index';

/**
 * These tests pin the mobile theme to its two sources of truth, which are now
 * deliberately different things (KNOT-020k):
 *
 * 1. **Colour is the monochrome system, pinned here.** The indigo design
 *    reference is retired for colour (KNOT-ADR-096): `docs/design-reference/tokens.json`
 *    no longer describes the palette, so the light and dark contracts below are
 *    asserted directly against `palette.ts`. Every value the founder specified is
 *    checked in both modes.
 * 2. **Geometry, type, elevation and the motion durations still come from the
 *    reference**, which is read from disk and compared as before. The one
 *    exception remains the type-scale weights, which are one step heavier for
 *    every token but the three body styles (KNOT-020c-fix6).
 *
 * The elevation test also pins the shadow ink to pure black: a monochrome app
 * cannot cast a navy shadow (KNOT-020k).
 */
type TokensReference = {
  fonts: { ui: string; display: string };
  spacing: number[];
  radius: number[];
  type: Record<string, string>;
  elevation: Record<string, string>;
  motion: {
    standard: string;
    enter: string;
    exit: string;
    durationsMs: Record<string, number>;
  };
};

const referencePath = join(
  __dirname,
  '..',
  '..',
  '..',
  '..',
  '..',
  'docs',
  'design-reference',
  'tokens.json',
);

const reference = JSON.parse(readFileSync(referencePath, 'utf8')) as TokensReference;

/** Pulls the four numbers out of a `cubic-bezier(a,b,c,d)` string. */
function parseCubicBezier(value: string): number[] {
  const match = /cubic-bezier\(([^)]+)\)/.exec(value);
  if (match === null) {
    throw new Error(`not a cubic-bezier: ${value}`);
  }
  return match[1].split(',').map((part) => Number(part.trim()));
}

/** Parses the geometry and alpha of the first `x y blur [spread] rgba(...)` shadow. */
function parseFirstShadow(value: string): { y: number; blur: number; opacity: number } {
  const geometry = /^(-?\d+)\s+(-?\d+)\s+(-?\d+)/.exec(value.trim());
  const alpha = /rgba\([^)]*?,\s*([0-9.]+)\)/.exec(value);
  if (geometry === null || alpha === null) {
    throw new Error(`unparseable elevation: ${value}`);
  }
  return { y: Number(geometry[2]), blur: Number(geometry[3]), opacity: Number(alpha[1]) };
}

/** Parses `"32/38 Jakarta 800"` (and `"11/14 Inter 600 +0.08em caps"`). */
function parseTypeSpec(spec: string): {
  fontSize: number;
  lineHeight: number;
  family: string;
  weight: string;
} {
  const match = /^(\d+)\/(\d+)\s+(Jakarta|Inter)\s+(\d+)/.exec(spec);
  if (match === null) {
    throw new Error(`unparseable type spec: ${spec}`);
  }
  return {
    fontSize: Number(match[1]),
    lineHeight: Number(match[2]),
    family: match[3] === 'Jakarta' ? 'Plus Jakarta Sans' : 'Inter',
    weight: match[4],
  };
}

/**
 * The three body-level styles that keep the reference weight (400), so prose keeps
 * its readability (KNOT-020c-fix6).
 */
const BODY_TOKENS = new Set(['reading', 'body', 'bodyS']);

/**
 * One step heavier, per the KNOT-020c-fix6 typography bump. The reference file is
 * a static design snapshot and is deliberately not edited, so the heavier mobile
 * weight is derived from the reference spec rather than hard-coded per token —
 * which keeps both sides honest: a change in either the spec or the scale fails the
 * suite.
 */
const ONE_STEP_HEAVIER: Readonly<Record<string, string>> = {
  '400': '400',
  '500': '600',
  '600': '700',
  '700': '800',
  '800': '800',
};

/** The weight the mobile type scale should carry for a reference type token. */
function expectedMobileWeight(name: string, referenceWeight: string): string {
  return BODY_TOKENS.has(name)
    ? referenceWeight
    : (ONE_STEP_HEAVIER[referenceWeight] ?? referenceWeight);
}

describe('the monochrome colour contract', () => {
  /** The nine primitive steps, light to dark (KNOT-020k). */
  it('exposes the nine-step grey primitive ramp', () => {
    expect(grey).toEqual({
      50: '#FFFFFF',
      100: '#F5F5F5',
      200: '#E5E5E5',
      300: '#BDBDBD',
      400: '#8A8A8A',
      500: '#555555',
      600: '#111111',
      700: '#1F1F1F',
      950: '#000000',
    });
  });

  it('resolves light mode exactly as specified', () => {
    const light = getColors('light');

    expect(light.bg.canvas).toBe('#FFFFFF');
    expect(light.bg.sunken).toBe('#F5F5F5');
    expect(light.bg.raised).toBe('#FFFFFF');
    expect(light.bg.scrim).toBe('rgba(0, 0, 0, 0.5)');
    expect(light.text.primary).toBe('#000000');
    expect(light.text.body).toBe('#111111');
    expect(light.text.secondary).toBe('#555555');
    expect(light.text.tertiary).toBe('#8A8A8A');
    expect(light.text.inverse).toBe('#FFFFFF');
    expect(light.border.hairline).toBe('#E5E5E5');
    expect(light.border.strong).toBe('#BDBDBD');
    expect(light.brand.primary).toBe('#000000');
    expect(light.brand.pressed).toBe('#333333');
    expect(light.brand.tint).toBe('#F0F0F0');
    expect(light.pillar.wonder).toBe('#000000');
    expect(light.pillar.heritage).toBe('#000000');
    expect(light.state.danger).toBe('#B00020');
    expect(light.state.success).toBe('#1B5E20');
    expect(light.state.warning).toBe('#8A5300');
  });

  it('resolves dark mode exactly as specified', () => {
    const dark = getColors('dark');

    expect(dark.bg.canvas).toBe('#000000');
    expect(dark.bg.sunken).toBe('#0A0A0A');
    expect(dark.bg.raised).toBe('#111111');
    expect(dark.bg.scrim).toBe('rgba(0, 0, 0, 0.7)');
    expect(dark.text.primary).toBe('#FFFFFF');
    expect(dark.text.body).toBe('#EDEDED');
    expect(dark.text.secondary).toBe('#A8A8A8');
    expect(dark.text.tertiary).toBe('#6E6E6E');
    expect(dark.text.inverse).toBe('#000000');
    expect(dark.border.hairline).toBe('#1F1F1F');
    expect(dark.border.strong).toBe('#3D3D3D');
    expect(dark.brand.primary).toBe('#FFFFFF');
    expect(dark.brand.pressed).toBe('#E0E0E0');
    expect(dark.brand.tint).toBe('#1A1A1A');
    expect(dark.pillar.wonder).toBe('#FFFFFF');
    expect(dark.pillar.heritage).toBe('#FFFFFF');
    expect(dark.state.danger).toBe('#FF6B6B');
    expect(dark.state.success).toBe('#66BB6A');
    expect(dark.state.warning).toBe('#FFB74D');
  });

  it('keeps the two modes exact mirrors of one another', () => {
    expect(Object.keys(palette.light).sort()).toEqual(Object.keys(palette.dark).sort());
  });

  it('leaves no accent colour anywhere: the brand is the mode ink', () => {
    const light = getColors('light');
    const dark = getColors('dark');

    expect(light.brand.primary).toBe(light.text.primary);
    expect(dark.brand.primary).toBe(dark.text.primary);
    expect(light.brand.primary).toBe(grey[950]);
    expect(dark.brand.primary).toBe(grey[50]);
  });

  it('keeps the deprecated legacy aliases resolving to monochrome roles', () => {
    expect(colors.brand.purple).toBe(colors.brand.primary);
    expect(colors.brand.magenta).toBe(colors.brand.primary);
    expect(colors.bg.primary).toBe(colors.bg.canvas);
    expect(colors.bg.secondary).toBe(colors.bg.sunken);
    expect(colors.bg.surface).toBe(colors.bg.raised);
    expect(colors.text.brand).toBe(colors.brand.primary);
    expect(colors.border.subtle).toBe(colors.border.hairline);
    expect(colors.border.default).toBe(colors.border.strong);
    expect(colors.state.error).toBe(colors.state.danger);
    expect(colors.overlay).toBe(colors.bg.scrim);
  });
});

describe('geometry, type, elevation and motion still match the retired reference', () => {
  it('font families match the reference', () => {
    expect(fonts.ui).toBe(reference.fonts.ui);
    expect(fonts.display).toBe(reference.fonts.display);
    // Legacy alias still resolves to the UI family.
    expect(fonts.primary).toBe(reference.fonts.ui);
  });

  it('the spacing scale matches the reference, in order', () => {
    expect([
      spacing.xs,
      spacing.sm,
      spacing.md,
      spacing.lg,
      spacing.xl,
      spacing.xxl,
      spacing.xxxl,
    ]).toEqual(reference.spacing);
    // Deprecated aliases point at the same steps.
    expect(spacing['2xl']).toBe(32);
    expect(spacing['3xl']).toBe(48);
    expect(spacing.unit).toBe(8);
  });

  it('the radius scale matches the reference, in order', () => {
    expect([radius.xs, radius.sm, radius.md, radius.lg, radius.xl, radius.xxl]).toEqual(
      reference.radius,
    );
    expect(radius.pill).toBe(999);
    expect(radius['2xl']).toBe(24);
  });

  it('every type-scale entry matches the reference, bar the heavier weights', () => {
    expect(Object.keys(typeScale).sort()).toEqual(Object.keys(reference.type).sort());

    for (const [name, spec] of Object.entries(reference.type)) {
      const expected = parseTypeSpec(spec);
      const actual = typeScale[name as keyof typeof typeScale];
      expect(actual.fontSize).toBe(expected.fontSize);
      expect(actual.lineHeight).toBe(expected.lineHeight);
      // The weight is one step heavier than the reference for every token but the
      // body-level three (KNOT-020c-fix6).
      expect(actual.fontWeight).toBe(expectedMobileWeight(name, expected.weight));
      expect(actual.fontFamily).toBe(expected.family);
    }

    expect(typeScale.overline.textTransform).toBe('uppercase');
  });

  it('every elevation level matches the reference', () => {
    expect(Object.keys(elevation).sort()).toEqual(Object.keys(reference.elevation).sort());

    for (const [level, spec] of Object.entries(reference.elevation)) {
      const expected = parseFirstShadow(spec);
      const actual = elevation[Number(level) as 1 | 2 | 3 | 4];
      expect(actual.shadowColor).toBe('#000000');
      expect(actual.shadowOffset).toEqual({ width: 0, height: expected.y });
      expect(actual.shadowRadius).toBe(expected.blur);
      expect(actual.shadowOpacity).toBeCloseTo(expected.opacity, 5);
    }
  });

  it('every motion token matches the reference', () => {
    expect(motion.easing.standard).toEqual(parseCubicBezier(reference.motion.standard));
    expect(motion.easing.enter).toEqual(parseCubicBezier(reference.motion.enter));
    expect(motion.easing.exit).toEqual(parseCubicBezier(reference.motion.exit));
    expect(motion.durations).toEqual(reference.motion.durationsMs);
  });
});
