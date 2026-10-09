import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, BackHandler, SafeAreaView, StyleSheet, View } from 'react-native';

import type { AuthResponse, User } from './src/api/client';
import type { Comment } from './src/api/conversations';
import type { Story } from './src/api/stories';
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
import ProfileScreen from './src/screens/profile/ProfileScreen';
import RootedSetupScreen from './src/screens/profile/RootedSetupScreen';
import AdaptStoryScreen from './src/screens/stories/AdaptStoryScreen';
import CreateStoryScreen from './src/screens/stories/CreateStoryScreen';
import FeedScreen from './src/screens/stories/FeedScreen';
import LanguageTreeScreen from './src/screens/stories/LanguageTreeScreen';
import StoryDetailScreen from './src/screens/stories/StoryDetailScreen';

/**
 * A non-tab screen, pushed over the tab bar.
 *
 * The four primary destinations are tabs (see `TabName`); every other screen is
 * an overlay on top of whichever tab is active. Overlays form a stack (see
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
  | { readonly name: 'placeStories'; readonly place: string };

/** The two auth screens, shown before there is a session. Login is the default. */
type AuthMode = 'login' | 'register';

/**
 * The first preferred language tag that is not blank, or "en" when the profile
 * names none. Used to seed the language field when adapting and commenting.
 */
function preferredLanguage(user: User): string {
  return user.preferred_languages.find((tag) => tag.trim() !== '') ?? 'en';
}

/**
 * Root component for the Knot mobile app.
 *
 * The app has three pieces of navigation state: an active tab (Home, Map,
 * Create, Profile), a stack of overlay screens pushed over it, and — before there
 * is a session — which auth screen is showing. The tab bar is rendered only when
 * the overlay stack is empty, so a secondary screen covers the whole surface
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
      case 'profile':
        return (
          <ProfileScreen
            userId={current.user.id}
            token={current.accessToken}
            currentUser={current.user}
            onSetRooted={() => setOverlays((stack) => pushOverlay(stack, { name: 'rootedSetup' }))}
            onUserUpdated={handleUserUpdated}
            onBack={() => setTab('feed')}
          />
        );
      case 'feed':
      default:
        return (
          <FeedScreen
            email={current.user.email}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            onCreateStory={() => setTab('createStory')}
            onSignOut={handleSignOut}
          />
        );
    }
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
        return <LanguageTreeScreen storyId={overlay.storyId} onBack={handleBack} />;
      case 'comments':
        return (
          <CommentThreadScreen
            versionId={overlay.versionId}
            token={current.accessToken}
            language={current.user.preferred_languages.find((tag) => tag.trim() !== '')}
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
