import React, { useCallback, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { PlayCircle, SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';
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

/** The play glyph drawn on a display-only preview. */
const PREVIEW_PLAY_ICON_SIZE = 48;

/** The preview's play badge's diameter. */
const PREVIEW_PLAY_BADGE_SIZE = 64;

/** A no-op mute handler, for a caller that draws no mute control. */
const NOOP = (): void => undefined;

type FeedVideoProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * True only for the entry the feed has decided is on screen; it is the single
   * input that starts or stops playback, so at most one video plays at a time.
   * Defaults to `false`, which draws the video paused. A `previewOnly` preview
   * never plays however this is set.
   */
  readonly isVisible?: boolean;
  /**
   * The feed-wide mute state — one control governs every video. Defaults to
   * `true`.
   */
  readonly isMuted?: boolean;
  /**
   * Called when the mute control is tapped. Defaults to a no-op, for a caller that
   * draws no control (a `previewOnly` preview).
   */
  readonly onToggleMute?: () => void;
  /**
   * Draws the paused first frame under a centred play glyph and none of the
   * player's chrome — no "Video" tag, no duration badge, no mute control — and
   * never plays, so a wall of cards costs no decoding. It is the profile card's
   * media preview (KNOT-020g-fix4). Defaults to `false`, the feed's full player.
   */
  readonly previewOnly?: boolean;
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
 * There is no centre play button in the player: the video starts on its own when
 * it scrolls into view, and tapping anywhere on it (except the mute control) opens
 * the story, which is the entry's own tap target. A grey layer sits behind the
 * video so something is drawn while the first frame decodes or if playback is
 * impossible.
 *
 * With `previewOnly` it becomes a display-only preview instead: the same paused
 * first frame under a centred play glyph, with no chrome, no playback, and no
 * handlers — the profile wall's card media (KNOT-020g-fix4).
 */
export default function FeedVideo({
  uri,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  previewOnly = false,
  testID,
}: FeedVideoProps): React.ReactElement {
  const [durationSeconds, setDurationSeconds] = useState(0);

  // The duration is only known once the file's metadata has loaded; before that
  // the badge is simply absent.
  const handleLoad = useCallback((data: OnLoadData): void => {
    setDurationSeconds(typeof data.duration === 'number' ? data.duration : 0);
  }, []);

  // A display-only preview is always paused and never reads a duration.
  const playing = previewOnly ? false : isVisible;
  const durationLabel = previewOnly ? '' : formatDuration(durationSeconds);

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
        onLoad={previewOnly ? undefined : handleLoad}
      />

      {previewOnly ? (
        // Decorative: the card around it owns the tap, and the video never plays.
        <View style={styles.previewPlay} pointerEvents="none">
          <PlayCircle color={colors.text.onBrand} size={PREVIEW_PLAY_ICON_SIZE} weight="fill" />
        </View>
      ) : (
        <>
          <View style={styles.mediaTag} pointerEvents="none">
            <Text style={[typeScale.caption, styles.badgeText]}>Video</Text>
          </View>

          {durationLabel !== '' ? (
            <View style={styles.durationBadge} pointerEvents="none">
              <Text style={[typeScale.caption, tabularNums, styles.badgeText]}>
                {durationLabel}
              </Text>
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
        </>
      )}
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
  // A display-only preview's play badge, centred on the frame it sits over.
  previewPlay: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.pill,
    height: PREVIEW_PLAY_BADGE_SIZE,
    justifyContent: 'center',
    left: '50%',
    marginLeft: -PREVIEW_PLAY_BADGE_SIZE / 2,
    marginTop: -PREVIEW_PLAY_BADGE_SIZE / 2,
    position: 'absolute',
    top: '50%',
    width: PREVIEW_PLAY_BADGE_SIZE,
  },
  video: {
    ...StyleSheet.absoluteFillObject,
  },
});
