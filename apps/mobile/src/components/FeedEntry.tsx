import React, { useState } from 'react';
import {
  Image,
  Pressable,
  StyleSheet,
  Text,
  View,
  type ImageLoadEventData,
  type NativeSyntheticEvent,
} from 'react-native';
import { Images } from 'phosphor-react-native';

import { API_BASE_URL } from '../config/api';
import { languageName } from '../data/languages';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../theme';
import { PLACEHOLDER_MEDIA_ASPECT_RATIO } from '../utils/mediaFrame';
import { useMediaAspectRatio } from '../utils/useMediaAspectRatio';
import AuthorLine from './AuthorLine';
import { feedMetaLine, isPreviewTruncated } from './feedEntryMeta';
import FeedVideo, { VIDEO_MUTE_BUTTON_SIZE } from './FeedVideo';
import PillarTag from './PillarTag';

/**
 * The content a feed entry renders, in the shape both kinds share: a story, and --
 * since KNOT-022 -- somebody's adaptation of one.
 *
 * The name says "story" because every field here is story content that the server
 * sends on both kinds of entry. What makes an entry an adaptation is not a field in
 * this object but the `variant` prop, which says how to label it.
 */
type FeedEntryStory = {
  readonly id: string;
  readonly title: string;
  readonly pillar: 'wonder' | 'heritage';
  readonly preview: string;
  /**
   * Whether `preview` is an excerpt rather than the whole body, as the server
   * reports it (`preview_truncated`). When absent the entry falls back to reading
   * a trailing ellipsis off the text, so a response from an older server still
   * reads correctly (KNOT-020g-fix12).
   */
  readonly preview_truncated?: boolean;
  readonly language: string;
  readonly place?: string | null;
  /**
   * Whether the story was flagged sensitive when it was told, inherited by every
   * adaptation of it. A sensitive entry holds its media behind a scrim until the
   * reader asks to see it (KNOT-023).
   */
  readonly sensitive?: boolean;
  readonly author_display_name?: string | null;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: { readonly place: string; readonly duration_bucket: string } | null;
  readonly created_at: string;
  readonly version_count?: number;
  readonly language_count?: number;
  readonly comment_count?: number;
  readonly media_count?: number;
  /** The first media item's kind, or null when the story has no media. */
  readonly first_media_type?: 'image' | 'video' | null;
  /** The first media item's relative path on the API, or null/empty for none. */
  readonly first_media_url?: string | null;
  /**
   * The first media item's pixel dimensions, or null when the uploader's client
   * did not report them. They let the media render at its natural width:height
   * ratio, so a portrait photo is shown whole rather than cropped into a fixed
   * frame (KNOT-020g-fix5).
   */
  readonly first_media_width?: number | null;
  readonly first_media_height?: number | null;
  readonly reactions?: {
    readonly rings_true: number;
    readonly know_it_differently: number;
    readonly adds_something_new: number;
    readonly needs_a_source: number;
  };
};

type FeedEntryProps = {
  /** The story to render. */
  readonly story: FeedEntryStory;
  /**
   * How to read the entry: `story` (the default) is somebody's own story, and
   * `version` is an adaptation of somebody else's, which labels the entry with its
   * position and language rather than its pillar, and shows the server's
   * attribution sentence (KNOT-022).
   */
  readonly variant?: 'story' | 'version';
  /**
   * The entry's position in its lineage, counting the original as 1, for the
   * `version` variant's badge. The server sends it as `version_number`.
   */
  readonly versionNumber?: number;
  /**
   * The server-rendered sentence tracing this version back to the original, for
   * the `version` variant. It is shown verbatim -- the server owns the wording --
   * and is absent (or empty) when the lineage could not be resolved, in which case
   * the entry simply shows no line (KNOT-021).
   */
  readonly attributionLine?: string;
  /** Called when the entry is tapped. */
  readonly onPress: () => void;
  /** Called when the author's identity is tapped. */
  readonly onOpenAuthor?: () => void;
  /**
   * True when this entry is the one the feed judges to be on screen. It is the
   * single input that plays or pauses the entry's video, so only one video in the
   * feed is ever playing (KNOT-020c-fix3). Defaults to `false`.
   */
  readonly isVisible?: boolean;
  /** The feed-wide mute state, shared by every video. Defaults to muted. */
  readonly isMuted?: boolean;
  /** Called when the entry's mute control is tapped. */
  readonly onToggleMute?: () => void;
  /** Test identifier. */
  readonly testID?: string;
};

