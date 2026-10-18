import React from 'react';
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
import Avatar from './Avatar';
import {
  attributionSegments,
  feedMetaLine,
  isPreviewTruncated,
  type AttributionSegment,
} from './feedEntryMeta';
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
  readonly author_display_name?: string | null;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: { readonly place: string; readonly duration_bucket: string } | null;
  /**
   * The ORIGINAL author of the story an adaptation adapts: the first link of its
   * lineage. The version variant heads the card with "{name}'s original story",
   * and these fields are what it reads -- the attribution sentence is never
   * parsed for them (KNOT-023a). A story entry carries none of them.
   */
  readonly root_author_id?: string;
  readonly root_author_display_name?: string | null;
  readonly root_author_avatar_url?: string | null;
  /**
   * The author of the version this one adapts directly -- the lineage actor the
   * attribution row credits first. For a first adaptation this is the same person
   * as the root author, and the row renders a single avatar (KNOT-022-fix2). A
   * story entry carries none of these.
   */
  readonly parent_author_id?: string;
  readonly parent_author_display_name?: string | null;
  readonly parent_author_avatar_url?: string | null;
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
   * the `version` variant. The card renders it verbatim in the attribution row,
   * drawing every name it mentions in a heavier voice -- the server owns the
   * wording (KNOT-021, KNOT-022-fix1). Empty when the lineage could not be
   * resolved, in which case the row falls back to the structured root author and,
   * failing that, to the quiet version label alone. The server sends the field as
   * `attribution_line`; the feed maps it onto this prop.
   */
  readonly attributionLine?: string;
  /**
   * Whether the entry draws its own bottom hairline. The Feed replaces that
   * inside-entry rule with a thick inter-card gap, so it passes `false`; Place
   * Stories keeps the default and its column stays hairline-separated
   * (KNOT-023b).
   */
  readonly showBottomHairline?: boolean;
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
 * The single-line attribution header's line limit. The sentence wraps when the
 * screen is narrow or the lineage is deep, but the header is a caption and must
 * not grow without bound (KNOT-023b).
 */
const ATTRIBUTION_LINES = 2;

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

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
 * Resolves an avatar path returned by the API against the base URL. A missing or
 * blank path resolves to `undefined`, which `Avatar` reads as "draw initials".
 */
function resolveAvatarUrl(path: string | null | undefined): string | undefined {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  return trimmed === '' ? undefined : `${API_BASE_URL}${trimmed}`;
}

/**
 * The attribution runs for a server that sent no sentence: the adapter and the
 * original author, in the same shape `attributionSegments` produces, so the row
 * renders the fallback as just another sentence (KNOT-022-fix1, KNOT-022-fix2).
 */
function fallbackAttributionRuns(
  adapterName: string,
  rootAuthorName: string,
): readonly AttributionSegment[] {
  if (rootAuthorName === '') {
    return [];
  }
  if (adapterName === '') {
    return [
      { text: 'Adapted from ', bold: false },
      { text: rootAuthorName, bold: true },
      { text: "'s original story.", bold: false },
    ];
  }

  return [
    { text: 'Adapted by ', bold: false },
    { text: adapterName, bold: true },
    { text: ' from ', bold: false },
    { text: rootAuthorName, bold: true },
    { text: "'s original story.", bold: false },
  ];
}

/** One face in the attribution row's lineage stack. */
type LineageAvatar = {
  readonly key: 'parent' | 'root';
  readonly uri: string | undefined;
  readonly name: string;
  readonly label: string;
};

/** The accessibility label for a lineage avatar, tolerating an unresolved name. */
function lineageAvatarLabel(name: string): string {
  return name === '' ? 'Lineage author\u2019s profile picture' : `${name}'s profile picture`;
}

