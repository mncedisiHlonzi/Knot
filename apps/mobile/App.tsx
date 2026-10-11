import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, BackHandler, SafeAreaView, StyleSheet, View } from 'react-native';

import type { AuthResponse, User } from './src/api/client';
import { conversationsApi, type Comment } from './src/api/conversations';
import type { Notification } from './src/api/notifications';
import type { Story } from './src/api/stories';
import { versionsApi } from './src/api/versions';
import { DEFAULT_LANGUAGE_CODE } from './src/config/language';
import { isLanguageCode } from './src/data/languages';
import { colors, spacing } from './src/theme';
import TabBar from './src/components/TabBar';
import type { TabName } from './src/components/TabBar';
import {
  popAllOverlays,
  popOverlay,
  pushOverlay,
  replaceOverlay,
  topOverlay,
} from './src/navigation/overlayStack';
import { clearSession, loadSession, saveSession, Session } from './src/session/session';
import LoginScreen from './src/screens/auth/LoginScreen';
import RegisterScreen from './src/screens/auth/RegisterScreen';
import BridgeScreen from './src/screens/conversations/BridgeScreen';
import CommentThreadScreen from './src/screens/conversations/CommentThreadScreen';
import DiscoveryMapScreen from './src/screens/discovery/DiscoveryMapScreen';
import PlaceStoriesScreen from './src/screens/discovery/PlaceStoriesScreen';
import AskInquiryScreen from './src/screens/inquiries/AskInquiryScreen';
import InquiryDetailScreen from './src/screens/inquiries/InquiryDetailScreen';
import InquiryListScreen from './src/screens/inquiries/InquiryListScreen';
import NotificationsScreen from './src/screens/notifications/NotificationsScreen';
import RootedSetupScreen from './src/screens/profile/RootedSetupScreen';
import UserProfileScreen from './src/screens/profile/UserProfileScreen';
import AdaptStoryScreen from './src/screens/stories/AdaptStoryScreen';
import CreateStoryScreen from './src/screens/stories/CreateStoryScreen';
import FeedScreen from './src/screens/stories/FeedScreen';
import LanguageTreeScreen from './src/screens/stories/LanguageTreeScreen';
import StoryDetailScreen from './src/screens/stories/StoryDetailScreen';

/**
 * A non-tab screen, pushed over the tab bar.
 *
 * The primary destinations are tabs (see `TabName`); every other screen is an
 * overlay on top of whichever tab is active. Overlays form a stack (see
 * `overlayStack.ts`): each carries the ids it needs both to render and to open
 * the next screen, so back is always "pop the stack" rather than a hard-coded
 * target. Each variant's payload is compile-time checked, which a loose
 * `{ name, props }` shape would not give.
 */
type Overlay =
  | { readonly name: 'detail'; readonly storyId: string }
  | { readonly name: 'adapt'; readonly storyId: string; readonly parentVersionId: string }
  | { readonly name: 'tree'; readonly storyId: string }
  | { readonly name: 'comments'; readonly storyId: string; readonly versionId: string }
  | {
      readonly name: 'bridge';
      readonly storyId: string;
      readonly versionId: string;
      readonly comment: Comment;
    }
  | { readonly name: 'rootedSetup' }
  | { readonly name: 'notifications' }
  | { readonly name: 'placeStories'; readonly place: string }
  | { readonly name: 'inquiry'; readonly inquiryId: string }
  | { readonly name: 'askInquiry' }
  | { readonly name: 'userProfile'; readonly userId: string };

/** The two auth screens, shown before there is a session. Login is the default. */
type AuthMode = 'login' | 'register';

/**
 * The first preferred language tag that is a canonical ISO 639-3 code, or
 * undefined when the profile names none the app knows. A legacy or malformed tag
 * is ignored rather than forwarded, because the server rejects it (KNOT-016-fix).
 */
function preferredLanguageTag(user: User): string | undefined {
  return user.preferred_languages.map((tag) => tag.trim()).find((tag) => isLanguageCode(tag));
}

/**
 * The first canonical preferred language, or the app-wide fallback. Used to seed
 * the language field when asking, adapting, and commenting.
 */
function preferredLanguage(user: User): string {
  return preferredLanguageTag(user) ?? DEFAULT_LANGUAGE_CODE;
}

