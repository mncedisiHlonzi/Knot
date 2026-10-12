import { readFileSync } from 'fs';
import { join } from 'path';

import { colors, elevation, fonts, motion, palette, radius, spacing, typeScale } from '../index';

/**
 * These tests pin the mobile theme to the design specification. They read
 * `docs/design-reference/tokens.json` straight from disk — the single source of
 * truth — and assert every token matches it. If a value drifts here or in the
 * spec, the suite fails, so the two can never silently diverge (KNOT-020a).
 */
type TokensReference = {
  light: Record<string, string>;
  dark: Record<string, string>;
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

describe('design tokens match docs/design-reference/tokens.json', () => {
  it('palette light and dark match the reference exactly', () => {
    expect(palette.light).toEqual(reference.light);
    expect(palette.dark).toEqual(reference.dark);
  });

  it('semantic colours resolve to the active (light) palette', () => {
    expect(colors.bg.canvas).toBe(reference.light.canvas);
    expect(colors.bg.paper).toBe(reference.light.paper);
    expect(colors.bg.sunken).toBe(reference.light.sunken);
    expect(colors.text.primary).toBe(reference.light.ink900);
    expect(colors.text.secondary).toBe(reference.light.ink700);
    expect(colors.text.tertiary).toBe(reference.light.ink500);
    expect(colors.text.disabled).toBe(reference.light.ink400);
    expect(colors.border.hairline).toBe(reference.light.line);
    expect(colors.brand.primary).toBe(reference.light.indigo600);
    expect(colors.brand.pressed).toBe(reference.light.indigo700);
    expect(colors.brand.focus).toBe(reference.light.indigo500);
    expect(colors.brand.tint).toBe(reference.light.indigo100);
    expect(colors.brand.surface).toBe(reference.light.indigo50);
    expect(colors.pillar.heritage.text).toBe(reference.light.heritageText);
    expect(colors.pillar.heritage.fill).toBe(reference.light.heritageFill);
    expect(colors.pillar.wonder.text).toBe(reference.light.wonderText);
    expect(colors.pillar.wonder.fill).toBe(reference.light.wonderFill);
    expect(colors.state.danger).toBe(reference.light.danger);
  });

  it('keeps the deprecated legacy color aliases resolving', () => {
    expect(colors.brand.purple).toBe(reference.light.indigo600);
    expect(colors.brand.magenta).toBe(reference.light.indigo600);
    expect(colors.bg.primary).toBe(reference.light.canvas);
    expect(colors.bg.secondary).toBe(reference.light.sunken);
    expect(colors.bg.surface).toBe(reference.light.paper);
    expect(colors.text.brand).toBe(reference.light.indigo600);
    expect(colors.border.subtle).toBe(reference.light.line);
    expect(colors.border.default).toBe(reference.light.ink400);
    expect(colors.state.error).toBe(reference.light.danger);
    expect(colors.overlay).toBe('rgba(15, 23, 42, 0.7)');
  });

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

  it('every type-scale entry matches the reference', () => {
    expect(Object.keys(typeScale).sort()).toEqual(Object.keys(reference.type).sort());

    for (const [name, spec] of Object.entries(reference.type)) {
      const expected = parseTypeSpec(spec);
      const actual = typeScale[name as keyof typeof typeScale];
      expect(actual.fontSize).toBe(expected.fontSize);
      expect(actual.lineHeight).toBe(expected.lineHeight);
      expect(actual.fontWeight).toBe(expected.weight);
      expect(actual.fontFamily).toBe(expected.family);
    }

    expect(typeScale.overline.textTransform).toBe('uppercase');
  });

  it('every elevation level matches the reference', () => {
    expect(Object.keys(elevation).sort()).toEqual(Object.keys(reference.elevation).sort());

    for (const [level, spec] of Object.entries(reference.elevation)) {
      const expected = parseFirstShadow(spec);
      const actual = elevation[Number(level) as 1 | 2 | 3 | 4];
      expect(actual.shadowColor).toBe('#0F172A');
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
