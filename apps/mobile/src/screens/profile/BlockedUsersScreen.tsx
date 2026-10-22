import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { BlockedUser, blocksApi, describeModerationError } from '../../api/moderation';
import Avatar from '../../components/Avatar';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import { API_BASE_URL } from '../../config/api';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

/** How many blocked users one page asks for. */
const BLOCKED_PAGE_SIZE = 50;

type BlockedUsersScreenProps = {
  /** The signed-in user's access token. The block list is private to them. */
  readonly token: string;
  /** Called when the person returns to the profile. */
  readonly onBack: () => void;
};

/** Resolves a blocked user's relative avatar path against the API base URL. */
function avatarUri(user: BlockedUser): string | undefined {
  const path = typeof user.avatar_url === 'string' ? user.avatar_url.trim() : '';
  return path === '' ? undefined : `${API_BASE_URL}${path}`;
}

/**
 * The people a person has blocked, with a way to unblock each of them.
 *
 * Blocking is mutual hiding plus write prevention (KNOT-ADR-060), and it is
 * private: only the blocker sees this list. It is reached from the profile's
 * options sheet rather than drawn inline, so the wall itself stays about what a
 * person made (KNOT-020g-fix2).
 *
 * Unblocking is immediate and optimistic in the plain sense: the API call removes
 * the block, and the row leaves the list once the server has confirmed it. A
 * failure is reported in place and the row stays, because the block is still in
 * force.
 */
export default function BlockedUsersScreen({
  token,
  onBack,
}: BlockedUsersScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [users, setUsers] = useState<readonly BlockedUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [unblocking, setUnblocking] = useState<string | undefined>(undefined);

  const load = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(undefined);

      try {
        const page = await blocksApi.listBlocks(token, undefined, BLOCKED_PAGE_SIZE);
        setUsers(page.blocks.map((entry) => entry.user));
      } catch (caught) {
        setError(describeModerationError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [token],
  );

  useEffect(() => {
    void load('initial');
  }, [load]);

  const handleUnblock = useCallback(
    async (userId: string): Promise<void> => {
      setUnblocking(userId);
      setError(undefined);

      try {
        await blocksApi.unblock(userId, token);
        setUsers((current) => current.filter((user) => user.id !== userId));
      } catch (caught) {
        setError(describeModerationError(caught));
      } finally {
        setUnblocking(undefined);
      }
    },
    [token],
  );

  return (
    <View style={styles.container}>
      <Header
        title="Blocked users"
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

      <FlatList
        data={users}
        keyExtractor={(user) => user.id}
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              void load('refresh');
            }}
            tintColor={colors.text.secondary}
          />
        }
        ListHeaderComponent={
          <View>
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}
            {error !== undefined ? (
              <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
            ) : null}
          </View>
        }
        renderItem={({ item }) => (
          <View style={styles.row}>
            <Avatar uri={avatarUri(item)} displayName={item.display_name} size="lg" />
            <Text style={[typeScale.h3, styles.name]} numberOfLines={1}>
              {item.display_name}
            </Text>
            <Button
              variant="secondary"
              label="Unblock"
              loading={unblocking === item.id}
              accessibilityLabel={`Unblock ${item.display_name}`}
              onPress={() => {
                void handleUnblock(item.id);
              }}
            />
          </View>
        )}
        ListEmptyComponent={
          !loading && error === undefined ? (
            <EmptyState
              headline="You have not blocked anyone."
              body="When you block someone, they will appear here."
            />
          ) : null
        }
      />
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
      textAlign: 'center',
    },
    // A 44-point tap target for the header's back button.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    name: {
      color: colors.text.primary,
      flex: 1,
    },
    // One blocked person, as the design system's list row: their avatar, their
    // name, and the way to unblock them, closed by a full-bleed hairline. There is
    // no carded box — the whole list is one continuous column, exactly as the
    // feed's entries are.
    row: {
      alignItems: 'center',
      borderBottomColor: colors.border.hairline,
      borderBottomWidth: 1,
      flexDirection: 'row',
      gap: spacing.md,
      marginHorizontal: -layout.screenPaddingFeed,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.lg,
    },
    spinner: {
      marginVertical: spacing.xl,
    },
  });
