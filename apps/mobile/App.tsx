import React, { useState } from 'react';
import { SafeAreaView, StyleSheet } from 'react-native';

import type { AuthResponse } from './src/api/client';
import type { Comment } from './src/api/conversations';
import type { Story } from './src/api/stories';
import { spacing } from './src/theme';
import LoginScreen from './src/screens/auth/LoginScreen';
import RegisterScreen from './src/screens/auth/RegisterScreen';
import BridgeScreen from './src/screens/conversations/BridgeScreen';
import CommentThreadScreen from './src/screens/conversations/CommentThreadScreen';
import AdaptStoryScreen from './src/screens/stories/AdaptStoryScreen';
import CreateStoryScreen from './src/screens/stories/CreateStoryScreen';
import FeedScreen from './src/screens/stories/FeedScreen';
import LanguageTreeScreen from './src/screens/stories/LanguageTreeScreen';
import StoryDetailScreen from './src/screens/stories/StoryDetailScreen';

/**
 * Every screen the app can show.
 *
 * Navigation proper (a router, deep links, back gestures) arrives in a later
 * task. Until then a single screen name in state is enough: the app is shallow
 * and every transition is explicit and typed.
 */
type ScreenName =
  'register' | 'login' | 'feed' | 'detail' | 'create' | 'adapt' | 'tree' | 'comments' | 'bridge';

/**
 * Root component for the Knot mobile app.
 *
 * Auth tokens live in component state only: they are lost when the app restarts.
 * Persistent storage is a later task, so this is deliberately in-memory for now.
 */
export default function App(): React.ReactElement {
  const [screen, setScreen] = useState<ScreenName>('register');
  const [session, setSession] = useState<AuthResponse | null>(null);
  const [openStoryId, setOpenStoryId] = useState<string | null>(null);
  const [adaptParentVersionId, setAdaptParentVersionId] = useState<string | null>(null);
  const [openVersionId, setOpenVersionId] = useState<string | null>(null);
  const [bridgeSourceComment, setBridgeSourceComment] = useState<Comment | null>(null);

  function handleAuthenticated(result: AuthResponse): void {
    setSession(result);
    setScreen('feed');
  }

  function handleSignOut(): void {
    setSession(null);
    setOpenStoryId(null);
    setAdaptParentVersionId(null);
    setOpenVersionId(null);
    setBridgeSourceComment(null);
    setScreen('register');
  }

  function handleOpenStory(id: string): void {
    setOpenStoryId(id);
    setScreen('detail');
  }

  function handleStoryCreated(story: Story): void {
    // Show the story the server actually stored, rather than assuming the form's
    // contents are what was persisted.
    setOpenStoryId(story.id);
    setScreen('detail');
  }

  function handleAdaptStory(rootVersionId: string): void {
    setAdaptParentVersionId(rootVersionId);
    setScreen('adapt');
  }

  function handleConversation(versionId: string): void {
    setOpenVersionId(versionId);
    setScreen('comments');
  }

  function handleBridge(comment: Comment): void {
    setBridgeSourceComment(comment);
    setScreen('bridge');
  }

  /**
   * Renders the current story screen.
   *
   * The session is passed in rather than read from state so the type checker can
   * see that it is non-null here.
   */
  function renderStories(current: AuthResponse): React.ReactElement {
    if (screen === 'create') {
      return (
        <CreateStoryScreen
          token={current.access_token}
          onCreated={handleStoryCreated}
          onCancel={() => setScreen('feed')}
        />
      );
    }

    if (screen === 'detail' && openStoryId !== null) {
      return (
        <StoryDetailScreen
          id={openStoryId}
          onBack={() => setScreen('feed')}
          onAdapt={handleAdaptStory}
          onViewTree={() => setScreen('tree')}
          onConversation={handleConversation}
        />
      );
    }

    if (screen === 'adapt' && openStoryId !== null && adaptParentVersionId !== null) {
      return (
        <AdaptStoryScreen
          storyId={openStoryId}
          parentVersionId={adaptParentVersionId}
          token={current.access_token}
          defaultLanguage={
            current.user.preferred_languages.find((tag) => tag.trim() !== '') ?? 'en'
          }
          onAdapted={() => setScreen('tree')}
          onCancel={() => setScreen('detail')}
        />
      );
    }

    if (screen === 'tree' && openStoryId !== null) {
      return <LanguageTreeScreen storyId={openStoryId} onBack={() => setScreen('detail')} />;
    }

    if (screen === 'comments' && openVersionId !== null) {
      return (
        <CommentThreadScreen
          versionId={openVersionId}
          token={current.access_token}
          language={current.user.preferred_languages.find((tag) => tag.trim() !== '')}
          onBridge={handleBridge}
          onBack={() => setScreen('detail')}
        />
      );
    }

    if (screen === 'bridge' && bridgeSourceComment !== null) {
      return (
        <BridgeScreen
          sourceComment={bridgeSourceComment}
          token={current.access_token}
          preferredLanguages={current.user.preferred_languages}
          onBridged={() => setScreen('comments')}
          onCancel={() => setScreen('comments')}
        />
      );
    }

    return (
      <FeedScreen
        email={current.user.email}
        onOpenStory={handleOpenStory}
        onCreateStory={() => setScreen('create')}
        onSignOut={handleSignOut}
      />
    );
  }

  if (session === null) {
    return (
      <SafeAreaView style={styles.authContainer}>
        {screen === 'login' ? (
          <LoginScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToRegister={() => setScreen('register')}
          />
        ) : (
          <RegisterScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToLogin={() => setScreen('login')}
          />
        )}
      </SafeAreaView>
    );
  }

  return <SafeAreaView style={styles.storyContainer}>{renderStories(session)}</SafeAreaView>;
}

const styles = StyleSheet.create({
  authContainer: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
  },
  storyContainer: {
    flex: 1,
  },
});
