import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import Logo from '../../components/Logo';
import { colors, spacing, typeScale } from '../../theme';

/** The logo's height on the splash, per the design brief (KNOT-020c). */
const LOGO_HEIGHT = 44;

/** The tagline shown beneath the logo. */
const TAGLINE = 'Tying human stories, languages, and truths together.';

/**
 * The cold-start splash: the Knot logo and tagline on a white canvas.
 *
 * It carries no controls. It is shown while the app reads the stored session and
 * the first-run flag, and the app root (not this screen) decides when to leave it
 * and where to go — so this component is purely presentational and holds no
 * timer. Motion is deliberately absent; the cross-fade is a later task
 * (KNOT-020e).
 */
export default function SplashScreen(): React.ReactElement {
  return (
    <View style={styles.container}>
      <Logo height={LOGO_HEIGHT} />
      <Text style={[typeScale.reading, styles.tagline]}>{TAGLINE}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    backgroundColor: colors.bg.canvas,
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.xxxl,
  },
  tagline: {
    color: colors.text.secondary,
    marginTop: spacing.lg,
    textAlign: 'center',
  },
});