/** The preview's line limit. */
const PREVIEW_LINES = 3;

/**
 * The attribution sentence's line limit. It is clamped rather than wrapped freely
 * because a deep lineage produces a long sentence, and an entry must not grow
 * taller than its content is worth (KNOT-022).
 */
const ATTRIBUTION_LINES = 2;

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

/**
 * How far the image is blurred while sensitive content is hidden. The value is
 * deliberately large enough that the picture reads as a shape rather than as
 * content, so the flag is meaningful on a phone screen (KNOT-023).
 */
const SENSITIVE_BLUR_RADIUS = 18;

/** A no-op for the mute control when a caller renders an entry without wiring one. */
const NOOP = (): void => undefined;

/**
 * Resolves a media path returned by the API against the base URL.
 *
 * The API sends a relative path (`/stories/{id}/media/{mid}/content`), so it is
 * joined to the API base. A missing or blank path resolves to the empty string,
 * which the caller reads as "no media".
 */
function resolveMediaUrl(path: string | null | undefined): string {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  return trimmed === '' ? '' : `${API_BASE_URL}${trimmed}`;
}

/**
 * One story in the feed, edge-to-edge with hairlines between entries.
 *
 * It is a full-bleed row on the canvas — no card, no border, no shadow — so the
 * feed reads as one continuous column. Its order is the reading order: the author
 * block, the pillar tag, the title, the opening lines, the media, then the meta
 * line. The whole entry is one tap target; the author identity inside it is a
 * nested target that opens the profile (KNOT-ADR-043).
 *
 * The author block is the design reference's three lines: avatar and name with the
 * relative time, then the quiet Rooted line (no dot, no pill), then the plain
 * pillar tag (no fill) — which is why the tag is asked for its `plain` variant
 * (KNOT-020c-fix3).
 *
 * The text column keeps the feed's horizontal inset, but the **media is
 * full-bleed**: no horizontal padding and no corner rounding. An image draws
 * directly; a video is handed to `FeedVideo`, which plays it in place when this
 * entry is the visible one. The count badge stacks above the video's mute control
 * so the two never collide.
 *
 * The media's frame is the **media's own shape**: full width, and a height that
 * follows the media's natural width:height ratio, so a landscape photo is short
 * and a portrait one is tall. The ratio comes from the payload when the server
 * reported the item's dimensions; otherwise it comes from the media itself, which
 * reports its natural size on load (KNOT-020g-fix7). Until one of them answers,
 * the frame is a full-width square of `bg.sunken` — the media fills whatever frame
 * it is given (`cover`), so a placeholder is never a bar.
 *
 * This component used to stay **hookless** so the tests could call it as a plain
 * function. It no longer is: resolving a frame from the loaded media needs state,
 * and the tests were moved to a suite that mocks the hook primitives instead. The
 * playback logic still lives in `FeedVideo`, which remains the only component that
 * owns a player.
 */
