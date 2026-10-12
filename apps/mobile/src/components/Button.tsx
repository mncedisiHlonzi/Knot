import React from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import { colors, radius, spacing, typeScale } from '../theme';

/** The four button presentations the design system offers. */
export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';

type ButtonProps = {
  /** The button's text. */
  readonly label: string;
  /** Called when the button is tapped. */
  readonly onPress: () => void;
  /**
   * The presentation. `primary` is the one filled action on a screen; `secondary`
   * is outlined; `ghost` is text-only; `danger` is destructive, text-only in the
   * danger colour. Defaults to `primary`.
   */
  readonly variant?: ButtonVariant;
  /** Greys the button out and blocks presses. */
  readonly disabled?: boolean;
  /** Swaps the label for a spinner while an action is in flight. */
  readonly loading?: boolean;
  /** Stretches the button to its container's width. */
  readonly fullWidth?: boolean;
  /** Overrides the accessibility label. Defaults to the visible label. */
  readonly accessibilityLabel?: string;
  /** Test identifier. */
  readonly testID?: string;
};

/** The button's resting height, per the design system. */
const HEIGHT = 48;

export default function Button({
  label,
  onPress,
  variant = 'primary',
  disabled = false,
  loading = false,
  fullWidth = false,
  accessibilityLabel,
  testID,
}: ButtonProps): React.ReactElement {
  const inactive = disabled || loading;

  // The label (and the spinner) take the variant's colour; a disabled button uses
  // the disabled ink so it reads as unavailable even before the opacity is applied.
  const labelColor = disabled
    ? colors.text.disabled
    : variant === 'primary'
      ? colors.text.onBrand
      : variant === 'secondary'
        ? colors.text.primary
        : variant === 'ghost'
          ? colors.brand.primary
          : colors.state.danger;

  const variantStyle =
    variant === 'primary'
      ? styles.primary
      : variant === 'secondary'
        ? styles.secondary
        : styles.plain;

  return (
    <Pressable
      onPress={onPress}
      disabled={inactive}
      style={({ pressed }) => [
        styles.base,
        variantStyle,
        fullWidth ? styles.fullWidth : null,
        // Primary darkens to the pressed indigo; the others dim, because a
        // transparent fill has no darker shade to move to.
        pressed && !inactive
          ? variant === 'primary'
            ? styles.primaryPressed
            : styles.pressedOpacity
          : null,
        disabled ? (variant === 'primary' ? styles.primaryDisabled : styles.disabledOpacity) : null,
      ]}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityState={{ disabled: inactive, busy: loading }}
      testID={testID}
    >
      <Text
        style={[
          typeScale.button,
          styles.label,
          { color: labelColor },
          loading && styles.labelHidden,
        ]}
        numberOfLines={1}
      >
        {label}
      </Text>
      {loading ? (
        <View pointerEvents="none" style={styles.spinner}>
          <ActivityIndicator color={labelColor} />
        </View>
      ) : null}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: {
    alignItems: 'center',
    borderRadius: radius.md,
    flexDirection: 'row',
    height: HEIGHT,
    justifyContent: 'center',
    paddingHorizontal: spacing.lg,
  },
  disabledOpacity: {
    opacity: 0.5,
  },
  fullWidth: {
    alignSelf: 'stretch',
  },
  label: {
    textAlign: 'center',
  },
  // The label keeps its place in the layout while loading, so the button's width
  // does not jump when the spinner appears.
  labelHidden: {
    opacity: 0,
  },
  plain: {
    backgroundColor: 'transparent',
  },
  pressedOpacity: {
    opacity: 0.7,
  },
  primary: {
    backgroundColor: colors.brand.primary,
  },
  primaryDisabled: {
    backgroundColor: colors.bg.sunken,
    opacity: 0.5,
  },
  primaryPressed: {
    backgroundColor: colors.brand.pressed,
  },
  secondary: {
    backgroundColor: 'transparent',
    borderColor: colors.border.hairline,
    borderWidth: 1,
  },
  spinner: {
    alignItems: 'center',
    bottom: 0,
    justifyContent: 'center',
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
  },
});
