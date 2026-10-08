import { colors, elevation, fontSizes, fonts, radius, spacing } from '../index';

const HEX = /^#[0-9A-Fa-f]{6}$/;

function isStrictlyIncreasing(values: readonly number[]): boolean {
  return values.every((value, index) => index === 0 || value > values[index - 1]);
}

describe('design tokens', () => {
  it('exposes the canonical brand anchors', () => {
    expect(colors.brand.purple).toBe('#7B3FE4');
    expect(colors.text.primary).toBe('#FFFFFF');
    expect(colors.text.inverse).toBe('#0F172A');
    expect(colors.bg.primary).toBe('#0F172A');
    expect(colors.gradient.primaryStart).toBe('#6B46FF');
    expect(fonts.primary).toBe('Inter');
  });

  it('never uses a placeholder hex value', () => {
    const values = [
      colors.brand.magenta,
      colors.text.secondary,
      colors.bg.primary,
      colors.state.error,
      colors.gradient.warmEnd,
    ];
    for (const value of values) {
      expect(value).toMatch(HEX);
    }
  });

  it('defines four elevation levels', () => {
    expect(Object.keys(elevation)).toHaveLength(4);
  });

  it('keeps the spacing, radius and font scales strictly increasing', () => {
    const space = [
      spacing.xs,
      spacing.sm,
      spacing.md,
      spacing.lg,
      spacing.xl,
      spacing['2xl'],
      spacing['3xl'],
    ];
    const corners = [radius.sm, radius.md, radius.lg, radius.xl, radius['2xl'], radius.pill];
    const sizes = [
      fontSizes.xs,
      fontSizes.sm,
      fontSizes.base,
      fontSizes.md,
      fontSizes.lg,
      fontSizes.xl,
      fontSizes['2xl'],
    ];
    expect(isStrictlyIncreasing(space)).toBe(true);
    expect(isStrictlyIncreasing(corners)).toBe(true);
    expect(isStrictlyIncreasing(sizes)).toBe(true);
  });
});
