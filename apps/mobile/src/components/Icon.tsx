import React from 'react';
import { IconContext, type IconProps, type IconWeight } from 'phosphor-react-native';

/**
 * The weight every Phosphor glyph in the app is drawn at unless it says otherwise.
 *
 * Phosphor's own default is `regular`. Knot draws its icons at 14-30 points, where
 * `regular` still reads as a mechanical, uniform line; `light` is the fine,
 * geometric outline the design calls for, and the app pairs it with a `fill`
 * wherever a glyph has to read as *selected* rather than merely present
 * (KNOT-020k-fix2).
 *
 * A lighter stroke loses weight before it loses legibility, so where a glyph
 * carries meaning on its own the size carries the contrast back: the tab bar's
 * glyphs grew to 30 points, and the Create disc keeps `regular` against its
 * sunken fill (KNOT-020k-fix2).
 */
export const DEFAULT_ICON_WEIGHT: IconWeight = 'light';

/**
 * The context value `IconScope` publishes.
 *
 * It is a module constant so its identity never changes: the provider is mounted
 * once at the root, and a fresh object on each render would re-render every icon in
 * the tree for no reason.
 *
 * Only `weight` is set. Phosphor destructures its own defaults for everything the
 * context omits (its fallback colour, size, mirroring and duotone opacity), so
 * omitting them keeps the library's defaults exactly as they were — and this file
 * never names a raw colour value (KNOT-020k-fix).
 */
const ICON_CONTEXT_VALUE: IconProps = { weight: DEFAULT_ICON_WEIGHT };

type IconScopeProps = {
  readonly children: React.ReactNode;
};

/**
 * The app-wide icon wrapper: every Phosphor glyph under it defaults to the light
 * weight.
 *
 * Knot imports its icons from `phosphor-react-native` directly in ~26 files, so a
 * per-call-site wrapper would have meant rewriting every one of them for a change
 * that is the same everywhere. Phosphor reads an `IconContext` fallback for the
 * weight, and an icon's own `weight` prop still wins, so:
 *
 *   - a bare `<House />`             draws light;
 *   - a `<MapPin weight="fill" />`   keeps its fill;
 *   - the tab bar's selected glyph   asks for `fill` explicitly.
 *
 * That is the whole strategy: one provider at the root, no import churn, and every
 * deliberate weight still honoured (KNOT-020k-fix, KNOT-020k-fix2).
 *
 * It is mounted inside the theme provider and outside every screen, including the
 * splash and the auth flow, so every screen draws the same weight.
 */
export function IconScope({ children }: IconScopeProps): React.ReactElement {
  return <IconContext.Provider value={ICON_CONTEXT_VALUE}>{children}</IconContext.Provider>;
}
