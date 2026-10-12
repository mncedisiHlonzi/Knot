import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { colors, radius, spacing, typeScale } from '../theme';
import Button from './Button';

type EmptyStateProps = {
  /** A custom illustration. When omitted, a quiet placeholder is drawn. */
  readonly illustration?: React.ReactNode;
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
 * shape stays the same everywhere. The whole block is centred and its width is
 * bounded by the horizontal padding rather than a fixed size, so it reads well on
 * any device.
 */
export default function EmptyState({
  illustration,
  headline,
  body,
  action,
  testID,
}: EmptyStateProps): React.ReactElement {
  return (
    <View style={styles.container} testID={testID}>
      <View style={styles.illustration}>{illustration ?? <View style={styles.placeholder} />}</View>

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
