import React from 'react';
import type { FC } from 'react';
import type { SvgProps } from 'react-native-svg';

import BellFill from '../assets/tabs/bell-fill.svg';
import BellOutline from '../assets/tabs/bell-outline.svg';
import ChatFill from '../assets/tabs/chat-fill.svg';
import ChatOutline from '../assets/tabs/chat-outline.svg';
import HomeFill from '../assets/tabs/home-fill.svg';
import HomeOutline from '../assets/tabs/home-outline.svg';
import MapFill from '../assets/tabs/map-fill.svg';
import MapOutline from '../assets/tabs/map-outline.svg';
import Plus from '../assets/tabs/plus.svg';

/**
 * The five bottom-navigation glyphs, drawn from the founder's own SVG files.
 *
 * The artwork is not code: it is the exact icon set the founder supplied, checked
 * in at `src/assets/tabs/` and rendered through `react-native-svg-transformer`,
 * the same mechanism the brand marks use (KNOT-020b-fix). An earlier attempt drew
 * the same shapes as hand-written paths and did not reach the quality of the
 * reference, so the files themselves are the source of truth (KNOT-020k-fix5).
 *
 * Each destination comes in two cuts — `*-outline` for an idle tab and `*-fill`
 * for the selected one — and the tab's state is a prop (`isActive`) rather than a
 * second glyph component, because one component owns both cuts of its own shape.
 * Create is the exception: the plus has a single cut, and the elevated disc behind
 * it carries the selection.
 *
 * Two facts about the assets are load-bearing:
 *
 * - every file declares `viewBox="0 0 512 512"`, so `width`/`height` scale the
 *   artwork rather than crop it, and
 * - every path is filled with `currentColor`, so the bar tints the glyph by
 *   passing `color` — the asset itself carries no ink of its own. (A hardcoded
 *   fill would win over any prop, because the shape's own attribute beats an
 *   inherited one.)
 *
 * Each glyph also carries a `testID` that names the cut it drew. Jest maps every
 * `.svg` import to one shared stub, so the two cuts are the same component object
 * there; the test id is what still distinguishes them.
 *
 * Each component is presentation-only — no state, no data, no icon library — so a
 * caller passes a size, an ink, and whether the tab is the selected one.
 */

/** The props every tab glyph takes. They are identical across the five. */
export type TabIconProps = {
  /** The drawn size, in points. The glyph is square. */
  readonly size: number;
  /** The ink. It reaches the artwork through the SVG's `currentColor` fill. */
  readonly color: string;
  /** Draws the solid (selected) cut instead of the outline (idle) cut. */
  readonly isActive: boolean;
};

/** What a `.svg` import resolves to: a component taking the usual SVG props. */
type Glyph = FC<SvgProps>;

/**
 * Hides the glyph from assistive technology.
 *
 * The tab is a `Pressable` that already carries the destination's spoken name, so
 * the artwork inside it must not be announced a second time (KNOT-020b-fix).
 */
const DECORATIVE = {
  accessibilityElementsHidden: true,
  accessible: false,
  importantForAccessibility: 'no-hide-descendants',
} as const;

/**
 * Home: the house, outline while idle and solid while selected.
 */
export function HomeIcon({ size, color, isActive }: TabIconProps): React.ReactElement {
  const Artwork: Glyph = isActive ? HomeFill : HomeOutline;

  return (
    <Artwork
      width={size}
      height={size}
      color={color}
      testID={isActive ? 'tab-icon-home-fill' : 'tab-icon-home-outline'}
      {...DECORATIVE}
    />
  );
}

/** Map: the folded map, outline while idle and solid while selected. */
export function MapIcon({ size, color, isActive }: TabIconProps): React.ReactElement {
  const Artwork: Glyph = isActive ? MapFill : MapOutline;

  return (
    <Artwork
      width={size}
      height={size}
      color={color}
      testID={isActive ? 'tab-icon-map-fill' : 'tab-icon-map-outline'}
      {...DECORATIVE}
    />
  );
}

/** Inquiries: the speech bubble, outline while idle and solid while selected. */
export function ChatIcon({ size, color, isActive }: TabIconProps): React.ReactElement {
  const Artwork: Glyph = isActive ? ChatFill : ChatOutline;

  return (
    <Artwork
      width={size}
      height={size}
      color={color}
      testID={isActive ? 'tab-icon-chat-fill' : 'tab-icon-chat-outline'}
      {...DECORATIVE}
    />
  );
}

/** Notifications: the bell, outline while idle and solid while selected. */
export function BellIcon({ size, color, isActive }: TabIconProps): React.ReactElement {
  const Artwork: Glyph = isActive ? BellFill : BellOutline;

  return (
    <Artwork
      width={size}
      height={size}
      color={color}
      testID={isActive ? 'tab-icon-bell-fill' : 'tab-icon-bell-outline'}
      {...DECORATIVE}
    />
  );
}

/**
 * Create: the elevated disc's plus.
 *
 * The supplied set has one plus, not a pair, so `isActive` draws nothing different
 * — the disc behind the glyph carries the selection, and the bar always passes
 * `false` here anyway (KNOT-020k-fix4, KNOT-020k-fix5).
 */
export function PlusIcon({ size, color }: TabIconProps): React.ReactElement {
  return <Plus width={size} height={size} color={color} testID="tab-icon-plus" {...DECORATIVE} />;
}
