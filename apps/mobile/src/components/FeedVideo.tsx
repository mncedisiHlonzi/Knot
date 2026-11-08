import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';

import { useMediaPlayback, useMediaPlayerActions } from '../context/MediaPlayerContext';
import {
  mediaOverlayText,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import { formatProgress } from './feedVideoMeta';

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
   * The media's identity for the engine. Defaults to the URL, because a URL is the
   * only identity a feed preview and the story's media array share — it is what
   * lets the viewer open on the exact clip the card is playing (KNOT-051).
   */
  readonly mediaId?: string;
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
  /**
   * Kept for callers that still pass it. It no longer mounts a second player: the
   * unified engine deliberately keeps **one** decoder, and a paused second player
   * per list is exactly the cost this task removes (KNOT-051).
   */
  readonly isNextInLine?: boolean;
  /**
   * The extracted poster still: the WebP frame the media worker took one second
   * into the clip. It is drawn OVER the player until the player has a frame of its
   * own, so a card opens on a picture instead of the black surface a video shows
   * while it decodes. Absent for a video whose poster could not be extracted, which
   * keeps the previous behaviour (KNOT-042).
   */
  readonly posterUri?: string | null;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * A video *surface*: the chrome of a card's player, and the hole the unified
 * engine's one player is drawn into (KNOT-051).
 *
 * This component no longer owns a `<Video>`. It registers itself with
 * `MediaPlayerContext` when the card is on screen, tells the engine which media to
 * play, and hands over the `View` the picture should cover. The player itself is
 * mounted once by `MediaHost` at the root, so scrolling a card out of view,
 * opening the story, or opening the full-screen viewer never tears down and
 * rebuilds a native player — and a clip keeps its position across all of them.
 *
 * The chrome stays here — the "Video" tag, the running time and the mute control —
 * because it belongs to the card, not to the player. Every screen that draws a
 * story's video uses this component (the Feed, Place Stories, the profile wall and
 * the Story Detail carousel), so they all share the same engine and the same mute
 * rule.
 *
 * The caller owns the frame's shape: `FeedEntry` and the profile card set it from
 * the media's reported dimensions, falling back to the natural size the engine
 * reports through `onLoadSize` when the payload has none (KNOT-020g-fix6,
 * KNOT-020g-fix7).
 */
