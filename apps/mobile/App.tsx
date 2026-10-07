import React, { useState } from 'react';
import { SafeAreaView, StyleSheet } from 'react-native';

import type { AuthResponse } from './src/api/client';
import type { Story } from './src/api/stories';
import LoginScreen from './src/screens/auth/LoginScreen';
import RegisterScreen from './src/screens/auth/RegisterScreen';
import CreateStoryScreen from './src/screens/stories/CreateStoryScreen';
import FeedScreen from './src/screens/stories/FeedScreen';
import StoryDetailScreen from './src/screens/stories/StoryDetailScreen';

/**
 * Every screen the app can show.
 *
 * Navigation proper (a router, deep links, back gestures) arrives in a later
 * task. Until then a single screen name in state is enough: the app is shallow
 * and every transition is explicit and typed.
 */
type ScreenName = 'register' | 'login' | 'feed' | 'detail' | 'create';

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

  function handleAuthenticated(result: AuthResponse): void {
    setSession(result);
    setScreen('feed');
  }

  function handleSignOut(): void {
    setSession(null);
    setOpenStoryId(null);
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
      return <StoryDetailScreen id={openStoryId} onBack={() => setScreen('feed')} />;
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
    paddingHorizontal: 24,
  },
  storyContainer: {
    flex: 1,
  },
});
