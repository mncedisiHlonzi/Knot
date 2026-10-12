import React from 'react';
import type { SvgProps } from 'react-native-svg';

import LogoDark from '../assets/brand/logo-primary-dark.svg';
import LogoPrimary from '../assets/brand/logo-primary-horizontal.svg';
import LogoLight from '../assets/brand/logo-primary-light.svg';
import LogoMonoDark from '../assets/brand/logo-monochrome-dark.svg';
import LogoMonoLight from '../assets/brand/logo-monochrome-light.svg';
import SvgImage from './SvgImage';

/** The colour treatments the full logo can be drawn in. */
export type LogoVariant = 'primary' | 'light' | 'dark' | 'mono-light' | 'mono-dark';

type LogoProps = {
  /** The rendered height, in points. The width follows the asset's aspect ratio. Defaults to 32. */
  readonly height?: number;
  /** The colour treatment. Defaults to `primary`. */
  readonly variant?: LogoVariant;
};

/** The horizontal logo asset's aspect ratio (every variant is 240 x 64). */
const LOGO_ASPECT_RATIO = 240 / 64;

/**
 * The full horizontal logo: the logomark beside the wordmark.
 *
 * This is the one asset family the brand set completes — each variant maps to a
 * dedicated file:
 *
 *   primary     logo-primary-horizontal.svg   gradient mark, navy wordmark (light surfaces)
 *   dark        logo-primary-dark.svg         same, for light backgrounds
 *   light       logo-primary-light.svg        for dark backgrounds
 *   mono-light  logo-monochrome-light.svg     all white
 *   mono-dark   logo-monochrome-dark.svg      all navy
 */
const SOURCES: Readonly<Record<LogoVariant, React.ComponentType<SvgProps>>> = {
  primary: LogoPrimary,
  dark: LogoDark,
  light: LogoLight,
  'mono-light': LogoMonoLight,
  'mono-dark': LogoMonoDark,
};

export default function Logo({ height = 32, variant = 'primary' }: LogoProps): React.ReactElement {
  const source = SOURCES[variant];
  return <SvgImage source={source} height={height} width={height * LOGO_ASPECT_RATIO} />;
}