export default function FeedEntry({
  story,
  variant = 'story',
  versionNumber,
  attributionLine,
  onPress,
  onOpenAuthor,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  testID,
}: FeedEntryProps): React.ReactElement {
  const mediaUrl = resolveMediaUrl(story.first_media_url);
  const isVideo = story.first_media_type === 'video';
  const mediaCount = story.media_count ?? 0;
  // The reveal is per-card and session-only: leaving the feed and coming back
  // remounts the card, so sensitive media is hidden again (KNOT-023).
  const [sensitiveRevealed, setSensitiveRevealed] = useState(false);
  const sensitive = story.sensitive === true;
  const sensitiveHidden = sensitive && !sensitiveRevealed;
  const toggleSensitive = (): void => setSensitiveRevealed((current) => !current);
  const { aspectRatio, onLoadSize } = useMediaAspectRatio(
    story.first_media_width,
    story.first_media_height,
  );
  // The media's own shape when either source knows it; a neutral square until one
  // does. Never a fixed landscape frame — that is the crop this rule removes.
  const mediaFrame = aspectRatio ?? PLACEHOLDER_MEDIA_ASPECT_RATIO;

  // The server's answer when it sent one; otherwise the text's own trailing
  // ellipsis decides. A feed entry renders the whole body under a three-line
  // clamp, so the clamp draws the "…" the link follows (KNOT-020g-fix12).
  const previewTruncated = story.preview_truncated ?? isPreviewTruncated(story.preview);

  /** Takes the shape off a decoded image, for media the payload could not size. */
  const handleImageLoad = (event: NativeSyntheticEvent<ImageLoadEventData>): void => {
    const { width, height } = event.nativeEvent.source;
    onLoadSize(width, height);
  };

  const metaLine = feedMetaLine({
    commentCount: story.comment_count,
    languageCount: story.language_count,
    reactions: story.reactions,
    versionCount: story.version_count,
  });

  const isVersion = variant === 'version';
  // The badge names the version and the language it was written in, which is what
  // makes an adaptation legible before the text is read: "Version 2 · Zulu". The
  // style upper-cases it, so the wording is written in sentence case here.
  const versionBadge =
    versionNumber === undefined
      ? `Version in ${languageName(story.language)}`
      : `Version ${versionNumber} · ${languageName(story.language)}`;

  return (
    <Pressable
      style={styles.entry}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={
        isVersion ? `Open this version: ${story.title}` : `Open the story: ${story.title}`
      }
      testID={testID}
    >
      <View style={styles.padded}>
        <AuthorLine
          displayName={story.author_display_name}
          avatarUrl={story.author_avatar_url}
          createdAt={story.created_at}
          rooted={story.author_rooted ?? null}
          size="large"
          showRootedOnNewLine
          onPress={onOpenAuthor}
        />

        {/* An adaptation is labelled by its inherited pillar, its version, and its
            language: the pillar frames the story the adaptation belongs to, and
            the version badge says which telling this is (KNOT-022, KNOT-023). */}
        {isVersion ? (
          <View style={[styles.tagRow, styles.versionRow]}>
            <PillarTag pillar={story.pillar} variant="plain" />
            <Text testID="version-badge" style={[typeScale.overline, styles.versionBadge]}>
              {versionBadge}
            </Text>
          </View>
        ) : (
          <View style={styles.tagRow}>
            <PillarTag pillar={story.pillar} place={story.place} variant="plain" />
          </View>
        )}

        <Text style={[typeScale.title, styles.title]}>{story.title}</Text>

        {/* The attribution sentence sits under the title: the title says what this
            version is called, and the line under it says whose story it adapts. It
            is rendered verbatim, because the server owns the wording (KNOT-021). */}
        {isVersion && attributionLine !== undefined && attributionLine !== '' ? (
          <Text style={[typeScale.bodyS, styles.attribution]} numberOfLines={ATTRIBUTION_LINES}>
            {attributionLine}
          </Text>
        ) : null}

        <Text style={[typeScale.body, styles.preview]} numberOfLines={PREVIEW_LINES}>
          {story.preview}
        </Text>

        {/* "Read more" continues the truncated preview. It is a second tap target
            on the same handler as the entry, so tapping it opens the story — and a
            tap that misses it still opens the story, because the whole entry is
            the outer target (KNOT-020g-fix12). */}
        {previewTruncated ? (
          <Pressable
            style={styles.readMore}
            onPress={onPress}
            hitSlop={spacing.sm}
            accessibilityRole="link"
            accessibilityLabel={`Read more of ${story.title}`}
          >
            <Text style={[typeScale.label, styles.readMoreText]}>Read more</Text>
          </Pressable>
        ) : null}
      </View>

      {mediaUrl !== '' ? (
        <View style={[styles.mediaFrame, { aspectRatio: mediaFrame }]}>
          {isVideo ? (
            <FeedVideo
              uri={mediaUrl}
              isVisible={isVisible}
              isMuted={isMuted}
              onToggleMute={onToggleMute}
              onLoadSize={onLoadSize}
            />
          ) : (
            <Image
              source={{ uri: mediaUrl }}
              style={styles.media}
              resizeMode="cover"
              blurRadius={sensitiveHidden ? SENSITIVE_BLUR_RADIUS : 0}
              onLoad={handleImageLoad}
              accessibilityIgnoresInvertColors
            />
          )}

          {/* A video frame cannot be blurred in place, so the same scrim covers
              the player and hides its chrome until the reader asks for it
              (KNOT-023). */}
          {sensitiveHidden ? (
            <Pressable
              style={styles.sensitiveOverlay}
              onPress={toggleSensitive}
              accessibilityRole="button"
              accessibilityLabel="Show sensitive content"
              testID="sensitive-overlay"
            >
              <Text style={[typeScale.caption, styles.sensitiveOverlayText]}>
                Sensitive content — tap to reveal
              </Text>
            </Pressable>
          ) : null}

          {/* The badge is both the flag and the control: tapping it hides the
              media again, so the reveal toggles without covering a video's
              controls (KNOT-023). */}
          {sensitive ? (
            <Pressable
              style={styles.sensitiveBadge}
              onPress={toggleSensitive}
              hitSlop={spacing.sm}
              accessibilityRole="button"
              accessibilityLabel={
                sensitiveHidden ? 'Sensitive content, hidden' : 'Sensitive content, shown'
              }
              testID="sensitive-badge"
            >
              <Text style={[typeScale.caption, styles.sensitiveBadgeText]}>Sensitive</Text>
            </Pressable>
          ) : null}

          {mediaCount > 1 ? (
            <View
              style={[styles.countBadge, isVideo ? styles.countBadgeOverVideo : null]}
              accessibilityLabel={`${mediaCount} media items`}
            >
              <Images color={colors.text.onBrand} size={COUNT_ICON_SIZE} weight="bold" />
              <Text style={[typeScale.caption, tabularNums, styles.countBadgeText]}>
                {mediaCount}
              </Text>
            </View>
          ) : null}
        </View>
      ) : null}

      <View style={styles.padded}>
        {metaLine !== '' ? (
          <Text style={[typeScale.caption, tabularNums, styles.metaText]}>{metaLine}</Text>
        ) : null}

        <View style={styles.hairline} />
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  attribution: {
    // Quiet: the attribution supports the entry, it does not compete with the
    // title or the preview (KNOT-022).
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  countBadge: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.sm,
    bottom: spacing.sm,
    flexDirection: 'row',
    gap: spacing.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    position: 'absolute',
    right: spacing.sm,
  },
  // A video owns the bottom-right corner with its mute control, so the count
  // badge sits directly above it instead of underneath it.
  countBadgeOverVideo: {
    bottom: spacing.sm + VIDEO_MUTE_BUTTON_SIZE + spacing.xs,
  },
  countBadgeText: {
    color: colors.text.onBrand,
  },
  entry: {
    paddingTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.lg,
  },
  media: {
    height: '100%',
    width: '100%',
  }, // Full-bleed: no horizontal inset and no rounding, so the media runs edge to
  // edge (KNOT-020c-fix2). The ratio is set per entry from the media's own shape,
  // so it is not fixed here (KNOT-020g-fix5); the fill is `cover`, so the frame is
  // always filled and never barred (KNOT-020g-fix6).
  mediaFrame: {
    backgroundColor: colors.bg.sunken,
    marginTop: spacing.md,
    overflow: 'hidden',
    width: '100%',
  },
  metaText: {
    color: colors.text.tertiary,
    marginTop: spacing.md,
  },
  // The text column keeps the feed inset; only the media leaves it.
  padded: {
    paddingHorizontal: layout.screenPaddingFeed,
  },
  preview: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
  },
  // The "read more" affordance under a truncated preview: a text link in the brand
  // ink, with hit slop taking the small label up to a comfortable touch target.
  readMore: {
    alignSelf: 'flex-start',
    marginTop: spacing.xs,
  },
  readMoreText: {
    color: colors.brand.primary,
  },
  // The version badge: the overline voice in the brand ink, marking the entry as an
  // adaptation at a glance (KNOT-022).
  versionBadge: {
    color: colors.brand.primary,
  },
  // The pillar and the version badge sit side by side, so an adaptation reads as
  // "Wonder · Version 2 · Zulu" without the two labels competing (KNOT-023).
  versionRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  tagRow: {
    marginTop: spacing.md,
  },
  // The sensitive flag, drawn over the media's bottom-left corner so it never
  // collides with the media-count badge or a video's mute control (KNOT-023).
  sensitiveBadge: {
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.sm,
    bottom: spacing.sm,
    left: spacing.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    position: 'absolute',
  },
  sensitiveBadgeText: {
    color: colors.text.onBrand,
  },
  // The scrim that holds sensitive media until it is revealed. It is opaque
  // enough to read as a cover rather than a tint, and the centred line says what
  // to do about it (KNOT-023).
  sensitiveOverlay: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
    bottom: 0,
    justifyContent: 'center',
    left: 0,
    paddingHorizontal: spacing.lg,
    position: 'absolute',
    right: 0,
    top: 0,
  },
  sensitiveOverlayText: {
    color: colors.text.onBrand,
    textAlign: 'center',
  },
  title: {
    color: colors.text.primary,
    marginTop: spacing.sm,
  },
});
