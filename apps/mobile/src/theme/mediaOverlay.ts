import type { TextStyle } from 'react-native';

import type { ThemeColors } from './colors';

/**
 * The ink and shadow for text drawn **over media**, on a `scrim.overlay` chip or
 * disc (KNOT-020j).
 *
 * The scrim is one translucent navy in both themes, so its text must be one
 * light value in both themes as well — and it needs a shadow for the case where
 * the photo or video frame behind it is itself bright. Both facts are true of
 * every media overlay in the app, so they are expressed once here rather than
 * repeated as three style entries on each screen.
 *
 * Usage: `style={[typeScale.label, mediaOverlayText(colors), styles.chipText]}`.
 * The size and weight come from `typeScale.label` (13pt, bold); this only adds
 * the colour and the shadow.
 */
export function mediaOverlayText(colors: ThemeColors): TextStyle {
  return {
    color: colors.text.onMedia,
    textShadowColor: colors.scrim.textShadow,
    textShadowOffset: { width: 0, height: 1 },
    textShadowRadius: 2,
  };
}