/**
 * One story in the feed, as a white card on the feed's grey canvas.
 *
 * The card is full-bleed horizontally — no side border, no shadow — and its
 * separation from the next card is the Feed's grey gap, not a rule. (Place
 * Stories, the other caller, keeps the entry's bottom hairline.) Its order is the
 * reading order: the author block, the pillar tag, the title, the opening lines,
 * the media, then the meta line. The whole entry is one tap target; the author
 * identity inside it is a nested target that opens the profile (KNOT-ADR-043).
 *
 * The author block is a vertical stack (KNOT-023b): the avatar on the left and,
 * on the right, the name, the quiet Rooted line, then the compact time. A version
 * card additionally opens with an attribution row — a small stack of the LINEAGE
 * authors' avatars (the direct parent and the original) beside the server's
 * sentence, whose names are bolded — which is the only place that sentence, and
 * the "Version N · Language" label, are drawn. The poster is the AuthorLine below,
 * not a face in that stack (KNOT-022-fix1, KNOT-022-fix2).
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
  showBottomHairline = true,
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
  // The people the row credits are the LINEAGE, not the poster: the author of the
  // version this one adapts (the parent) and the story's original author (the
  // root). The poster -- this version's own author -- is already the AuthorLine
  // below, so repeating them in the stack credited the wrong person
  // (KNOT-022-fix2). The sentence is never parsed for these; the server sends the
  // structured fields from the same batched author read.
  const adapterName =
    typeof story.author_display_name === 'string' ? story.author_display_name.trim() : '';
  const rootAuthorID = typeof story.root_author_id === 'string' ? story.root_author_id.trim() : '';
  const rootAuthorName =
    typeof story.root_author_display_name === 'string' ? story.root_author_display_name.trim() : '';
  const rootAuthorAvatarUrl = resolveAvatarUrl(story.root_author_avatar_url);
  const parentAuthorID =
    typeof story.parent_author_id === 'string' ? story.parent_author_id.trim() : '';
  const parentAuthorName =
    typeof story.parent_author_display_name === 'string'
      ? story.parent_author_display_name.trim()
      : '';
  const parentAuthorAvatarUrl = resolveAvatarUrl(story.parent_author_avatar_url);

  const parentKnown = parentAuthorName !== '' || parentAuthorAvatarUrl !== undefined;
  const rootKnown = rootAuthorName !== '' || rootAuthorAvatarUrl !== undefined;
  // A first adaptation's parent IS the original, so the two structure fields name
  // the same person and only one face is drawn. The id is the reliable comparison;
  // the name is the fallback for a server that omits it.
  const sameLineageAuthor =
    parentKnown &&
    rootKnown &&
    (parentAuthorID !== '' && rootAuthorID !== ''
      ? parentAuthorID === rootAuthorID
      : parentAuthorName !== '' && parentAuthorName === rootAuthorName);

  const lineageAvatars: LineageAvatar[] = [];
  if (parentKnown) {
    lineageAvatars.push({
      key: 'parent',
      uri: parentAuthorAvatarUrl,
      name: parentAuthorName,
      label: lineageAvatarLabel(parentAuthorName),
    });
  }
  if (rootKnown && !sameLineageAuthor) {
    lineageAvatars.push({
      key: 'root',
      uri: rootAuthorAvatarUrl,
      name: rootAuthorName,
      label: lineageAvatarLabel(rootAuthorName),
    });
  }

  const attributionText = typeof attributionLine === 'string' ? attributionLine.trim() : '';
  // The sentence names the people in the lineage, so it is split into runs and
  // every name is drawn in the bold label voice (KNOT-022-fix1). Parsing is
  // display-only; an unexpected sentence falls back to one plain run.
  const attributionRuns: readonly AttributionSegment[] =
    attributionText !== ''
      ? attributionSegments(attributionText)
      : fallbackAttributionRuns(adapterName, rootAuthorName);
  // The version's position rides in this row, quietly, so the card's overline can
  // stay the pillar tag alone (KNOT-022-fix1). The sentence no longer carries the
  // target language either -- the reader is already looking at that telling -- so
  // a sentence contributes only the number; when it is missing, the full label is
  // drawn (KNOT-022-fix2).
  const versionLabel =
    versionNumber === undefined
      ? `Version in ${languageName(story.language)}`
      : `Version ${versionNumber} · ${languageName(story.language)}`;
  const attributionMeta =
    attributionText === '' || versionNumber === undefined
      ? versionLabel
      : `Version ${versionNumber}`;
  // A version card always carries a version label, so the row is drawn even when
  // the lineage could not be resolved; in that case it is the label alone.
  const showAttributionHeader = isVersion;

  return (
    <Pressable
      style={[styles.entry, showBottomHairline ? null : styles.entryCard]}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={
        isVersion ? `Open this version: ${story.title}` : `Open the story: ${story.title}`
      }
      testID={testID}
    >
      <View style={styles.padded}>
        {/* The attribution row: the lineage authors -- the direct parent and the
            original -- as stacked avatars, then a two-line social credit (the
            server's sentence with its names bolded, plus the quiet version
            label). The poster is deliberately absent: they are the AuthorLine
            below, not the lineage (KNOT-023b, KNOT-022-fix1, KNOT-022-fix2). */}
        {showAttributionHeader ? (
          <View style={styles.attributionRow} testID="attribution-header">
            {lineageAvatars.length > 0 ? (
              <View style={styles.attributionAvatars}>
                {lineageAvatars.map((actor, index) => (
                  <View
                    key={actor.key}
                    style={index === 0 ? null : styles.attributionAvatarStacked}
                  >
                    <Avatar
                      uri={actor.uri}
                      displayName={actor.name}
                      size="sm"
                      accessibilityLabel={actor.label}
                    />
                  </View>
                ))}
              </View>
            ) : null}
            <Text
              testID="attribution-sentence"
              style={[typeScale.bodyS, styles.attributionText]}
              numberOfLines={ATTRIBUTION_LINES}
            >
              {attributionRuns.map((run, index) =>
                run.bold ? (
                  <Text key={index} style={[typeScale.label, styles.attributionName]}>
                    {run.text}
                  </Text>
                ) : (
                  run.text
                ),
              )}
              {attributionRuns.length > 0 ? ' · ' : null}
              <Text style={[typeScale.caption, styles.attributionMeta]}>{attributionMeta}</Text>
            </Text>
          </View>
        ) : null}

        {/* A hairline separates the attribution line from the poster's author
            block, so the two identities read as distinct (KNOT-023c). */}
        {showAttributionHeader ? (
          <View style={styles.attributionDivider} testID="attribution-divider" />
        ) : null}

        <AuthorLine
          displayName={story.author_display_name}
          avatarUrl={story.author_avatar_url}
          createdAt={story.created_at}
          rooted={story.author_rooted ?? null}
          size="large"
          // The feed's social-card author block: name, Rooted, then time, with a
          // left avatar. The compact time is the feed's alone (KNOT-023b).
          layout="stacked"
          timeFormat="short"
          onPress={onOpenAuthor}
        />

        {/* Both kinds open with the same pillar tag -- "WONDER · ARLIT" -- so an
            adaptation reads as a story filed under a pillar. Which telling it is
            lives in the attribution row above (KNOT-022-fix1). */}
        <View style={styles.tagRow}>
          <PillarTag pillar={story.pillar} place={story.place} variant="plain" />
        </View>

        <Text style={[typeScale.title, styles.title]}>{story.title}</Text>

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
              onLoad={handleImageLoad}
              accessibilityIgnoresInvertColors
            />
          )}

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

        {showBottomHairline ? <View style={styles.hairline} /> : null}
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // The attribution row: stacked avatars on the left, a two-line social credit on
  // the right. It is a label above the author block, not a byline of its own
  // (KNOT-022-fix1).
  attributionRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  attributionAvatars: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  // Each later lineage face sits over the one before it, ringed in the canvas so
  // the faces stay legible where they meet (KNOT-022-fix2).
  attributionAvatarStacked: {
    backgroundColor: colors.bg.canvas,
    borderColor: colors.bg.canvas,
    borderRadius: radius.pill,
    borderWidth: 2,
    marginLeft: -spacing.sm,
  },
  attributionName: {
    color: colors.text.primary,
  },
  // The version label rides at the end of the sentence in the quietest ink, so
  // the credit reads as one line rather than a badge (KNOT-022-fix1).
  attributionMeta: {
    color: colors.text.tertiary,
  },
  attributionText: {
    color: colors.text.secondary,
    flexShrink: 1,
  },
  // The rule between the attribution line and the poster's author block
  // (KNOT-023c). It is the design system's hairline, not the heavier divider.
  attributionDivider: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginBottom: spacing.sm,
    marginTop: spacing.sm,
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
  // The Feed's card form: without its bottom hairline, the entry needs its own
  // bottom inset so the next card's grey gap does not touch the meta line
  // (KNOT-023b).
  entryCard: {
    paddingBottom: spacing.lg,
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
  tagRow: {
    marginTop: spacing.md,
  },
  title: {
    color: colors.text.primary,
    marginTop: spacing.sm,
  },
});
