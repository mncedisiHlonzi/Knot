import React from 'react';
import type { SvgProps } from 'react-native-svg';

import WordmarkSource from '../assets/brand/logo-wordmark.svg';
import SvgImage from './SvgImage';

/** The colour treatments a wordmark can be drawn in. */
export type WordmarkVariant = 'dark' | 'light';

type WordmarkProps = {
  /** The rendered height, in points. The width follows the asset's aspect ratio. Defaults to 24. */
  readonly size?: number;
  /** The colour treatment. Defaults to `dark`. */
  readonly variant?: WordmarkVariant;
};

/** The wordmark asset's aspect ratio (`logo-wordmark.svg` is 160 x 64). */
const WORDMARK_ASPECT_RATIO = 160 / 64;

/**
 * The "Knot" wordmark on its own, with no logomark.
 *
 * NOTE (KNOT-020b-fix): the brand set ships a **single** standalone wordmark
 * (`logo-wordmark.svg`, navy). There is no standalone white wordmark asset — the
 * white treatment exists only inside `logo-monochrome-light.svg`, which is the
 * logomark *and* the wordmark — so both variants resolve to the navy wordmark
 * today. The map is the single place to point at new assets when the set gains a
 * standalone tinted wordmark.
 */
const SOURCES: Readonly<Record<WordmarkVariant, React.ComponentType<SvgProps>>> = {
  dark: WordmarkSource,
  light: WordmarkSource,
};

export default function Wordmark({
  size = 24,
  variant = 'dark',
}: WordmarkProps): React.ReactElement {
  const source = SOURCES[variant];
  return <SvgImage source={source} height={size} width={size * WORDMARK_ASPECT_RATIO} />;
}
