import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import type { SvgProps } from 'react-native-svg';

import NoInquiries from '../assets/brand/empty-no-inquiries.svg';
import NoNotifications from '../assets/brand/empty-no-notifications.svg';
import NoResults from '../assets/brand/empty-no-results.svg';
import NoStories from '../assets/brand/empty-no-stories.svg';
import { colors, radius, spacing, typeScale } from '../theme';
import Button from './Button';
import SvgImage from './SvgImage';

/** The named empty-state illustrations the brand ships (KNOT-020b-fix). */
export type EmptyIllustrationName =
  'no-stories' | 'no-results' | 'no-notifications' | 'no-inquiries';

/** The brand illustration each name selects. */
const ILLUSTRATIONS: Readonly<Record<EmptyIllustrationName, React.ComponentType<SvgProps>>> = {
  'no-stories': NoStories,
  'no-results': NoResults,
  'no-notifications': NoNotifications,
  'no-inquiries': NoInquiries,
};

type EmptyStateProps = {
  /** A custom illustration. Takes precedence over `illustrationName` when both are given. */
  readonly illustration?: React.ReactNode;
  /**
   * A named brand illustration, drawn when no `illustration` node is given. It is
   * an addition: an existing caller that passes `illustration` (or neither) is
   * unaffected.
   */
  readonly illustrationName?: EmptyIllustrationName;
  /** The headline. */
  readonly headline: string;
  /** Optional supporting copy. */
  readonly body?: string;
  /** An optional call to action, rendered as a primary button. */
  readonly action?: { readonly label: string; readonly onPress: () => void };
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The centred empty state: an illustration, a headline, optional body copy and an
 * optional action.
 *
 * It is the one place a screen explains that there is nothing to show, so the
 * shape stays the same everywhere. The artwork is either a caller's node, one of
 * the brand's named illustrations, or a quiet placeholder. The whole block is
 * centred and its width is bounded by the horizontal padding rather than a fixed
 * size, so it reads well on any device.
 */
export default function EmptyState({
  illustration,
  illustrationName,
  headline,
  body,
  action,
  testID,
}: EmptyStateProps): React.ReactElement {
  const artwork =
    illustration ??
    (illustrationName !== undefined ? (
      <SvgImage source={ILLUSTRATIONS[illustrationName]} decorative />
    ) : (
      <View style={styles.placeholder} />
    ));

  return (
    <View style={styles.container} testID={testID}>
      <View style={styles.illustration}>{artwork}</View>

      <Text style={[typeScale.h3, styles.headline]}>{headline}</Text>
      {body !== undefined ? <Text style={[typeScale.bodyS, styles.body]}>{body}</Text> : null}

      {action !== undefined ? (
        <View style={styles.action}>
          <Button label={action.label} onPress={action.onPress} />
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  action: {
    marginTop: spacing.xl,
  },
  body: {
    color: colors.text.secondary,
    marginTop: spacing.sm,
    textAlign: 'center',
  },
  container: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    paddingVertical: spacing.xxl,
  },
  headline: {
    color: colors.text.primary,
    textAlign: 'center',
  },
  illustration: {
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: spacing.lg,
  },
  placeholder: {
    backgroundColor: colors.bg.sunken,
    borderRadius: radius.pill,
    height: 72,
    width: 72,
  },
});