export default function FeedVideo({
  uri,
  mediaId,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  onLoadSize,
  durationMs,
  showTag = true,
  durationPosition = 'top-right',
  posterUri,
  testID,
}: FeedVideoProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const player = useMediaPlayerActions();
  const playback = useMediaPlayback();

  const id = typeof mediaId === 'string' && mediaId.trim() !== '' ? mediaId.trim() : uri;
  const active = playback.activeMediaId === id;
  const fullscreen = playback.mode === 'fullscreen';
  // The cleanup below pauses only the media this card owns; a different card's
  // clip (or the viewer's) must never be stopped by this card going away.
  const activeRef = useRef(active);
  activeRef.current = active;

  const anchorRef = useRef<React.ComponentRef<typeof View>>(null);

  // The poster covers the player until the player has something to show. It is
  // what removes the black frame: the native video surface is opaque but empty
  // until the first frame decodes, so a still drawn UNDER it would never be seen
  // (KNOT-042).
  const [posterVisible, setPosterVisible] = useState(true);

  const poster = typeof posterUri === 'string' && posterUri.trim() !== '' ? posterUri : null;
  const showPoster = poster !== null && posterVisible;

  // Attachment: the visible card claims the engine's surface and hands it back
  // when it leaves the screen or the viewer takes over. The media id is the whole
  // point — mounting this card again while the engine is already playing the same
  // clip resumes it instead of reloading it (KNOT-051).
  useEffect(() => {
    if (!isVisible) {
      player.releaseHost(id);
      if (activeRef.current) {
        player.pauseMedia(id);
      }
      return;
    }

    if (fullscreen) {
      // The viewer holds the surface now. Give up the hole, but do NOT pause: this
      // card may be the very clip the viewer is showing, and pausing here would
      // stop the picture the reader just opened (KNOT-051).
      player.releaseHost(id);
      return;
    }

    player.registerHost(id, anchorRef.current);
    player.playMedia({ id, uri, mode: 'inline' });

    return () => {
      player.releaseHost(id);
    };
  }, [fullscreen, id, isVisible, player, uri]);

  // The first measurement has to wait for the play to commit: the engine only
  // measures the host it considers active, so registering before its state has
  // updated would measure nothing. This effect runs on that commit, and again
  // whenever this card becomes the active one (for example after the viewer
  // hands the surface back).
  useEffect(() => {
    if (!active || fullscreen || !isVisible) {
      return;
    }
    player.registerHost(id, anchorRef.current);
    player.measureHost();
  }, [active, fullscreen, id, isVisible, player]);

  // One mute setting governs every video, so the active surface publishes it to
  // the one player. A card that is not playing never touches it, or an off-screen
  // entry could mute the viewer.
  useEffect(() => {
    if (active && !fullscreen) {
      player.setMuted(isMuted);
    }
  }, [active, fullscreen, isMuted, player]);

  // The still is retired once the picture is really on screen, and comes back for
  // a fresh load of the same clip. A progress tick is the only proof that a frame
  // was drawn: on Android `onReadyForDisplay` fires when the player is prepared,
  // before the decoder has produced a picture (KNOT-048-fix).
  useEffect(() => {
    setPosterVisible(!(active && playback.frameVisible));
  }, [active, playback.frameVisible]);

  // A video's payload rarely carries dimensions; the engine's metadata is the
  // only source of its natural shape (KNOT-020g-fix7).
  useEffect(() => {
    const size = playback.naturalSize;
    if (active && size !== null && onLoadSize !== undefined) {
      onLoadSize(size.width, size.height);
    }
  }, [active, onLoadSize, playback.naturalSize]);

  const seedSeconds =
    typeof durationMs === 'number' && Number.isFinite(durationMs) && durationMs > 0
      ? durationMs / 1000
      : 0;
  const durationSeconds = active && playback.duration > 0 ? playback.duration : seedSeconds;
  const currentSeconds = active ? playback.currentTime : 0;
  const progressLabel = formatProgress(currentSeconds, durationSeconds);

  const handleLayout = useCallback((): void => {
    player.measureHost();
  }, [player]);

  return (
    <View style={styles.container} testID={testID}>
      {/* Behind the video: covers the gap before the first frame, and stands in
          if the video can never play. */}
      <View style={styles.fallback} />

      {/* The hole the engine's single player is drawn into. It is measured in the
          engine's coordinate space, and the containing list re-measures it on a
          scroll tick — so the picture follows the card without re-rendering a
          single row, and without a native player per entry (KNOT-051). The anchor
          is intentionally intangible. */}
      <View
        ref={anchorRef}
        style={styles.videoAnchor}
        onLayout={handleLayout}
        pointerEvents="none"
        testID="feed-video-host"
      />

      {/* The still, over the player and under the chrome. Removed the moment the
          player renders a frame, so a video that is playing is never hidden by
          its own poster (KNOT-042). The wrapper is what makes it untouchable: an
          `Image` has no pointerEvents prop, and a touch that landed on the still
          would never reach the card's own tap target. */}
      {showPoster ? (
        <View style={styles.poster} pointerEvents="none">
          <Image
            source={{ uri: poster }}
            style={styles.posterImage}
            resizeMode="cover"
            accessibilityIgnoresInvertColors
            testID="feed-video-poster"
          />
        </View>
      ) : null}

      {showTag ? (
        <View style={styles.mediaTag} pointerEvents="none">
          <Text style={[typeScale.label, mediaOverlayText(colors)]}>Video</Text>
        </View>
      ) : null}

      {progressLabel !== '' ? (
        <View
          style={[
            styles.durationBadge,
            durationPosition === 'bottom-left'
              ? styles.durationBottomLeft
              : styles.durationTopRight,
          ]}
          pointerEvents="none"
          testID="video-progress-badge"
        >
          <Text style={[typeScale.label, tabularNums, mediaOverlayText(colors)]}>
            {progressLabel}
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
          <SpeakerSlash color={colors.text.onMedia} size={MUTE_ICON_SIZE} weight="fill" />
        ) : (
          <SpeakerHigh color={colors.text.onMedia} size={MUTE_ICON_SIZE} weight="fill" />
        )}
      </Pressable>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
    // The extracted still sits exactly where the picture will, so removing it is
    // invisible: the player's first frame has the same framing (KNOT-042).
    poster: {
      ...StyleSheet.absoluteFillObject,
    },
    posterImage: {
      height: '100%',
      width: '100%',
    },
    video: {
      ...StyleSheet.absoluteFillObject,
    },
    videoAnchor: {
      ...StyleSheet.absoluteFillObject,
    },
  });
