import React from 'react';
import type { SvgProps } from 'react-native-svg';

type SvgImageProps = {
  /**
   * The imported SVG component — the default export of a `.svg` file, turned into
   * a React component by `react-native-svg-transformer` (KNOT-020b-fix).
   */
  readonly source: React.ComponentType<SvgProps>;
  /** A square size, in points. Used only when `width`/`height` are not given. */
  readonly size?: number;
  /** An explicit width, in points. Takes precedence over `size`. */
  readonly width?: number;
  /** An explicit height, in points. Takes precedence over `size`. */
  readonly height?: number;
  /** Tints the artwork wherever it is drawn with `currentColor`. */
  readonly color?: string;
  /**
   * Hides the artwork from assistive technology. Pair it with a parent that
   * carries the accessible name, so a decorative glyph is not announced twice.
   */
  readonly decorative?: boolean;
  /** Overrides the accessible name (ignored when `decorative`). */
  readonly accessibilityLabel?: string;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * A thin wrapper for rendering a local SVG file as a React Native view.
 *
 * React Native cannot render an SVG string, so every brand asset is imported
 * through `react-native-svg-transformer` as a component. This wrapper is the one
 * place the size and tint props are applied, so a caller passes a size and (for a
 * `currentColor` glyph) a colour rather than repeating the same props everywhere.
 *
 * It is intentionally presentation-only: no data, no state, no fetching.
 */
export default function SvgImage({
  source: Source,
  size,
  width,
  height,
  color,
  decorative = false,
  accessibilityLabel,
  testID,
}: SvgImageProps): React.ReactElement {
  return (
    <Source
      width={width ?? size}
      height={height ?? size}
      color={color}
      accessible={decorative ? false : undefined}
      accessibilityElementsHidden={decorative || undefined}
      importantForAccessibility={decorative ? 'no-hide-descendants' : undefined}
      accessibilityLabel={accessibilityLabel}
      testID={testID}
    />
  );
}
