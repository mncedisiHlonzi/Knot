import React, { useState } from 'react';
import { SafeAreaView, StyleSheet, View } from 'react-native';

import type { AuthResponse } from './src/api/client';
import type { Comment } from './src/api/conversations';
import type { Story } from './src/api/stories';
import { spacing } from './src/theme';
import TabBar from './src/components/TabBar';
import type { TabName } from './src/components/TabBar';
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
 * an overlay on top of whichever tab is active. Each overlay carries the ids it
 * needs both to render and to navigate back, so returning from one is a matter of
 * setting the overlay to the previous one, or to null for the tab beneath.
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

/** The two auth screens, shown before there is a session. */
type AuthScreen = 'register' | 'login';

/**
 * Root component for the Knot mobile app.
 *
 * The app has two levels: an active tab (Home, Map, Create, Profile), and an
 * optional overlay screen pushed over it. Splitting the two means the tab bar is
 * only ever shown for a primary destination, and a secondary screen (a story, a
 * place, the composer's follow-ups) replaces the whole surface until dismissed.
 * This is a hand-rolled state machine, not a navigation library; see KNOT-ADR-019.
 *
 * Auth tokens live in component state only: they are lost when the app restarts.
 * Persistent storage is a later task, so this is deliberately in-memory for now.
 */
export default function App(): React.ReactElement {
  const [tab, setTab] = useState<TabName>('feed');
  const [overlay, setOverlay] = useState<Overlay | null>(null);
  const [authScreen, setAuthScreen] = useState<AuthScreen>('register');
  const [session, setSession] = useState<AuthResponse | null>(null);

  function handleAuthenticated(result: AuthResponse): void {
    setSession(result);
    setTab('feed');
    setOverlay(null);
  }

  function handleSignOut(): void {
    setSession(null);
    setOverlay(null);
    setTab('feed');
    setAuthScreen('register');
  }

  function handleStoryCreated(story: Story): void {
    // Show the story the server actually stored, rather than assuming the form's
    // contents are what was persisted.
    setOverlay({ name: 'detail', storyId: story.id });
  }

  /**
   * Renders the active tab's screen.
   *
   * The session is passed in rather than read from state so the type checker can
   * see that it is non-null here.
   */
  function renderTab(current: AuthResponse): React.ReactElement {
    switch (tab) {
      case 'discoveryMap':
        return (
          <DiscoveryMapScreen
            onOpenPlace={(place) => setOverlay({ name: 'placeStories', place })}
          />
        );
      case 'createStory':
        return (
          <CreateStoryScreen
            token={current.access_token}
            onCreated={handleStoryCreated}
            onCancel={() => setTab('feed')}
          />
        );
      case 'profile':
        return (
          <ProfileScreen
            userId={current.user.id}
            token={current.access_token}
            currentUser={current.user}
            onSetRooted={() => setOverlay({ name: 'rootedSetup' })}
            onBack={() => setTab('feed')}
          />
        );
      case 'feed':
      default:
        return (
          <FeedScreen
            email={current.user.email}
            onOpenStory={(id) => setOverlay({ name: 'detail', storyId: id })}
            onCreateStory={() => setTab('createStory')}
            onSignOut={handleSignOut}
          />
        );
    }
  }

  /**
   * Renders the overlay screen over the active tab, or the tab itself when there
   * is no overlay.
   */
  function renderOverlay(current: AuthResponse): React.ReactElement {
    if (overlay === null) {
      return renderTab(current);
    }

    switch (overlay.name) {
      case 'detail':
        return (
          <StoryDetailScreen
            id={overlay.storyId}
            onBack={() => setOverlay(null)}
            onAdapt={(parentVersionId) =>
              setOverlay({ name: 'adapt', storyId: overlay.storyId, parentVersionId })
            }
            onViewTree={() => setOverlay({ name: 'tree', storyId: overlay.storyId })}
            onConversation={(versionId) =>
              setOverlay({ name: 'comments', storyId: overlay.storyId, versionId })
            }
          />
        );
      case 'adapt':
        return (
          <AdaptStoryScreen
            storyId={overlay.storyId}
            parentVersionId={overlay.parentVersionId}
            token={current.access_token}
            defaultLanguage={
              current.user.preferred_languages.find((tag) => tag.trim() !== '') ?? 'en'
            }
            onAdapted={() => setOverlay({ name: 'tree', storyId: overlay.storyId })}
            onCancel={() => setOverlay({ name: 'detail', storyId: overlay.storyId })}
          />
        );
      case 'tree':
        return (
          <LanguageTreeScreen
            storyId={overlay.storyId}
            onBack={() => setOverlay({ name: 'detail', storyId: overlay.storyId })}
          />
        );
      case 'comments':
        return (
          <CommentThreadScreen
            versionId={overlay.versionId}
            token={current.access_token}
            language={current.user.preferred_languages.find((tag) => tag.trim() !== '')}
            onBridge={(comment) =>
              setOverlay({
                name: 'bridge',
                storyId: overlay.storyId,
                versionId: overlay.versionId,
                comment,
              })
            }
            onBack={() => setOverlay({ name: 'detail', storyId: overlay.storyId })}
          />
        );
      case 'bridge':
        return (
          <BridgeScreen
            sourceComment={overlay.comment}
            token={current.access_token}
            preferredLanguages={current.user.preferred_languages}
            onBridged={() =>
              setOverlay({
                name: 'comments',
                storyId: overlay.storyId,
                versionId: overlay.versionId,
              })
            }
            onCancel={() =>
              setOverlay({
                name: 'comments',
                storyId: overlay.storyId,
                versionId: overlay.versionId,
              })
            }
          />
        );
      case 'rootedSetup':
        return (
          <RootedSetupScreen
            token={current.access_token}
            onSaved={() => setOverlay(null)}
            onCancel={() => setOverlay(null)}
          />
        );
      case 'placeStories':
        return (
          <PlaceStoriesScreen
            place={overlay.place}
            onOpenStory={(id) => setOverlay({ name: 'detail', storyId: id })}
            onBack={() => setOverlay(null)}
          />
        );
      default:
        return renderTab(current);
    }
  }

  if (session === null) {
    return (
      <SafeAreaView style={styles.authContainer}>
        {authScreen === 'login' ? (
          <LoginScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToRegister={() => setAuthScreen('register')}
          />
        ) : (
          <RegisterScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToLogin={() => setAuthScreen('login')}
          />
        )}
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.storyContainer}>
      <View style={styles.body}>{renderOverlay(session)}</View>
      {overlay === null ? <TabBar activeTab={tab} onSelect={setTab} /> : null}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  authContainer: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
  },
  body: {
    flex: 1,
  },
  storyContainer: {
    flex: 1,
  },
});
