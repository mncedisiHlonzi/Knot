import React, { useState } from 'react';
import { Pressable, SafeAreaView, StyleSheet, Text, View } from 'react-native';

import type { AuthResponse } from './src/api/client';
import LoginScreen from './src/screens/auth/LoginScreen';
import RegisterScreen from './src/screens/auth/RegisterScreen';

/** The two auth screens. Navigation proper arrives in a later task. */
type ScreenName = 'register' | 'login';

/**
 * Root component for the Knot mobile app.
 *
 * Auth tokens live in component state only: they are lost when the app restarts.
 * Persistent storage is a later task, so this is deliberately in-memory for now.
 */
export default function App(): React.ReactElement {
  const [screen, setScreen] = useState<ScreenName>('register');
  const [session, setSession] = useState<AuthResponse | null>(null);

  if (session !== null) {
    return (
      <SafeAreaView style={styles.container}>
        <Text style={styles.title}>Knot</Text>
        <Text style={styles.body}>Signed in as {session.user.email}.</Text>
        <Text style={styles.hint}>
          Tokens are kept in memory only and are discarded when the app restarts.
        </Text>
        <View style={styles.actions}>
          <Pressable style={styles.secondaryButton} onPress={() => setSession(null)}>
            <Text style={styles.secondaryButtonText}>Sign out</Text>
          </Pressable>
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.container}>
      {screen === 'register' ? (
        <RegisterScreen onAuthenticated={setSession} onSwitchToLogin={() => setScreen('login')} />
      ) : (
        <LoginScreen
          onAuthenticated={setSession}
          onSwitchToRegister={() => setScreen('register')}
        />
      )}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  actions: {
    flexDirection: 'row',
    marginTop: 24,
  },
  body: {
    color: '#24292f',
    fontSize: 16,
    marginBottom: 8,
    textAlign: 'center',
  },
  container: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: 24,
  },
  hint: {
    color: '#57606a',
    fontSize: 13,
    textAlign: 'center',
  },
  secondaryButton: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    paddingHorizontal: 20,
    paddingVertical: 12,
  },
  secondaryButtonText: {
    color: '#24292f',
    fontSize: 15,
    fontWeight: '600',
  },
  title: {
    color: '#24292f',
    fontSize: 24,
    fontWeight: '700',
    marginBottom: 16,
    textAlign: 'center',
  },
});
