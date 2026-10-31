import React from 'react';
import type { SvgProps } from 'react-native-svg';

import LogomarkMonoSource from '../assets/brand/logo-logomark-mono.svg';
import LogomarkSource from '../assets/brand/logo-logomark.svg';
import SvgImage from './SvgImage';

/** The colour treatments a logomark can be drawn in. */
export type LogomarkVariant = 'primary' | 'light' | 'dark' | 'mono-light' | 'mono-dark';

type LogomarkProps = {
  /** The rendered size (square), in points. Defaults to 32. */
  readonly size?: number;
  /** The colour treatment. Defaults to `primary`. */
  readonly variant?: LogomarkVariant;
  /**
   * Draws the mark in ONE ink instead of the brand gradient.
   *
   * The mark is a single stroked ribbon, so its monochrome cut is the same
   * geometry with the gradient replaced by `currentColor`: pass a colour and the
   * whole glyph takes it. The Feed's header passes `colors.text.primary`, which is
   * pure black in light mode and pure white in dark mode, so the mark follows the
   * monochrome palette instead of sitting on it as a purple patch
   * (KNOT-020k-fix9). When omitted the gradient mark draws, exactly as before.
   */
  readonly color?: string;
};

/**
 * The Knot logomark — the knot/infinity mark — on its own, with no wordmark.
 *
 * Two treatments exist:
 *
 *   gradient  `logo-logomark.svg`       the brand mark, for first impressions
 *   mono      `logo-logomark-mono.svg`  the same ribbon in `currentColor`, for
 *                                       surfaces that are monochrome by design
 *
 * NOTE (KNOT-020b-fix): the brand set ships a **single** standalone gradient
 * logomark. The `light`, `dark`, `mono-light` and `mono-dark` treatments exist only
 * inside the combined horizontal logo files (`logo-primary-*`,
 * `logo-monochrome-*`), which also carry the wordmark, so every `variant` still
 * resolves to the gradient mark. A caller that wants a single-ink mark passes
 * `color` instead, which is the only standalone monochrome cut that exists
 * (KNOT-020k-fix9).
 */
const SOURCES: Readonly<Record<LogomarkVariant, React.ComponentType<SvgProps>>> = {
  primary: LogomarkSource,
  light: LogomarkSource,
  dark: LogomarkSource,
  'mono-light': LogomarkSource,
  'mono-dark': LogomarkSource,
};

export default function Logomark({
  size = 32,
  variant = 'primary',
  color,
}: LogomarkProps): React.ReactElement {
  // One ink: the mono asset is stroked with `currentColor`, so the `color` prop
  // tints the whole ribbon.
  if (color !== undefined) {
    return <SvgImage source={LogomarkMonoSource} width={size} height={size} color={color} />;
  }

  return <SvgImage source={SOURCES[variant]} width={size} height={size} />;
}
