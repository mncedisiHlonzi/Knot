import React from 'react';
import { Animated, StyleSheet, View } from 'react-native';

import { useTheme } from '../theme';
import { useCrossfade } from './useCrossfade';

/**
 * The canvas crossfade, drawn once by the root.
 *
 * It sits above every screen and below nothing, covers the window with the mode
 * being left and fades out, and never takes a touch. It renders nothing at all
 * when no transition is in flight, so the app carries no extra layer at rest.
 *
 * It reads the active mode from the theme context rather than taking a prop,
 * because it must be inside `ThemeProvider` to see the change at all.
 */
export default function ThemeCrossfade(): React.ReactElement | null {
  const { theme } = useTheme();
  const { color, opacity } = useCrossfade(theme === 'dark');

  if (color === null) {
    return null;
  }

  return (
    <View pointerEvents="none" style={StyleSheet.absoluteFill}>
      <Animated.View style={[style.fill, { backgroundColor: color, opacity }]} />
    </View>
  );
}

const style = StyleSheet.create({
  fill: {
    bottom: 0,
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
  },
});
