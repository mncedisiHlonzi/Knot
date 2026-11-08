import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import Video, { type OnLoadData, type OnProgressData } from 'react-native-video';
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
import { cachedVideoUri } from '../utils/videoCache';
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
   * True only for the card the list has settled on: the entry the screen decided
   * is at least 90% on screen while the list is at rest. It is the single input
   * that attaches this surface to the engine, and therefore the single input that
   * mounts the one `<Video>` — a card that is scrolling, or that is only partly on
   * screen, shows its poster instead (KNOT-020c-fix3, KNOT-049). Defaults to
   * `false`, which draws the card with no player at all.
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
 * A video surface that lives **inside the card** (KNOT-050).
 *
 * The card owns a real `<Video>`: it is mounted in the card's own layout, sized to
 * the media frame, and it scrolls with the card at the platform's frame rate. That
 * is the whole fix for the floating, stuttering picture — there is nothing to
 * measure and nothing to reposition while the list moves, because the player *is*
 * part of what moves.
 *
 * Only one card in a list draws it: the screen decides which entry is active (the
 * one the viewport is showing) and every other card renders its still alone, so at
 * most one decoder exists per list. The player is unmounted, not just paused, when
 * its card stops being the active one — that is the memory bound the inline
 * placement has to keep.
 *
 * The component still talks to `MediaPlayerContext`, because the *shared* facts are
 * worth keeping: the card publishes where its clip has reached (`reportProgress`),
 * so opening the full-screen viewer on the same video carries on at that second
 * instead of restarting at 0:00 (KNOT-051).
 *
 * The chrome stays here — the "Video" tag, the running time and the mute control —
 * because it belongs to the card. Every screen that draws a story's video uses this
 * component (the Feed, Place Stories, the profile wall, the Story Detail and Story
 * Version carousels).
 *
 * The caller owns the frame's shape: `FeedEntry` and the profile card set it from
 * the payload's reported dimensions, reserving the design's 4:5 frame when the
 * payload has none, so the frame never changes height once it has been painted
 * (KNOT-020g-fix6, KNOT-048-fix2).
 */
export default function FeedVideo({
  uri,
  mediaId,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
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
  const fullscreen = playback.mode === 'fullscreen';
  // One rule decides whether this card draws a player at all: it must be the entry
  // the screen chose AND the viewer must not be holding the theatre. A card that is
  // scrolled away, that is not the settled one, or that sits behind the full-screen
  // viewer draws its still and nothing else (KNOT-050).
  const mounted = isVisible && !fullscreen;

  // The local cache proxy is applied to the source, so a second pass over the same
  // clip re-reads it from the device instead of downloading it again
  // (KNOT-037-fix10).
  const sourceUri = useMemo(() => cachedVideoUri(uri), [uri]);

  const videoRef = useRef<React.ComponentRef<typeof Video>>(null);

  // The still covers the player until the player has something to show: the native
  // surface is opaque but empty until the first frame decodes, so a still drawn
  // UNDER it would never be seen (KNOT-042). A progress tick is the only proof a
  // frame was drawn — on Android `onReadyForDisplay` fires when the player is
  // prepared, before the decoder has produced a picture (KNOT-048-fix).
  const [frameDrawn, setFrameDrawn] = useState(false);
  const [currentSeconds, setCurrentSeconds] = useState(0);
  const [loadedSeconds, setLoadedSeconds] = useState(0);

  const poster = typeof posterUri === 'string' && posterUri.trim() !== '' ? posterUri : null;
  const showPoster = poster !== null && !(mounted && frameDrawn);

  // Where to pick the picture up when this card's player mounts. Closing the
  // viewer must not restart the clip, and the engine is the only thing that knows
  // where the viewer left it (KNOT-051, KNOT-050).
  const resumeAt = useRef(0);

  // Mounting claims the clip in the engine — which is what lets the viewer open on
  // the same second — and remembers the position to resume from.
  useEffect(() => {
    if (!mounted) {
      setFrameDrawn(false);
      setCurrentSeconds(0);
      return;
    }

    const position = player.getCurrentTime();
    resumeAt.current = position > 0 ? position : 0;
    setCurrentSeconds(resumeAt.current);
    player.playMedia({ id, uri, mode: 'inline' });
  }, [id, mounted, player, uri]);

  // One mute setting governs the card's own player. A card that draws no player
  // never publishes it, or an off-screen entry could mute the viewer.
  useEffect(() => {
    if (mounted) {
      player.setMuted(isMuted);
    }
  }, [isMuted, mounted, player]);

  const handleLoad = useCallback(
    (data: OnLoadData): void => {
      const seconds = data.duration > 0 ? data.duration : 0;
      if (seconds > 0) {
        setLoadedSeconds(seconds);
      }
      // The engine keeps the clip's length too, so the viewer's transport knows it
      // before its own player has loaded anything.
      player.reportLoaded(id, seconds);

      // A re-seat: the same clip continues where it was left.
      const target = resumeAt.current;
      resumeAt.current = 0;
      if (target > 0) {
        videoRef.current?.seek(target);
        setCurrentSeconds(target);
      }
    },
    [id, player],
  );

  const handleProgress = useCallback(
    (data: OnProgressData): void => {
      setFrameDrawn(true);
      setCurrentSeconds(data.currentTime);
      // Publishes the playhead the full-screen viewer opens on (KNOT-051).
      player.reportProgress(id, data.currentTime);
    },
    [id, player],
  );

  // A video's payload rarely carries dimensions; the frame therefore reserves the
  // design's 4:5 shape rather than chasing the clip's natural size, which would
  // resize the card the moment the player read the file (KNOT-048-fix2).

  const seedSeconds =
    typeof durationMs === 'number' && Number.isFinite(durationMs) && durationMs > 0
      ? durationMs / 1000
      : 0;
  const durationSeconds = loadedSeconds > 0 ? loadedSeconds : seedSeconds;
  const progressLabel = formatProgress(currentSeconds, durationSeconds);

  return (
    <View style={styles.container} testID={testID}>
      {/* Behind the video: covers the gap before the first frame, and stands in
          if the video can never play. */}
      <View style={styles.fallback} />

      {/* The card's own player (KNOT-050). It is a child of this card, so the
          platform scrolls the picture with the card: nothing is measured and
          nothing is repositioned while the list moves, which is the whole reason
          there is no floating player any more. It is mounted only while this card
          is the active one; every other card draws its still alone. The wrapper is
          intangible because the native surface would otherwise swallow the card's
          own taps (KNOT-020k-fix5). */}
      {mounted ? (
        <View style={styles.videoAnchor} pointerEvents="none" testID="feed-video-surface">
          <Video
            ref={videoRef}
            source={{ uri: sourceUri }}
            style={styles.video}
            // The active card plays; a card that is not active has no player at all.
            paused={false}
            muted={isMuted}
            repeat
            resizeMode="cover"
            // A `TextureView` is an ordinary view: it stays inside this card's frame
            // and stacks with the chrome instead of punching through it
            // (KNOT-025-fix6, KNOT-048-fix2).
            useTextureView
            progressUpdateInterval={250}
            // The native surface is opaque and empty until the first frame decodes,
            // so the player is asked for the surround it sits in (KNOT-045).
            shutterColor={colors.bg.canvas}
            onLoad={handleLoad}
            onProgress={handleProgress}
            testID="feed-video-player"
          />
        </View>
      ) : null}

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
    // The card's own player fills the media frame exactly, so the picture is the
    // size of the still it replaces (KNOT-050).
    video: {
      height: '100%',
      width: '100%',
    },
    videoAnchor: {
      ...StyleSheet.absoluteFillObject,
    },
  });
