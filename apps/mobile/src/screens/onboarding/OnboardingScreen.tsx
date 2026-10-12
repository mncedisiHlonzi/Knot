import React, { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import type { SvgProps } from 'react-native-svg';

import EmptyNoInquiries from '../../assets/brand/empty-no-inquiries.svg';
import EmptyNoStories from '../../assets/brand/empty-no-stories.svg';
import OnboardingHero from '../../assets/brand/onboarding-hero.svg';
import Button from '../../components/Button';
import SvgImage from '../../components/SvgImage';
import { colors, radius, spacing, typeScale } from '../../theme';
import {
  ONBOARDING_SLIDES,
  type OnboardingIllustration,
  advance,
  continueLabel,
} from './onboarding';

/**
 * The illustration drawn for each slide. Only `hero` is real brand artwork
 * (`onboarding-hero.svg`); the two placeholders reuse the empty-state
 * illustrations until the real onboarding art is commissioned (KNOT-020c
 * disclosure).
 */
const ILLUSTRATIONS: Readonly<Record<OnboardingIllustration, React.ComponentType<SvgProps>>> = {
  hero: OnboardingHero,
  'placeholder-a': EmptyNoStories,
  'placeholder-b': EmptyNoInquiries,
};

/** The box each illustration is drawn into. */
const ILLUSTRATION_SIZE: Readonly<Record<OnboardingIllustration, { w: number; h: number }>> = {
  hero: { w: 280, h: 220 },
  'placeholder-a': { w: 220, h: 200 },
  'placeholder-b': { w: 220, h: 200 },
};

type OnboardingScreenProps = {
  /** Called when the flow finishes (Skip, or "Get started" on the last slide). */
  readonly onDone: () => void;
};

/**
 * The first-run onboarding: three slides explaining the loop, ending at the auth
 * screens.
 *
 * Each slide draws its illustration, a row of pagination dots, a headline and a
 * body. The footer carries "Skip" on the left and the primary action on the
 * right, which reads "Continue" until the last slide and "Get started" there. The
 * copy and the advance arithmetic live in `onboarding.ts`; this screen is the
 * presentation and the local slide index.
 */
export default function OnboardingScreen({ onDone }: OnboardingScreenProps): React.ReactElement {
  const [index, setIndex] = useState(0);
  const slide = ONBOARDING_SLIDES[index];
  const size = ILLUSTRATION_SIZE[slide.illustration];

  function handlePrimary(): void {
    const next = advance(index);
    if (next.done) {
      onDone();
      return;
    }
    setIndex(next.index);
  }

  return (
    <View style={styles.container}>
      <View style={styles.content}>
        <View style={styles.illustration}>
          <SvgImage
            source={ILLUSTRATIONS[slide.illustration]}
            width={size.w}
            height={size.h}
            decorative
          />
        </View>

        <View style={styles.dots} accessibilityElementsHidden importantForAccessibility="no">
          {ONBOARDING_SLIDES.map((item, dotIndex) => (
            <View
              key={item.headline}
              style={[styles.dot, dotIndex === index ? styles.dotActive : null]}
            />
          ))}
        </View>

        <Text style={[typeScale.h1, styles.headline]}>{slide.headline}</Text>
        <Text style={[typeScale.reading, styles.body]}>{slide.body}</Text>
      </View>

      <View style={styles.footer}>
        <Button variant="ghost" label="Skip" onPress={onDone} />
        <Button variant="primary" label={continueLabel(index)} onPress={handlePrimary} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  body: {
    color: colors.text.secondary,
    marginTop: spacing.md,
    textAlign: 'center',
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    alignItems: 'center',
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
  },
  dot: {
    backgroundColor: colors.bg.sunken,
    borderRadius: radius.pill,
    height: 8,
    width: 8,
  },
  dotActive: {
    backgroundColor: colors.brand.primary,
  },
  dots: {
    flexDirection: 'row',
    gap: spacing.sm,
    justifyContent: 'center',
    marginTop: spacing.xl,
  },
  footer: {
    alignItems: 'center',
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingBottom: spacing.lg,
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.md,
  },
  headline: {
    color: colors.text.primary,
    marginTop: spacing.xl,
    textAlign: 'center',
  },
  illustration: {
    alignItems: 'center',
    justifyContent: 'center',
  },
});
