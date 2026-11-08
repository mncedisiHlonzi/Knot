/**
 * The one image renderer (KNOT-051).
 *
 * Every surface that shows a photo had its own layering rules: the feed drew one
 * `<Image>`, the Story Detail carousel drew a thumbnail under the display, and the
 * viewer drew a still under an animated rendition. They now share this component,
 * so "progressive" means the same thing everywhere:
 *
 * 1. the **still** — a 400px WebP thumbnail for a photo, the extracted poster for a
 *    video frame — is drawn first, because it is already in memory from the card
 *    or arrives in a few kilobytes;
 * 2. the **full rendition** fades in over it when it has decoded.
 *
 * The still is never removed: it sits underneath, so the fade can never reveal a
 * blank frame. Both layers share one box, so a caller that transforms the box (the
 * viewer's pinch-zoom) moves them together and a zoomed page never tears apart
 * (KNOT-042).
 */
import React, { useCallback, useRef } from 'react';
import {
  Animated,
  Easing,
  Image,
  View,
  type ImageStyle,
  type StyleProp,
  type ViewStyle,
} from 'react-native';

import { motion } from '../theme';

/** How long the full rendition takes to fade in. */
export const MEDIA_IMAGE_FADE_MS = motion.durations.medium;

type MediaImageProps = {
  /** The full rendition's address, already resolved to an absolute URL. */
  readonly uri: string;
  /** The still drawn underneath until the full rendition decodes. */
  readonly stillUri?: string | null;
  /** The box both layers fill. */
  readonly style?: StyleProp<ViewStyle>;
  /** An extra style for each layer (rarely needed; they fill the box). */
  readonly layerStyle?: StyleProp<ImageStyle>;
  /** Defaults to `cover`, the list behaviour. The viewer asks for `contain`. */
  readonly resizeMode?: 'cover' | 'contain';
  /** Called once the full rendition has loaded, so a caller can retire a loader. */
  readonly onFullLoadEnd?: () => void;
  /** Test identifier for the box. */
  readonly testID?: string;
  /** Test identifier for the still layer. */
  readonly stillTestID?: string;
  /** Test identifier for the full layer. */
  readonly fullTestID?: string;
};

/** The layer geometry: an explicit fill, so no test needs `absoluteFillObject`. */
const LAYER_FILL: ImageStyle = {
  bottom: 0,
  left: 0,
  position: 'absolute',
  right: 0,
  top: 0,
};

/**
 * A photo at its best available resolution, with no blank frame in between.
 *
 * With no still the full rendition is simply drawn undimmed and pops in when it
 * decodes; with one, the still is the picture the reader sees and the full
 * rendition fades over it.
 */
export default function MediaImage({
  uri,
  stillUri = null,
  style,
  layerStyle,
  resizeMode = 'cover',
  onFullLoadEnd,
  testID,
  stillTestID = 'media-image-still',
  fullTestID = 'media-image-full',
}: MediaImageProps): React.ReactElement | null {
  const still = typeof stillUri === 'string' && stillUri.trim() !== '' ? stillUri : null;
  const full = typeof uri === 'string' ? uri.trim() : '';

  const opacity = useRef(new Animated.Value(still === null ? 1 : 0)).current;

  const handleLoad = useCallback((): void => {
    Animated.timing(opacity, {
      toValue: 1,
      duration: MEDIA_IMAGE_FADE_MS,
      easing: Easing.bezier(...motion.easing.standard),
      useNativeDriver: true,
    }).start();

    if (onFullLoadEnd !== undefined) {
      onFullLoadEnd();
    }
  }, [onFullLoadEnd, opacity]);

  if (full === '') {
    return null;
  }

  return (
    <View style={style} testID={testID}>
      {still === null ? null : (
        <Image
          source={{ uri: still }}
          style={[LAYER_FILL, layerStyle]}
          resizeMode={resizeMode}
          accessibilityIgnoresInvertColors
          testID={stillTestID}
        />
      )}

      <Animated.Image
        source={{ uri: full }}
        // With a still underneath, the rendition starts invisible and fades in as
        // it decodes; with none there is nothing to reveal, so it is drawn at full
        // strength and simply appears.
        style={[LAYER_FILL, layerStyle, { opacity }]}
        resizeMode={resizeMode}
        onLoad={handleLoad}
        accessibilityIgnoresInvertColors
        testID={fullTestID}
      />
    </View>
  );
}
