import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Image,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ArrowLeft, Trash } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { type Story, storiesApi } from '../../api/stories';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import { API_BASE_URL } from '../../config/api';
import { layout, radius, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

/** The thumbnail's edge length, so every bin row is the same shape. */
const THUMBNAIL_SIZE = 64;

/** The glyph drawn when a story has no picture to show. */
const PLACEHOLDER_ICON_SIZE = 22;

/**
 * The still a bin row draws: an image's thumbnail, or a video's poster. A video
 * never has a thumbnail and an image never has a poster, so one of the two names
 * the picture the author actually uploaded (KNOT-054).
 */
function stillPath(story: Story): string | undefined {
  const path = story.first_media_thumbnail_url ?? story.first_media_poster_url ?? undefined;
  return typeof path === 'string' && path.trim() !== '' ? path : undefined;
}

type RecentlyDeletedScreenProps = {
  /** The signed-in author's access token; the bin is always their own. */
  readonly token: string;
  /** Called when the author returns to Settings. */
  readonly onBack: () => void;
};

/**
 * Recently Deleted: the author's recycle bin (KNOT-054).
 *
 * A deleted story is not gone for 30 days. This screen is where that promise is
 * visible: it lists the caller's own soft-deleted stories — the server scopes the
 * read to the token, so there is nothing to filter here — and gives each one two
 * actions. Restore puts it back exactly as it was. Delete forever removes the row,
 * its versions and its media objects, which cannot be undone, so it asks first.
 *
 * Deletion is the only write here, and both actions are optimistic in the safe
 * direction: the row leaves the list only after the server has answered, because a
 * bin that empties itself on a failed request would be the screen claiming
 * something that did not happen.
 */
export default function RecentlyDeletedScreen({
  token,
  onBack,
}: RecentlyDeletedScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [stories, setStories] = useState<readonly Story[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  // The story whose action is in flight, so only its own controls show a spinner.
  const [busyId, setBusyId] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const response = await storiesApi.listDeletedStories(token);
      setStories(response.stories);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    void load();
  }, [load]);

  /** Puts one story back and removes it from the bin list. */
  const restore = useCallback(
    async (story: Story): Promise<void> => {
      setBusyId(story.id);
      try {
        await storiesApi.restoreStory(story.id, token);
        setStories((existing) => existing.filter((item) => item.id !== story.id));
      } catch (caught) {
        Alert.alert('Story not restored', describeError(caught));
      } finally {
        setBusyId(undefined);
      }
    },
    [token],
  );

  /** Removes one story and its media for good. */
  const purge = useCallback(
    async (story: Story): Promise<void> => {
      setBusyId(story.id);
      try {
        await storiesApi.permanentlyDeleteStory(story.id, token);
        setStories((existing) => existing.filter((item) => item.id !== story.id));
      } catch (caught) {
        Alert.alert('Story not deleted', describeError(caught));
      } finally {
        setBusyId(undefined);
      }
    },
    [token],
  );

  /**
   * Asks before a permanent removal.
   *
   * The question is a native alert because this is the one action on the screen
   * that cannot be undone, and it says so in one sentence.
   */
  const requestPurge = useCallback(
    (story: Story): void => {
      Alert.alert(
        'Delete this story forever?',
        'This cannot be undone. The story and everything attached to it will be removed permanently.',
        [
          { text: 'Cancel', style: 'cancel' },
          {
            text: 'Delete forever',
            style: 'destructive',
            onPress: () => {
              void purge(story);
            },
          },
        ],
      );
    },
    [purge],
  );

  return (
    <View style={styles.container}>
      <Header
        title="Recently Deleted"
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
      />

      {loading ? (
        <ActivityIndicator style={styles.spinner} />
      ) : (
        <FlatList
          data={stories}
          keyExtractor={(story) => story.id}
          contentContainerStyle={styles.content}
          ListHeaderComponent={
            <Text style={[typeScale.caption, styles.note]}>
              Stories stay here for 30 days. Restore one to bring it back, or delete it forever.
            </Text>
          }
          ListEmptyComponent={
            error !== undefined ? (
              <Text style={styles.error}>{error}</Text>
            ) : (
              <EmptyState
                illustrationName="no-stories"
                headline="Nothing here."
                body="Stories you delete will wait here for 30 days."
              />
            )
          }
          renderItem={({ item }) => {
            const still = stillPath(item);
            const busy = busyId === item.id;

            return (
              <View style={styles.row} testID={`deleted-${item.id}`}>
                {still === undefined ? (
                  <View style={styles.thumbnailPlaceholder}>
                    <Trash
                      color={colors.text.tertiary}
                      size={PLACEHOLDER_ICON_SIZE}
                      weight="bold"
                    />
                  </View>
                ) : (
                  <Image
                    source={{ uri: `${API_BASE_URL}${still}` }}
                    style={styles.thumbnail}
                    resizeMode="cover"
                    accessibilityIgnoresInvertColors
                  />
                )}

                <View style={styles.info}>
                  <Text style={[typeScale.h3, styles.title]} numberOfLines={2}>
                    {item.title}
                  </Text>

                  <View style={styles.actions}>
                    <Button
                      variant="secondary"
                      label="Restore"
                      loading={busy}
                      onPress={() => {
                        void restore(item);
                      }}
                      accessibilityLabel={`Restore ${item.title}`}
                      testID={`restore-${item.id}`}
                    />
                    <Button
                      variant="danger"
                      label="Delete forever"
                      disabled={busy}
                      onPress={() => requestPurge(item)}
                      accessibilityLabel={`Delete ${item.title} forever`}
                      testID={`purge-${item.id}`}
                    />
                  </View>
                </View>
              </View>
            );
          }}
        />
      )}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    actions: {
      flexDirection: 'row',
      gap: spacing.sm,
      marginTop: spacing.md,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.md,
    },
    // A 44-point tap target for the header's back control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    info: {
      flex: 1,
      marginLeft: spacing.md,
    },
    note: {
      color: colors.text.secondary,
      paddingBottom: spacing.md,
      paddingTop: spacing.sm,
    },
    row: {
      alignItems: 'flex-start',
      borderBottomColor: colors.bg.separator,
      borderBottomWidth: 4,
      flexDirection: 'row',
      paddingVertical: spacing.md,
    },
    spinner: {
      marginTop: spacing.xl,
    },
    thumbnail: {
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.md,
      height: THUMBNAIL_SIZE,
      width: THUMBNAIL_SIZE,
    },
    thumbnailPlaceholder: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.md,
      height: THUMBNAIL_SIZE,
      justifyContent: 'center',
      width: THUMBNAIL_SIZE,
    },
    title: {
      color: colors.text.primary,
    },
  });
