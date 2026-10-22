import React, { useCallback, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';
import Video, { type OnLoadData } from 'react-native-video';

import { radius, spacing, tabularNums, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';
import { formatDuration } from './feedVideoMeta';

/**
 * The mute control's square tap target, exported so the entry can stack its
 * media-count badge clear of it.
 */
export const VIDEO_MUTE_BUTTON_SIZE = 44;

/** The speaker glyph inside the mute control. */
const MUTE_ICON_SIZE = 22;

/** A no-op mute handler, for a caller that draws no mute control. */
const NOOP = (): void => undefined;

type FeedVideoProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * True only for the entry the screen has decided is on screen; it is the single
   * input that starts or stops playback, so at most one video plays at a time.
   * Defaults to `false`, which draws the video paused.
   */
  readonly isVisible?: boolean;
  /**
   * The screen-wide mute state — one control governs every video. Defaults to
   * `true`.
   */
  readonly isMuted?: boolean;
  /** Called when the mute control is tapped. Defaults to a no-op. */
  readonly onToggleMute?: () => void;
  /**
   * Reports the video's natural pixel size once the player has read it.
   *
   * It is how a frame learns the shape of a video the payload could not describe:
   * the upload pipeline has no dimensions for most video assets, because the
   * picker reports only their duration, so the frame would otherwise never know
   * what shape to be (KNOT-020g-fix7).
   */
  readonly onLoadSize?: (width: number, height: number) => void;
  /**
   * The duration the payload already knows, in milliseconds. It seeds the duration
   * badge so the Story Detail carousel can show `0:48` before the player has
   * loaded; the player's own report replaces it once metadata arrives. Optional,
   * and ignored when it is not a positive number (KNOT-025).
   */
  readonly durationMs?: number | null;
  /**
   * Whether the small "Video" tag is drawn in the top-left. Defaults to `true`,
   * the Feed's presentation; the Story Detail carousel passes `false` because its
   * counter pill and caption already identify the media (KNOT-025).
   */
  readonly showTag?: boolean;
  /**
   * Where the duration badge sits. Defaults to `top-right`, the Feed's place. The
   * Story Detail carousel asks for `bottom-left`, leaving the top-right corner for
   * the carousel's counter pill (KNOT-025). The mute control keeps the
   * bottom-right corner in both cases.
   */
  readonly durationPosition?: 'top-right' | 'bottom-left';
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The feed's video preview: it plays itself in place.
 *
 * Playback is driven entirely by `isVisible` (the list's viewability result) and
 * `isMuted` (the screen-wide toggle), so the component holds no playback state of
 * its own beyond the duration it learns on load. It is the ONLY stateful part of
 * a feed entry: `FeedEntry` stays hookless from the caller's point of view, and
 * the video — which needs hooks — lives here (KNOT-020c-fix3).
 *
 * There is no centre play button: the video starts on its own when it scrolls into
 * view, and tapping anywhere on it (except the mute control) opens the story,
 * which is the enclosing card's own tap target. A grey layer sits behind the video
 * so something is drawn while the first frame decodes or if playback is
 * impossible.
 *
 * Every screen that draws a story's video uses this component — the Feed, Place
 * Stories, and the profile wall — so they all autoplay and mute the same way
 * (KNOT-020g-fix13).
 *
 * The video is fitted with `cover`, so it always fills the frame its caller gives
 * it. The caller owns the ratio: `FeedEntry` and the profile card both set it from
 * the media's reported dimensions, falling back to the natural size this component
 * reports through `onLoadSize` when the payload has none (KNOT-020g-fix6,
 * KNOT-020g-fix7).
 */
export default function FeedVideo({
  uri,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  onLoadSize,
  durationMs,
  showTag = true,
  durationPosition = 'top-right',
  testID,
}: FeedVideoProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  // The payload's duration, when it has one, is the first thing the badge can
  // show; the player replaces it with the authoritative value on load.
  const [durationSeconds, setDurationSeconds] = useState(() =>
    typeof durationMs === 'number' && Number.isFinite(durationMs) && durationMs > 0
      ? durationMs / 1000
      : 0,
  );

  // The duration and the natural size are both known only once the file's
  // metadata has loaded; before that the duration badge is simply absent and the
  // frame is still its placeholder shape.
  const handleLoad = useCallback(
    (data: OnLoadData): void => {
      setDurationSeconds(typeof data.duration === 'number' ? data.duration : 0);

      // The size is reported defensively: the type declares it, but a platform
      // that could not read the track leaves it empty, and a frame must not be
      // shaped from a missing pair.
      const natural = data.naturalSize;
      if (
        onLoadSize !== undefined &&
        typeof natural?.width === 'number' &&
        typeof natural?.height === 'number' &&
        natural.width > 0 &&
        natural.height > 0
      ) {
        onLoadSize(natural.width, natural.height);
      }
    },
    [onLoadSize],
  );

  const playing = isVisible;
  const durationLabel = formatDuration(durationSeconds);

  return (
    <View style={styles.container} testID={testID}>
      {/* Behind the video: covers the gap before the first frame, and stands in
          if the video can never play. */}
      <View style={styles.fallback} />

      <Video
        source={{ uri }}
        style={styles.video}
        paused={!playing}
        muted={isMuted}
        repeat
        resizeMode="cover"
        onLoad={handleLoad}
      />

      {showTag ? (
        <View style={styles.mediaTag} pointerEvents="none">
          <Text style={[typeScale.caption, styles.badgeText]}>Video</Text>
        </View>
      ) : null}

      {durationLabel !== '' ? (
        <View
          style={[
            styles.durationBadge,
            durationPosition === 'bottom-left'
              ? styles.durationBottomLeft
              : styles.durationTopRight,
          ]}
          pointerEvents="none"
        >
          <Text style={[typeScale.caption, tabularNums, styles.badgeText]}>{durationLabel}</Text>
        </View>
      ) : null}

      <Pressable
        style={styles.muteButton}
        onPress={onToggleMute}
        accessibilityRole="button"
        accessibilityLabel={isMuted ? 'Unmute video' : 'Mute'}
      >
        {isMuted ? (
          <SpeakerSlash color={colors.text.onBrand} size={MUTE_ICON_SIZE} weight="fill" />
        ) : (
          <SpeakerHigh color={colors.text.onBrand} size={MUTE_ICON_SIZE} weight="fill" />
        )}
      </Pressable>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    badgeText: {
      color: colors.text.onBrand,
    },
    container: {
      ...StyleSheet.absoluteFillObject,
    },
    durationBadge: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.sm,
      paddingHorizontal: spacing.sm,
      paddingVertical: 2,
      position: 'absolute',
    },
    durationBottomLeft: {
      bottom: spacing.sm,
      left: spacing.sm,
    },
    durationTopRight: {
      right: spacing.sm,
      top: spacing.sm,
    },
    fallback: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.sunken,
    },
    mediaTag: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.sm,
      left: spacing.sm,
      paddingHorizontal: spacing.sm,
      paddingVertical: 2,
      position: 'absolute',
      top: spacing.sm,
    },
    muteButton: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      bottom: spacing.sm,
      height: VIDEO_MUTE_BUTTON_SIZE,
      justifyContent: 'center',
      position: 'absolute',
      right: spacing.sm,
      width: VIDEO_MUTE_BUTTON_SIZE,
    },
    video: {
      ...StyleSheet.absoluteFillObject,
    },
  });
