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
import { DotsThree, Images, ShareNetwork, X } from 'phosphor-react-native';

import type { RecentInteraction } from '../api/stories';
import { API_BASE_URL } from '../config/api';
import {
  layout,
  mediaOverlayText,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import { PLACEHOLDER_MEDIA_ASPECT_RATIO } from '../utils/mediaFrame';
import { useMediaAspectRatio } from '../utils/useMediaAspectRatio';
import AuthorLine from './AuthorLine';
import { feedMetaLine, isPreviewTruncated } from './feedEntryMeta';
import FeedVideo, { VIDEO_MUTE_BUTTON_SIZE } from './FeedVideo';
import PillarTag from './PillarTag';
import type { ViewerMedia, ViewerStory } from './MediaGalleryModal';
import { hasSocialProof } from './socialProof';
import SocialProofHeader, { type SocialProofAvatar } from './SocialProofHeader';

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
  /**
   * The STORY this entry belongs to, when it differs from `id`. A version entry's
   * `id` is the version (what the card opens), so the viewer's "View Story" needs
   * the story it adapts instead (KNOT-020k-fix2). Absent on a story entry, where
   * `id` is already the story.
   */
  readonly storyId?: string;
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
  /**
   * The entry's latest social proof, newest first. A version card heads the entry
   * with it, falling back to `attributionLine` when it is empty (KNOT-028).
   */
  readonly recent_interactions?: readonly RecentInteraction[];
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
   * The entry's position in its lineage, counting the original as 1. The server
   * sends it as `version_number`, and it is still carried here for callers, but the
   * feed card no longer displays it: the attribution sentence ends after the
   * language (KNOT-025-fix4).
   */
  readonly versionNumber?: number;
  /**
   * The server-rendered sentence tracing this version back to the original, for
   * the `version` variant. The card hands it to `SocialProofHeader`, which shows
   * it verbatim when the version has no interactions, drawing every name in a
   * heavier voice -- the server owns the wording (KNOT-021, KNOT-022-fix1,
   * KNOT-028). Empty when the lineage could not be resolved, in which case the
   * header falls back to the structured root author and, failing that, draws
   * nothing. The server sends the field as `attribution_line`; the feed maps it
   * onto this prop.
   */
  readonly attributionLine?: string;
  /**
   * Whether the entry draws its own bottom hairline. The Feed closes each card with
   * its own 4pt band instead, so it passes `false`; Place Stories keeps the default
   * and its column stays hairline-separated (KNOT-023b, KNOT-031, KNOT-020k-fix20).
   */
  readonly showBottomHairline?: boolean;
  /** Called when the entry is tapped. */
  readonly onPress: () => void;
  /** Called when the author's identity is tapped. */
  readonly onOpenAuthor?: () => void;
  /**
   * Called when the card's overflow control is tapped, to open the post menu.
   * When omitted, no control is drawn (Place Stories uses the card without a
   * menu). The Feed passes it for every entry (KNOT-031).
   */
  readonly onOpenOptions?: () => void;
  /**
   * Called when the card's hide control is tapped: a one-tap "hide this post"
   * that skips the menu entirely. When omitted, no control is drawn, so a caller
   * that has nothing to hide the entry from draws no dead target (KNOT-032).
   */
  readonly onHide?: () => void;
  /**
   * Called when the media itself is tapped, to open the full-screen viewer. The
   * media area is its own tap target, so a tap there never also opens the story
   * (KNOT-020k). The third argument is the story context the viewer's sheet
   * presents (KNOT-020k-fix2).
   */
  readonly onOpenMedia?: (
    items: readonly ViewerMedia[],
    index: number,
    story?: ViewerStory | null,
  ) => void;
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

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

/** The adaptation marker's glyph size: small enough to ride beside the name. */
const ADAPT_BADGE_SIZE = 16;

/** The overflow glyph's size; its button is the 44pt touch target. */
const MENU_ICON_SIZE = 22;

/** The hide glyph's size: one step smaller than the overflow mark, so the pair
 * reads as `⋯` then `✕` rather than as two equal, competing buttons. */
const HIDE_ICON_SIZE = 20;

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

/** The accessibility label for a lineage avatar, tolerating an unresolved name. */
function lineageAvatarLabel(name: string): string {
  return name === '' ? 'Lineage author\u2019s profile picture' : `${name}'s profile picture`;
}

/**
 * One story in the feed, as a card on the feed's canvas.
 *
 * The card is full-bleed horizontally — no side border, no shadow — and the Feed
 * closes one card with a 4pt band of its own rather than with a rule drawn inside
 * the entry. (Place Stories, the other caller, keeps the entry's bottom hairline.)
 * Its order is the reading order: the author block, the pillar tag, the
 * title, the opening lines, the media, then the meta line. The whole entry is one
 * tap target; the author identity inside it is a nested target that opens the
 * profile (KNOT-ADR-043), and so are the two quick actions the Feed supplies —
 * the overflow control (`onOpenOptions`) and the one-tap hide (`onHide`)
 * (KNOT-031, KNOT-032).
 *
 * The author block is a vertical stack (KNOT-023b): the avatar on the left and,
 * on the right, the name, the quiet Rooted line, then the compact time. Every card
 * opens with a `SocialProofHeader` (KNOT-029): the newest interactions if there
 * are any; a version falls back to the lineage avatars and the server's
 * attribution sentence when there are none, while a story (which has no
 * attribution) draws nothing. A version card also carries a permanent adapt badge
 * beside the adapter's name, so the adaptation stays legible when interactions
 * replace the sentence (KNOT-029). The poster is the AuthorLine below, not a face
 * in that header (KNOT-022-fix1, KNOT-022-fix2). The version number is deliberately
 * absent from the feed: the credit ends after the language (KNOT-025-fix4).
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
  attributionLine,
  showBottomHairline = true,
  onPress,
  onOpenAuthor,
  onOpenOptions,
  onHide,
  onOpenMedia,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  testID,
}: FeedEntryProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
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

  // The full-screen viewer takes the same item shape as `StoryMedia`; a feed
  // entry carries only its first item, so its viewer is a single page
  // (KNOT-020k).
  const firstMediaPath =
    typeof story.first_media_url === 'string' ? story.first_media_url.trim() : '';
  const viewerItems: readonly ViewerMedia[] =
    firstMediaPath === ''
      ? []
      : [
          {
            id: story.id,
            media_type: isVideo ? 'video' : 'image',
            url: firstMediaPath,
            width: story.first_media_width ?? null,
            height: story.first_media_height ?? null,
          },
        ];

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

  // The viewer's overlay draws the same story content this card does (author,
  // title, opening lines). The media array carries the item that was tapped, so
  // the viewer receives a complete `ViewerStory` — the same shape Story Detail
  // hands it — and opens on the first frame instead of waiting for `App` to read
  // the story first. A card holds one preview, so this is a one-item gallery
  // (KNOT-020k-fix11).
  const viewerStory: ViewerStory = {
    storyId: story.storyId ?? story.id,
    title: story.title,
    body: story.preview,
    media: viewerItems,
    authorDisplayName: story.author_display_name,
    authorAvatarUrl: story.author_avatar_url,
    createdAt: story.created_at,
  };
  // The people the row credits are the LINEAGE, not the poster: the author of the
  // version this one adapts (the parent) and the story's original author (the
  // root). The poster -- this version's own author -- is already the AuthorLine
  // below, so repeating them in the stack credited the wrong person
  // (KNOT-022-fix2). The sentence is never parsed for these; the server sends the
  // structured fields from the same batched author read.
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

  const lineageAvatars: SocialProofAvatar[] = [];
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

  // The header is drawn only when it has something to say, so the card's top inset
  // follows it: a card WITH a header opens with the compact `spacing.sm` the band is
  // built around, while a card without one keeps the taller resting inset that the
  // author block needs (KNOT-032-fix2).
  const showSocialProof = hasSocialProof(
    story.recent_interactions,
    isVersion ? attributionText : '',
    isVersion ? rootAuthorName : '',
  );

  // A version's telling is always marked, even when its attribution sentence has
  // been replaced by interactions. The badge is a status marker beside the
  // adapter's name, not a tap target, so it stays out of the profile Pressable
  // (KNOT-029).
  const adaptBadge = isVersion ? (
    <View
      style={styles.adaptBadge}
      testID="adapt-badge"
      accessibilityRole="image"
      accessibilityLabel="Adapted story"
    >
      <ShareNetwork color={colors.brand.primary} size={ADAPT_BADGE_SIZE} weight="bold" />
    </View>
  ) : null;

  return (
    <Pressable
      style={[
        styles.entry,
        showSocialProof ? styles.entryWithSocialProof : null,
        // A card that ends in a meta row hands its bottom inset to that row, so the
        // counts sit symmetrically and the Feed's band sits flush under them
        // (KNOT-020k-fix21).
        showBottomHairline ? null : metaLine === '' ? styles.entryCard : styles.entryCardWithMeta,
      ]}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={
        isVersion ? `Open this version: ${story.title}` : `Open the story: ${story.title}`
      }
      testID={testID}
    >
      <View style={styles.padded}>
        {/* The social-proof header is universal: every card shows its newest
            interactions. A version falls back to its hereditary attribution when
            there are none; a story has no attribution, so it simply draws no
            header (KNOT-028, KNOT-029). */}
        <SocialProofHeader
          interactions={story.recent_interactions}
          attributionLine={isVersion ? attributionText : ''}
          originalAuthorName={isVersion ? rootAuthorName : ''}
          lineageAvatars={isVersion ? lineageAvatars : []}
        />

        <View style={styles.authorRow}>
          <View style={styles.authorLine}>
            <AuthorLine
              displayName={story.author_display_name}
              avatarUrl={story.author_avatar_url}
              createdAt={story.created_at}
              rooted={story.author_rooted ?? null}
              size="large"
              // The feed's social-card author block: name, Rooted, then time, with
              // a left avatar. The compact time is the feed's alone (KNOT-023b).
              layout="stacked"
              timeFormat="short"
              badge={adaptBadge}
              onPress={onOpenAuthor}
            />
          </View>

          {/* Two nested Pressables take the touch themselves, so a quick action
              never also opens the card (the same isolation the author target
              uses, KNOT-ADR-043). Each 44pt box is its own touch target, and the
              pair sits at the card's top-right in the order a reader scans them:
              `⋯` for the menu, then `✕` for the one-tap hide (KNOT-031,
              KNOT-032). */}
          {onOpenOptions === undefined && onHide === undefined ? null : (
            <View style={styles.cardActions}>
              {onOpenOptions === undefined ? null : (
                <Pressable
                  style={styles.cardAction}
                  onPress={onOpenOptions}
                  accessibilityRole="button"
                  accessibilityLabel="Post options"
                  testID="post-options"
                >
                  <DotsThree color={colors.text.tertiary} size={MENU_ICON_SIZE} weight="bold" />
                </Pressable>
              )}

              {onHide === undefined ? null : (
                <Pressable
                  style={styles.cardAction}
                  onPress={onHide}
                  accessibilityRole="button"
                  accessibilityLabel="Hide this post"
                  testID="post-hide"
                >
                  <X color={colors.text.tertiary} size={HIDE_ICON_SIZE} weight="bold" />
                </Pressable>
              )}
            </View>
          )}
        </View>

        {/* Both kinds open with the same pillar tag -- "WONDER · ARLIT" -- so an
            adaptation reads as a story filed under a pillar. Which telling it is
            lives in the attribution row above (KNOT-022-fix1). */}
        <View style={styles.tagRow}>
          <PillarTag pillar={story.pillar} place={story.place} />
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
          {/* The media is its own tap target: the viewer is opened from here,
              never from the card, whose tap still opens the story (KNOT-020k).
              A feed entry only carries its FIRST item, so the viewer opens on a
              single page; the story detail has the full list. */}
          <Pressable
            style={styles.mediaPress}
            onPress={
              onOpenMedia === undefined ? undefined : () => onOpenMedia(viewerItems, 0, viewerStory)
            }
            disabled={onOpenMedia === undefined}
            accessibilityRole={onOpenMedia === undefined ? undefined : 'imagebutton'}
            accessibilityLabel={
              onOpenMedia === undefined ? undefined : `Open media from ${story.title}`
            }
          >
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
          </Pressable>

          {mediaCount > 1 ? (
            <View
              style={[styles.countBadge, isVideo ? styles.countBadgeOverVideo : null]}
              accessibilityLabel={`${mediaCount} media items`}
            >
              <Images color={colors.text.onMedia} size={COUNT_ICON_SIZE} weight="bold" />
              <Text style={[typeScale.label, tabularNums, mediaOverlayText(colors)]}>
                {mediaCount}
              </Text>
            </View>
          ) : null}
        </View>
      ) : null}

      <View style={styles.padded}>
        {metaLine === '' ? null : (
          <View style={styles.metaRow} testID="feed-meta-row">
            <Text style={[typeScale.label, tabularNums, styles.metaText]}>{metaLine}</Text>
          </View>
        )}

        {showBottomHairline ? (
          <View style={[styles.hairline, metaLine === '' ? styles.hairlineNoMeta : null]} />
        ) : null}
      </View>
    </Pressable>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The author block keeps the row's width; the quick actions sit at its end. They
    // are level with the avatar, not centred on the block, because the block grows as
    // the Rooted line is added (KNOT-031).
    authorLine: {
      flex: 1,
    },
    authorRow: {
      alignItems: 'flex-start',
      flexDirection: 'row',
    },
    // The quick actions share one row so the two 44pt targets sit side by side and
    // never stack, which would push the `✕` down into the pillar tag (KNOT-032).
    cardActions: {
      flexDirection: 'row',
    },
    // One quick action's box: the 44pt minimum touch target around a small glyph.
    cardAction: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
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
    entry: {
      paddingTop: spacing.lg,
    },
    // A card whose first child is the social-proof band opens at the compact step: the
    // band already separates the credit from the card's top edge, so the taller
    // resting inset would double it (KNOT-032-fix2). The inner `padded` wrapper keeps
    // the horizontal inset either way.
    entryWithSocialProof: {
      paddingTop: spacing.sm,
    },
    // The adaptation marker rides beside the adapter's name, so it needs no ink of
    // its own beyond the glyph's tint. Its box exists to carry the accessibility
    // label (KNOT-029).
    adaptBadge: {
      alignItems: 'center',
      justifyContent: 'center',
    },
    // The Feed's card form: without its bottom hairline, the entry needs its own
    // bottom inset so the next card's grey gap does not touch the meta line
    // (KNOT-023b).
    entryCard: {
      paddingBottom: spacing.lg,
    },
    // ...and with a meta row that inset moves INTO the row, so the counts sit level
    // between the media above and the band below instead of hugging the media
    // (KNOT-020k-fix21).
    entryCardWithMeta: {
      paddingBottom: 0,
    },
    // One step darker than the hairline, so a rule between entries is clearly
    // visible rather than dissolving into the canvas (KNOT-031). The meta row already
    // contributes `spacing.md` below its text, so the rule only adds the remainder of
    // the old 16pt gap (KNOT-020k-fix21).
    hairline: {
      backgroundColor: colors.border.divider,
      height: 1,
      marginTop: spacing.xs,
    },
    // An entry with no counts at all draws no meta row, so the rule takes the whole
    // gap itself (KNOT-020k-fix21).
    hairlineNoMeta: {
      marginTop: spacing.lg,
    },
    media: {
      height: '100%',
      width: '100%',
    },
    // The media's tap target fills its frame. It is spelled out rather than using
    // `StyleSheet.absoluteFillObject`, because the component tests call this file
    // with a `StyleSheet` stub that only provides `create` (KNOT-020g-fix7).
    mediaPress: {
      bottom: 0,
      left: 0,
      position: 'absolute',
      right: 0,
      top: 0,
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
    // The stats row carries real counts, so it reads in the secondary ink and the
    // bolder label voice rather than as a caption (KNOT-031). Its vertical padding is
    // symmetric — `spacing.md` above and below the text — and the card adds no bottom
    // inset of its own, so the line sits level between the media and the band
    // (KNOT-020k-fix21).
    metaRow: {
      alignItems: 'center',
      flexDirection: 'row',
      paddingVertical: spacing.md,
    },
    metaText: {
      color: colors.text.secondary,
      flexShrink: 1,
    },
    // The text column keeps the feed inset; only the media leaves it.
    padded: {
      paddingHorizontal: layout.screenPaddingFeed,
    },
    // The opening lines are the story's own voice, so they take the primary ink —
    // pure black in light, pure white in dark — rather than the supporting grey the
    // meta row uses (KNOT-020k-fix19).
    preview: {
      color: colors.text.primary,
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
