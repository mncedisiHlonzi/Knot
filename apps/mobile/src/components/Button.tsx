import React, { useCallback, useRef } from 'react';
import {
  ActivityIndicator,
  Animated,
  Easing,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { motion, radius, spacing, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';

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
  /**
   * Replaces the default spinner while `loading` — the Thread, for instance
   * (KNOT-020h). Omitted, the button draws its `ActivityIndicator` as before.
   */
  readonly loadingIndicator?: React.ReactNode;
  /** Stretches the button to its container's width. */
  readonly fullWidth?: boolean;
  /** Overrides the accessibility label. Defaults to the visible label. */
  readonly accessibilityLabel?: string;
  /** Test identifier. */
  readonly testID?: string;
};

/** The button's resting height, per the design system. */
const HEIGHT = 48;

/**
 * How far the button shrinks while a finger is down. Small enough to read as
 * contact rather than as a jump: the design direction asks for tactile feedback
 * through press states, not elevation changes.
 */
const PRESS_SCALE = 0.97;

export default function Button({
  label,
  onPress,
  variant = 'primary',
  disabled = false,
  loading = false,
  loadingIndicator,
  fullWidth = false,
  accessibilityLabel,
  testID,
}: ButtonProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const inactive = disabled || loading;

  // The press scale is one native-driver timing per press, so it never touches
  // the JS thread while the finger is down (KNOT-020h).
  const scale = useRef(new Animated.Value(1)).current;
  const animateScale = useCallback(
    (toValue: number): void => {
      Animated.timing(scale, {
        toValue,
        duration: motion.durations.micro,
        easing: Easing.bezier(...motion.easing.standard),
        useNativeDriver: true,
      }).start();
    },
    [scale],
  );

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
    // The wrapper is the flex child now; it carries `fullWidth` so the button
    // still stretches, and the scale transform so a press moves the whole pill.
    <Animated.View style={[fullWidth ? styles.fullWidth : null, { transform: [{ scale }] }]}>
      <Pressable
        onPress={onPress}
        onPressIn={() => animateScale(PRESS_SCALE)}
        onPressOut={() => animateScale(1)}
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
          disabled
            ? variant === 'primary'
              ? styles.primaryDisabled
              : styles.disabledOpacity
            : null,
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
            {loadingIndicator ?? <ActivityIndicator color={labelColor} />}
          </View>
        ) : null}
      </Pressable>
    </Animated.View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
