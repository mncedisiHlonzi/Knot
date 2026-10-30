import React from 'react';
import { IconContext, type IconProps, type IconWeight } from 'phosphor-react-native';

/**
 * The weight every Phosphor glyph in the app is drawn at unless it says otherwise.
 *
 * Phosphor's own default is `regular`, which reads as a hairline at the sizes Knot
 * draws icons (14-28 points) — too light for the monochrome design, where a glyph
 * carries the whole meaning of a control. `bold` is the app's base weight
 * (KNOT-020k-fix).
 *
 * The bottom navigation is the one place this does not apply: it draws Ionicons
 * rather than Phosphor, because the founder wanted chunky geometric shapes and
 * Phosphor's are rounder (KNOT-020k-fix3). Nothing else changes weight.
 */
export const DEFAULT_ICON_WEIGHT: IconWeight = 'bold';

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
 * The app-wide icon wrapper: every Phosphor glyph under it defaults to the bold
 * weight.
 *
 * Knot imports its icons from `phosphor-react-native` directly in ~26 files, so a
 * per-call-site wrapper would have meant rewriting every one of them for a change
 * that is the same everywhere. Phosphor reads an `IconContext` fallback for the
 * weight, and an icon's own `weight` prop still wins, so:
 *
 *   - a bare `<House />`             draws bold;
 *   - a `<MapPin weight="fill" />`   keeps its fill;
 *   - an explicit weight anywhere    is honoured.
 *
 * That is the whole strategy: one provider at the root, no import churn, and every
 * deliberate weight still honoured (KNOT-020k-fix).
 *
 * It is mounted inside the theme provider and outside every screen, including the
 * splash and the auth flow, so every screen draws the same weight.
 */
export function IconScope({ children }: IconScopeProps): React.ReactElement {
  return <IconContext.Provider value={ICON_CONTEXT_VALUE}>{children}</IconContext.Provider>;
}