/**
 * Root component for the Knot mobile app.
 *
 * The app has three pieces of navigation state: an active tab (Home, Map, Create,
 * Inquiries, Profile), a stack of overlay screens pushed over it, and — before
 * there is a session — which auth screen is showing. The tab bar is rendered only
 * when the overlay stack is empty, so a secondary screen covers the whole surface
 * until it is popped. This is a hand-rolled state machine, not a navigation
 * library; see KNOT-ADR-019 and KNOT-ADR-025.
 *
 * The session is restored from AsyncStorage on launch and written back on login
 * or register, so it survives a restart; signing out clears it (KNOT-ADR-024).
 */
export default function App(): React.ReactElement {
  const [session, setSession] = useState<Session | null>(null);
  // True until the stored session has been read once, so a returning user does
  // not see the login screen flash before their session is restored.
  const [restoring, setRestoring] = useState(true);
  const [authMode, setAuthMode] = useState<AuthMode>('login');
  const [tab, setTab] = useState<TabName>('feed');
  const [overlays, setOverlays] = useState<readonly Overlay[]>([]);

  // Restore a stored session on first mount. loadSession never throws: a missing
  // or corrupted value resolves to null, which just means "show the login".
  useEffect(() => {
    let active = true;

    void (async () => {
      const stored = await loadSession();
      if (!active) {
        return;
      }
      if (stored !== null) {
        setSession(stored);
      }
      setRestoring(false);
    })();

    return () => {
      active = false;
    };
  }, []);

  // Android hardware back pops the overlay stack and falls through to the OS
  // (background the app) only when there is nothing to pop. BackHandler exists on
  // every platform but only fires on Android, so this is inert on iOS.
  useEffect(() => {
    const subscription = BackHandler.addEventListener('hardwareBackPress', () => {
      if (overlays.length === 0) {
        return false;
      }
      setOverlays((current) => popOverlay(current));
      return true;
    });

    return () => subscription.remove();
  }, [overlays]);

  /** Pops the top overlay — the back action every overlay screen calls. */
  const handleBack = useCallback((): void => {
    setOverlays((current) => popOverlay(current));
  }, []);

  function handleAuthenticated(result: AuthResponse): void {
    const next: Session = {
      accessToken: result.access_token,
      refreshToken: result.refresh_token,
      user: result.user,
    };
    void saveSession(next);
    setSession(next);
    setTab('feed');
    setOverlays((current) => popAllOverlays(current));
  }

  function handleSignOut(): void {
    void clearSession();
    setSession(null);
    setOverlays((current) => popAllOverlays(current));
    setTab('feed');
    setAuthMode('login');
  }

  function handleStoryCreated(story: Story): void {
    // Show the story the server actually stored, rather than assuming the form's
    // contents are what was persisted.
    setOverlays((current) => pushOverlay(current, { name: 'detail', storyId: story.id }));
  }

  /**
   * Replaces the session's user after a profile change (an avatar upload). The
   * updated copy is written back to storage so it survives a restart.
   */
  function handleUserUpdated(user: User): void {
    setSession((current) => {
      if (current === null) {
        return current;
      }
      const next: Session = { ...current, user };
      void saveSession(next);
      return next;
    });
  }

  /**
   * Renders the active tab's screen.
   *
   * The session is passed in rather than read from state so the type checker can
   * see that it is non-null here.
   */
  function renderTab(current: Session): React.ReactElement {
    switch (tab) {
      case 'discoveryMap':
        return (
          <DiscoveryMapScreen
            onOpenPlace={(place) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'placeStories', place }))
            }
          />
        );
      case 'createStory':
        return (
          <CreateStoryScreen
            token={current.accessToken}
            onCreated={handleStoryCreated}
            onCancel={() => setTab('feed')}
          />
        );
      case 'inquiries':
        // The Inquiries tab is a destination, not a pushed screen: it lists the
        // open questions and is where asking starts. Asking and opening a question
        // are overlays, so the tab bar disappears while either is showing
        // (KNOT-ADR-058).
        return (
          <InquiryListScreen
            token={current.accessToken}
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onAsk={() => setOverlays((stack) => pushOverlay(stack, { name: 'askInquiry' }))}
            onOpenUserProfile={openUserProfile}
          />
        );
      case 'profile':
        // The Profile tab is the signed-in user's own wall, the same screen the
        // feed opens when an author is tapped. It is rendered without a back link,
        // because a tab is a destination rather than a pushed screen
        // (KNOT-ADR-044).
        return (
          <UserProfileScreen
            userId={current.user.id}
            token={current.accessToken}
            isOwnProfile
            showBackButton={false}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            onOpenComment={(storyId, versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }))
            }
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onSetRooted={() => setOverlays((stack) => pushOverlay(stack, { name: 'rootedSetup' }))}
            onUserUpdated={handleUserUpdated}
            onSignOut={handleSignOut}
          />
        );
      case 'feed':
      default:
        return (
          <FeedScreen
            email={current.user.email}
            token={current.accessToken}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            onCreateStory={() => setTab('createStory')}
            onOpenNotifications={() =>
              setOverlays((stack) => pushOverlay(stack, { name: 'notifications' }))
            }
            onOpenUserProfile={openUserProfile}
            onSignOut={handleSignOut}
          />
        );
    }
  }

  /**
   * Opens what a notification points at.
   *
   * Each entity type resolves to a screen over one or two public GETs:
   *
   *   version.created  GET /versions/{id}                 -> the story detail
   *   comment.created  GET /comments/{id}                 -> the comment's thread
   *   bridge.created   GET /bridges/{id}, then the source -> the source's thread
   *                    comment with GET /comments/{id}
   *   inquiry.*        the inquiry itself                    -> the inquiry detail
   *
   * `GET /comments/{id}` names both the version and the story, so a resolved
   * comment opens CommentThreadScreen with no further lookup. Both inquiry events
   * point at the inquiry rather than at the answer, so a tap opens the question,
   * where the new answer is visible in its thread (KNOT-ADR-056). The read marker is
   * written by the inbox before this runs, so this method only navigates.
   * Content that can no longer be read (deleted, offline) simply does not open;
   * the inbox stays where it is.
   */
  function handleOpenNotification(notification: Notification): void {
    void (async () => {
      try {
        switch (notification.entity_type) {
          case 'story': {
            // A reaction on a story: the entity id names the story itself.
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'detail', storyId: notification.entity_id }),
            );
            return;
          }
          case 'version': {
            const { version } = await versionsApi.getVersion(notification.entity_id);
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'detail', storyId: version.story_id }),
            );
            return;
          }
          case 'comment': {
            const { comment } = await conversationsApi.getComment(notification.entity_id);
            openCommentThread(comment.version_id, comment.story_id);
            return;
          }
          case 'bridge': {
            const { bridge } = await conversationsApi.getBridge(notification.entity_id);
            const { comment } = await conversationsApi.getComment(bridge.source_comment_id);
            openCommentThread(comment.version_id, comment.story_id);
            return;
          }
          case 'inquiry': {
            // Both inquiry.answered and inquiry.nearby name the inquiry, so a tap
            // opens the question with no conditional on the event type.
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'inquiry', inquiryId: notification.entity_id }),
            );
            return;
          }
          default:
            // A notification the inbox does not produce today has nowhere to go.
            return;
        }
      } catch {
        // A version, comment, bridge, or inquiry that can no longer be read does
        // not open.
      }
    })();
  }

  /** Pushes the comment thread for a resolved version, over the inbox. */
  function openCommentThread(versionId: string, storyId: string): void {
    setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }));
  }

  /**
   * Pushes a user's profile wall over whatever is showing.
   *
   * The same overlay serves the owner and everyone else: the screen shows the
   * owner controls only when the id is the signed-in user's, so tapping your own
   * author line opens your wall with the avatar and sign-out controls.
   */
  function openUserProfile(userId: string): void {
    setOverlays((stack) => pushOverlay(stack, { name: 'userProfile', userId }));
  }

  /**
   * Renders the top overlay over the active tab, or the tab itself when the stack
   * is empty.
   *
   * Every overlay's back action is the same: pop the stack, revealing whatever was
   * underneath — the tab, or the screen that pushed it. Advancing pushes a new
   * overlay; a screen that hands off in place (adapting a story, then its tree)
   * replaces its own entry via `replaceOverlay`.
   */
  function renderOverlay(current: Session): React.ReactElement {
    const overlay = topOverlay(overlays);

    if (overlay === undefined) {
      return renderTab(current);
    }

    switch (overlay.name) {
      case 'detail':
        return (
          <StoryDetailScreen
            id={overlay.storyId}
            token={current.accessToken}
            onBack={handleBack}
            onAdapt={(parentVersionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'adapt', storyId: overlay.storyId, parentVersionId }),
              )
            }
            onViewTree={() =>
              setOverlays((stack) => pushOverlay(stack, { name: 'tree', storyId: overlay.storyId }))
            }
            onConversation={(versionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'comments', storyId: overlay.storyId, versionId }),
              )
            }
            onOpenUserProfile={openUserProfile}
          />
        );
      case 'adapt':
        return (
          <AdaptStoryScreen
            storyId={overlay.storyId}
            parentVersionId={overlay.parentVersionId}
            token={current.accessToken}
            defaultLanguage={preferredLanguage(current.user)}
            onAdapted={() =>
              setOverlays((stack) =>
                replaceOverlay(stack, { name: 'tree', storyId: overlay.storyId }),
              )
            }
            onCancel={handleBack}
          />
        );
      case 'tree':
        return (
          <LanguageTreeScreen
            storyId={overlay.storyId}
            token={current.accessToken}
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'comments':
        return (
          <CommentThreadScreen
            versionId={overlay.versionId}
            token={current.accessToken}
            language={preferredLanguageTag(current.user)}
            onBridge={(comment) =>
              setOverlays((stack) =>
                pushOverlay(stack, {
                  name: 'bridge',
                  storyId: overlay.storyId,
                  versionId: overlay.versionId,
                  comment,
                }),
              )
            }
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'bridge':
        return (
          <BridgeScreen
            sourceComment={overlay.comment}
            token={current.accessToken}
            preferredLanguages={current.user.preferred_languages}
            onBridged={handleBack}
            onOpenUserProfile={openUserProfile}
            onCancel={handleBack}
          />
        );
      case 'rootedSetup':
        return (
          <RootedSetupScreen
            token={current.accessToken}
            onSaved={handleBack}
            onCancel={handleBack}
          />
        );
      case 'placeStories':
        return (
          <PlaceStoriesScreen
            place={overlay.place}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            onBack={handleBack}
          />
        );
      case 'inquiry':
        return (
          <InquiryDetailScreen
            inquiryId={overlay.inquiryId}
            token={current.accessToken}
            preferredLanguages={current.user.preferred_languages}
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'askInquiry':
        return (
          <AskInquiryScreen
            token={current.accessToken}
            defaultLanguage={preferredLanguage(current.user)}
            onAsked={(inquiryId) =>
              setOverlays((stack) => replaceOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onCancel={handleBack}
          />
        );
      case 'notifications':
        return (
          <NotificationsScreen
            token={current.accessToken}
            onOpenEntity={handleOpenNotification}
            onBack={handleBack}
          />
        );
      case 'userProfile':
        return (
          <UserProfileScreen
            userId={overlay.userId}
            token={current.accessToken}
            isOwnProfile={overlay.userId === current.user.id}
            onOpenStory={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId }))
            }
            onOpenComment={(storyId, versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }))
            }
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onUserUpdated={handleUserUpdated}
            onSetRooted={() => setOverlays((stack) => pushOverlay(stack, { name: 'rootedSetup' }))}
            onSignOut={handleSignOut}
            onBack={handleBack}
          />
        );
      default:
        return renderTab(current);
    }
  }

  if (restoring) {
    return (
      <SafeAreaView style={styles.loadingContainer}>
        <ActivityIndicator color={colors.brand.purple} size="large" />
      </SafeAreaView>
    );
  }

  if (session === null) {
    return (
      <SafeAreaView style={styles.authContainer}>
        {authMode === 'login' ? (
          <LoginScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToRegister={() => setAuthMode('register')}
          />
        ) : (
          <RegisterScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToLogin={() => setAuthMode('login')}
          />
        )}
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.storyContainer}>
      <View style={styles.body}>{renderOverlay(session)}</View>
      {overlays.length === 0 ? <TabBar activeTab={tab} onSelect={setTab} /> : null}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  authContainer: {
    backgroundColor: colors.bg.primary,
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
  },
  body: {
    flex: 1,
  },
  loadingContainer: {
    alignItems: 'center',
    backgroundColor: colors.bg.primary,
    flex: 1,
    justifyContent: 'center',
  },
  storyContainer: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
});
