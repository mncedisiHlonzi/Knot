import React from 'react';
import { SafeAreaView, StyleSheet, Text } from 'react-native';

/**
 * Root component for the Knot mobile app.
 *
 * Intentionally minimal (KNOT-001): no navigation, no screens, no domain logic.
 */
export default function App(): React.ReactElement {
  return (
    <SafeAreaView style={styles.container}>
      <Text style={styles.title}>Knot</Text>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    flex: 1,
    justifyContent: 'center',
  },
  title: {
    fontSize: 24,
    fontWeight: '600',
  },
});
