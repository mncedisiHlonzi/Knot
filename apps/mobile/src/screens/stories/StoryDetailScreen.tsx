import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  Pressable,
  ScrollView,
  StyleProp,
  StyleSheet,
  Text,
  View,
  ViewStyle,
} from 'react-native';
import { ArrowLeft, CaretDown, DotsThree, GitBranch } from 'phosphor-react-native';
import Video from 'react-native-video';

import { describeError } from '../../api/client';
import { EMPTY_REACTION_COUNTS } from '../../api/reactions';
import { type Story, type StoryMedia, storiesApi } from '../../api/stories';
import { versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import CameraCaptureBadge from '../../components/CameraCaptureBadge';
import Header from '../../components/Header';
import MediaGalleryModal from '../../components/MediaGalleryModal';
import PillarTag from '../../components/PillarTag';
import ReactionBar from '../../components/ReactionBar';
import ReportModal from '../../components/ReportModal';
import { API_BASE_URL } from '../../config/api';
import { languageName } from '../../data/languages';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../../theme';

/** Resolves a relative media path returned by the API against the base URL. */
function resolveMediaUrl(url: string): string {
  return `${API_BASE_URL}${url}`;
}

/** The hero's aspect ratio (4:3), per the design brief. */
const HERO_ASPECT_RATIO = 4 / 3;

/** The floating header's height, used to inset the body when there is no hero. */
const HEADER_HEIGHT = 56;

/** A still image or a paused video with a play control and the capture badge. */
function MediaTile({
  media,
  onOpen,
  style,
}: {
  readonly media: StoryMedia;
  readonly onOpen: () => void;
  readonly style?: StyleProp<ViewStyle>;
}): React.ReactElement {
  const [paused, setPaused] = useState(true);
  const uri = resolveMediaUrl(media.url);

  return (
    <View style={[styles.tile, style]}>
      {/* A full-tile tap target, under the play button so the play button still
          receives its own taps. */}
      <Pressable
        style={styles.tileOpenTarget}
        onPress={onOpen}
        accessibilityRole="button"
        accessibilityLabel="Open media full screen"
      />

      {media.media_type === 'video' ? (
        <Video
          source={{ uri }}
          style={styles.tileMedia}
          paused={paused}
          resizeMode="cover"
          repeat
          muted
        />
      ) : (
        <Image style={styles.tileMedia} source={{ uri }} resizeMode="cover" />
      )}

      {media.media_type === 'video' && paused ? (
        <Pressable
          style={styles.playButton}
          onPress={() => setPaused(false)}
          accessibilityRole="button"
          accessibilityLabel="Play video"
        >
          <Text style={styles.playButtonIcon}>▶</Text>
        </Pressable>
      ) : null}

      {media.source === 'camera' ? <CameraCaptureBadge style={styles.tileBadge} /> : null}
    </View>
  );
}

type StoryDetailScreenProps = {
  /** The id of the story to read. */
  readonly id: string;
  /**
   * The signed-in user's access token, or undefined when signed out. A reaction
   * needs it; without it the bar invites the reader to sign in.
   */
  readonly token?: string;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
  /**
   * Called to adapt this story. The argument is the story's root version id,
   * which is where an adaptation starts.
   */
  readonly onAdapt: (rootVersionId: string) => void;
  /** Called when the person wants to see every version of the story. */
  readonly onViewTree: () => void;
  /**
   * Called to open the conversation on a version. The argument is the version
   * id, which is the story's root version from this screen.
   */
  readonly onConversation: (versionId: string) => void;
  /** Called when the story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * One story in full, as an edge-to-edge reading flow.
 *
 * The layout is the design reference's: a transparent header (back, overflow) over
 * a 4:3 hero, then a reading column (pillar tag, title, author line, the language
 * row that names the root language and its version count, the body, then any
 * extra media), a hairline, the four perspective signals, and the two calls to
 * action — adapt this story, or join its conversation. The report flow sits behind
 * the header's overflow control.
 *
 * A story that does not exist is reported as a normal error message rather than a
 * crash: the server answers 404 with a readable message, and `describeError`
 * surfaces it verbatim.
 */
export default function StoryDetailScreen({
  id,
  token,
  onBack,
  onAdapt,
  onViewTree,
  onConversation,
  onOpenUserProfile,
}: StoryDetailScreenProps): React.ReactElement {
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [versionCount, setVersionCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  // The index of the item shown full screen, or undefined when the viewer is
  // closed.
  const [viewerIndex, setViewerIndex] = useState<number | undefined>(undefined);
  const [reportVisible, setReportVisible] = useState(false);

  const media = story?.media ?? [];
  const hero = media.length > 0 ? media[0] : undefined;

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      // The story carries its root version's content; the tree carries every
      // version of it, so the count comes from the tree.
      const [storyResult, treeResult] = await Promise.all([
        storiesApi.getStory(id, token),
        versionsApi.getTree(id, token),
      ]);
      setStory(storyResult.story);
      setVersionCount(treeResult.versions.length);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [id, token]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <View style={styles.container}>
      <ScrollView contentContainerStyle={styles.content}>
        {hero !== undefined ? (
          <Image
            source={{ uri: resolveMediaUrl(hero.url) }}
            style={styles.hero}
            resizeMode="cover"
            accessibilityIgnoresInvertColors
          />
        ) : (
          // With no media the reading column starts below the floating header.
          <View style={styles.heroSpacer} />
        )}

        <View style={styles.body}>
          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

          {story !== undefined ? (
            <>
              <PillarTag pillar={story.pillar} place={story.approximate_location} />

              <Text style={[typeScale.h1, styles.title]}>{story.title}</Text>

              <View style={styles.authorRow}>
                <AuthorLine
                  displayName={story.author_display_name}
                  avatarUrl={story.author_avatar_url}
                  createdAt={story.created_at}
                  rooted={story.author_rooted}
                  size="medium"
                  onPress={() => onOpenUserProfile(story.author_id)}
                />
              </View>

              <View style={styles.languageRow}>
                <View style={styles.languageLeft}>
                  <GitBranch color={colors.brand.primary} size={20} />
                  <Text style={[typeScale.label, styles.languageName]} numberOfLines={1}>
                    {languageName(story.language)}
                  </Text>
                  <CaretDown color={colors.text.tertiary} size={16} />
                </View>
                <Pressable
                  onPress={onViewTree}
                  hitSlop={spacing.sm}
                  accessibilityRole="button"
                  accessibilityLabel={`View the language tree, ${versionCount} ${
                    versionCount === 1 ? 'version' : 'versions'
                  }`}
                >
                  <Text style={[typeScale.caption, tabularNums, styles.versionCount]}>
                    {versionCount} {versionCount === 1 ? 'version' : 'versions'}
                  </Text>
                </Pressable>
              </View>

              <Text style={[typeScale.reading, styles.storyBody]}>{story.body}</Text>

              {media.length > 1 ? (
                <ScrollView
                  horizontal
                  showsHorizontalScrollIndicator={false}
                  style={styles.carousel}
                  contentContainerStyle={styles.carouselContent}
                >
                  {media.map((item, index) => (
                    <MediaTile
                      key={item.id}
                      media={item}
                      onOpen={() => setViewerIndex(index)}
                      style={styles.carouselItem}
                    />
                  ))}
                </ScrollView>
              ) : null}

              <View style={styles.hairline} />

              <ReactionBar
                entityType="story"
                entityId={story.id}
                counts={story.reactions ?? EMPTY_REACTION_COUNTS}
                myReactions={story.my_reactions ?? []}
                token={token}
                onChange={(counts, mine) =>
                  setStory((previous) =>
                    previous === undefined
                      ? previous
                      : { ...previous, my_reactions: mine, reactions: counts },
                  )
                }
              />

              <View style={styles.actions}>
                <Button
                  variant="primary"
                  fullWidth
                  label="Tell this to your people"
                  onPress={() => onAdapt(story.root_version_id)}
                />
                <View style={styles.actionGap} />
                <Button
                  variant="ghost"
                  fullWidth
                  label={`Join the conversation in ${languageName(story.language)}`}
                  onPress={() => onConversation(story.root_version_id)}
                />
              </View>
            </>
          ) : null}

          {!loading && story === undefined && error !== undefined ? (
            <View style={styles.retry}>
              <Button variant="secondary" label="Try again" onPress={() => void load()} />
            </View>
          ) : null}
        </View>
      </ScrollView>

      <View style={styles.headerOverlay} pointerEvents="box-none">
        <Header
          left={
            <Pressable
              style={styles.iconButton}
              onPress={onBack}
              accessibilityRole="button"
              accessibilityLabel="Back"
            >
              <ArrowLeft color={colors.text.primary} size={24} />
            </Pressable>
          }
          right={
            token !== undefined ? (
              <Pressable
                style={styles.iconButton}
                onPress={() => setReportVisible(true)}
                accessibilityRole="button"
                accessibilityLabel="More options"
              >
                <DotsThree color={colors.text.primary} size={24} weight="bold" />
              </Pressable>
            ) : null
          }
        />
      </View>

      {story !== undefined && token !== undefined ? (
        <ReportModal
          visible={reportVisible}
          token={token}
          entityType="story"
          entityId={story.id}
          onClose={() => setReportVisible(false)}
          onSubmitted={() => setReportVisible(false)}
        />
      ) : null}

      <MediaGalleryModal
        visible={viewerIndex !== undefined}
        media={media}
        initialIndex={viewerIndex ?? 0}
        onClose={() => setViewerIndex(undefined)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  actionGap: {
    height: spacing.sm,
  },
  actions: {
    marginTop: spacing.lg,
  },
  authorRow: {
    marginTop: spacing.md,
  },
  body: {
    paddingHorizontal: layout.screenPaddingReading,
    paddingTop: spacing.lg,
  },
  carousel: {
    marginTop: spacing.lg,
  },
  carouselContent: {
    paddingRight: spacing.md,
  },
  carouselItem: {
    marginRight: spacing.md,
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.xl,
  },
  headerOverlay: {
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
    zIndex: 1,
  },
  hero: {
    aspectRatio: HERO_ASPECT_RATIO,
    backgroundColor: colors.bg.sunken,
    width: '100%',
  },
  heroSpacer: {
    height: HEADER_HEIGHT,
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  languageLeft: {
    alignItems: 'center',
    flexDirection: 'row',
    flexShrink: 1,
    gap: spacing.sm,
  },
  languageName: {
    color: colors.text.primary,
    flexShrink: 1,
  },
  languageRow: {
    alignItems: 'center',
    backgroundColor: colors.bg.paper,
    borderColor: colors.border.hairline,
    borderRadius: radius.lg,
    borderWidth: 1,
    flexDirection: 'row',
    height: 48,
    justifyContent: 'space-between',
    marginTop: spacing.lg,
    paddingHorizontal: spacing.md,
  },
  playButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.primary,
    borderRadius: radius.pill,
    height: 44,
    justifyContent: 'center',
    left: '50%',
    marginLeft: -22,
    marginTop: -22,
    position: 'absolute',
    top: '50%',
    width: 44,
  },
  playButtonIcon: {
    color: colors.text.onBrand,
    marginLeft: 3,
  },
  retry: {
    marginTop: spacing.xl,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  storyBody: {
    color: colors.text.primary,
    marginTop: spacing.lg,
  },
  tile: {
    backgroundColor: colors.bg.paper,
    borderColor: colors.border.hairline,
    borderRadius: radius.lg,
    borderWidth: 1,
    height: 200,
    overflow: 'hidden',
    width: 200,
  },
  tileBadge: {
    left: spacing.xs,
    position: 'absolute',
    top: spacing.xs,
  },
  tileMedia: {
    height: '100%',
    width: '100%',
  },
  tileOpenTarget: {
    ...StyleSheet.absoluteFillObject,
    zIndex: 1,
  },
  title: {
    color: colors.text.primary,
    marginTop: spacing.md,
  },
  versionCount: {
    color: colors.text.tertiary,
  },
});
