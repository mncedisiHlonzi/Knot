import React, { useCallback, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';
import Video, { type OnLoadData } from 'react-native-video';

import { colors, radius, spacing, tabularNums, typeScale } from '../theme';
import { formatDuration } from './feedVideoMeta';

/**
 * The mute control's square tap target, exported so the entry can stack its
 * media-count badge clear of it.
 */
export const VIDEO_MUTE_BUTTON_SIZE = 44;

/** The speaker glyph inside the mute control. */
const MUTE_ICON_SIZE = 22;

type FeedVideoProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * True only for the entry the feed has decided is on screen; it is the single
   * input that starts or stops playback, so at most one video plays at a time.
   */
  readonly isVisible: boolean;
  /** The feed-wide mute state — one control governs every video. */
  readonly isMuted: boolean;
  /** Called when the mute control is tapped. */
  readonly onToggleMute: () => void;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The feed's video preview: it plays itself in place.
 *
 * Playback is driven entirely by `isVisible` (the feed's viewability result) and
 * `isMuted` (the feed-wide toggle), so the component holds no playback state of
 * its own beyond the duration it learns on load. It is the ONLY stateful part of
 * a feed entry: `FeedEntry` stays hookless so it can be called directly in the
 * renderer-less tests, and the video — which needs hooks — lives here
 * (KNOT-020c-fix3).
 *
 * There is no centre play button: the video starts on its own when it scrolls
 * into view, and tapping anywhere on it (except the mute control) opens the
 * story, which is the entry's own tap target. A grey layer sits behind the video
 * so something is drawn while the first frame decodes or if playback is
 * impossible.
 */
export default function FeedVideo({
  uri,
  isVisible,
  isMuted,
  onToggleMute,
  testID,
}: FeedVideoProps): React.ReactElement {
  const [durationSeconds, setDurationSeconds] = useState(0);

  // The duration is only known once the file's metadata has loaded; before that
  // the badge is simply absent.
  const handleLoad = useCallback((data: OnLoadData): void => {
    setDurationSeconds(typeof data.duration === 'number' ? data.duration : 0);
  }, []);

  const durationLabel = formatDuration(durationSeconds);

  return (
    <View style={styles.container} testID={testID}>
      {/* Behind the video: covers the gap before the first frame, and stands in
          if the video can never play. */}
      <View style={styles.fallback} />

      <Video
        source={{ uri }}
        style={styles.video}
        paused={!isVisible}
        muted={isMuted}
        repeat
        resizeMode="cover"
        onLoad={handleLoad}
      />

      <View style={styles.mediaTag} pointerEvents="none">
        <Text style={[typeScale.caption, styles.badgeText]}>Video</Text>
      </View>

      {durationLabel !== '' ? (
        <View style={styles.durationBadge} pointerEvents="none">
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

const styles = StyleSheet.create({
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
