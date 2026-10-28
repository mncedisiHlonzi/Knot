import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import Logo from '../../components/Logo';
import TheThread from '../../components/TheThread';
import { spacing, typeScale, useTheme, useThemedStyles, type ThemeColors } from '../../theme';

/** The logo's height on the splash, per the design brief (KNOT-020c). */
const LOGO_HEIGHT = 44;

/** The tagline shown beneath the logo. */
const TAGLINE = 'Tying human stories, languages, and truths together.';

/** The Thread's length on the splash, so it reads as an underline for the mark. */
const THREAD_LENGTH = 120;

/**
 * The cold-start splash: the Knot logo and tagline on a white canvas.
 *
 * It carries no controls. It is shown while the app reads the stored session and
 * the first-run flag, and the app root (not this screen) decides when to leave it
 * and where to go — so this component is purely presentational and holds no
 * timer. The Thread draws itself beneath the logo while that read is in flight
 * (KNOT-020h); there is no spinner, and no other motion on the screen.
 */
export default function SplashScreen(): React.ReactElement {
  const { theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  // The logo is tinted to the mode: pure black on the light canvas, pure white on
  // the dark one (KNOT-020k). The PRIMARY logo is a gradient and cannot be
  // tinted, so the monochrome assets carry this screen.
  const logoVariant = theme === 'dark' ? 'mono-light' : 'mono-dark';

  return (
    <View style={styles.container}>
      <Logo height={LOGO_HEIGHT} variant={logoVariant} />
      <View style={styles.thread}>
        <TheThread length={THREAD_LENGTH} testID="splash-thread" />
      </View>
      <Text style={[typeScale.reading, styles.tagline]}>{TAGLINE}</Text>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
    // The line sits between the mark and the tagline, close enough to read as
    // drawing the logo's underline.
    thread: {
      marginTop: spacing.lg,
    },
  });
