import React from 'react';
import type { SvgProps } from 'react-native-svg';

import LogomarkSource from '../assets/brand/logo-logomark.svg';
import SvgImage from './SvgImage';

/** The colour treatments a logomark can be drawn in. */
export type LogomarkVariant = 'primary' | 'light' | 'dark' | 'mono-light' | 'mono-dark';

type LogomarkProps = {
  /** The rendered size (square), in points. Defaults to 32. */
  readonly size?: number;
  /** The colour treatment. Defaults to `primary`. */
  readonly variant?: LogomarkVariant;
};

/**
 * The Knot logomark — the knot/infinity mark — on its own, with no wordmark.
 *
 * NOTE (KNOT-020b-fix): the brand set ships a **single** standalone logomark, the
 * gradient one (`logo-logomark.svg`). The `light`, `dark`, `mono-light` and
 * `mono-dark` treatments exist only inside the combined horizontal logo files
 * (`logo-primary-*`, `logo-monochrome-*`), which also carry the wordmark, so there
 * is no standalone asset to select for them. Every variant therefore resolves to
 * the gradient mark today; the prop is accepted so callers can be written against
 * the intended API, and the map is the single place to point at new assets when
 * the set gains standalone tinted logomarks.
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
}: LogomarkProps): React.ReactElement {
  return <SvgImage source={SOURCES[variant]} width={size} height={size} />;
}
