import React, { useEffect, useRef } from 'react';
import { Animated, Pressable, StyleSheet, Text } from 'react-native';
import { X } from 'phosphor-react-native';

import { radius, spacing, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';
import { usePressScale } from '../motion/usePressScale';

type ChipProps = {
  /** The chip's text. */
  readonly label: string;
  /** Highlights the chip (brand tint fill, brand text). */
  readonly selected?: boolean;
  /** Called when the chip is tapped. When omitted, the chip is not a control. */
  readonly onPress?: () => void;
  /** Shows a remove affordance. When omitted, no remove button is drawn. */
  readonly onRemove?: () => void;
  /** `default` is a sunken fill; `outline` is a transparent fill with a hairline. */
  readonly variant?: 'default' | 'outline';
  /**
   * The label's type step. Defaults to `caption`, the chip's usual size.
   *
   * A chip that is a primary choice rather than a filter reads one or two steps
   * heavier: the feed's prompt card asks for `button` so its two pillars are
   * legible beside a full-size action (KNOT-020k-fix8). It is a step name, not a
   * style object, so a caller cannot fork the chip's typography.
   */
  readonly textScale?: 'caption' | 'label' | 'button';
  /** An optional leading icon. */
  readonly icon?: React.ReactNode;
  /** Test identifier. */
  readonly testID?: string;
};

/** The chip's height, per the design system. */
const HEIGHT = 32;

/** Expands the 32-point chip to a 44-point touch target without changing its look. */
const VERTICAL_HIT_SLOP = 6;

/** The leading icon's scale at rest and while it springs in when selected. */
const ICON_REST_SCALE = 0.72;

/**
 * The chip's fill is a PLAIN colour, not an animated one (KNOT-020k-fix15).
 *
 * `backgroundColor` cannot run on the native driver, so a tween on it has to be
 * JS-driven — and the press transform on the very same view IS native-driven. Once
 * the press starts, React Native moves that view's node to the native side, and the
 * JS fill animation then throws "Attempting to run JS driven animation on animated
 * node that has been moved to 'native' earlier". Keeping the fill flat is the only
 * fix that leaves the chip's appearance untouched without either a second layer
 * over the hairline border or a JS press scale for every control in the app: the
 * selected surface still changes, it simply changes with the re-render instead of
 * sliding. The press still scales on the native driver, and the icon still springs
 * (KNOT-020h).
 */
const AnimatedChip = Animated.createAnimatedComponent(Pressable);

export default function Chip({
  label,
  selected = false,
  onPress,
  onRemove,
  variant = 'default',
  textScale = 'caption',
  icon,
  testID,
}: ChipProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  // The same press-scale every control uses (KNOT-020k). A chip with no `onPress`
  // is not a control, so it stays still.
  const press = usePressScale(onPress !== undefined);
  const labelType =
    textScale === 'button'
      ? typeScale.button
      : textScale === 'label'
        ? typeScale.label
        : typeScale.caption;
  const textColor = selected
    ? colors.brand.primary
    : variant === 'outline'
      ? colors.text.primary
      : colors.text.secondary;

  // The resting fill and the brand tint, applied instantly. This is a plain style
  // rather than an interpolation of an `Animated.Value`: the same view carries the
  // native press transform, and a JS-driven prop on a native-moved node is exactly
  // the crash KNOT-020k-fix15 removes.
  const fillColor = selected
    ? colors.brand.tint
    : variant === 'outline'
      ? 'transparent'
      : colors.bg.sunken;

  // The icon springs in when the chip becomes selected, which gives the tap a
  // point of arrival beyond the fill alone. It animates its own node's transform,
  // so it never shares a node with the fill.
  const iconScale = useRef(new Animated.Value(selected ? 1 : ICON_REST_SCALE)).current;
  useEffect(() => {
    Animated.spring(iconScale, {
      toValue: selected ? 1 : ICON_REST_SCALE,
      speed: 20,
      bounciness: 9,
      useNativeDriver: true,
    }).start();
  }, [iconScale, selected]);

  const content = (
    <>
      {icon !== undefined ? (
        <Animated.View style={[styles.leadingIcon, { transform: [{ scale: iconScale }] }]}>
          {icon}
        </Animated.View>
      ) : null}
      <Text style={[labelType, { color: textColor }]} numberOfLines={1}>
        {label}
      </Text>
      {onRemove !== undefined ? (
        <Pressable
          onPress={onRemove}
          hitSlop={spacing.sm}
          style={styles.remove}
          accessibilityRole="button"
          accessibilityLabel={`Remove ${label}`}
        >
          <X color={textColor} size={12} weight="bold" />
        </Pressable>
      ) : null}
    </>
  );

  // The fill is a flat colour; the only animated property on this node is the
  // native press transform.
  const chipStyle = [
    styles.chip,
    variant === 'outline' ? styles.outline : null,
    { backgroundColor: fillColor },
    { transform: [{ scale: press.scale }] },
  ];

  if (onPress !== undefined) {
    return (
      <AnimatedChip
        onPress={onPress}
        onPressIn={press.onPressIn}
        onPressOut={press.onPressOut}
        style={chipStyle}
        hitSlop={{ bottom: VERTICAL_HIT_SLOP, top: VERTICAL_HIT_SLOP }}
        accessibilityRole="button"
        accessibilityState={{ selected }}
        accessibilityLabel={label}
        testID={testID}
      >
        {content}
      </AnimatedChip>
    );
  }

  return (
    <Animated.View style={chipStyle} testID={testID}>
      {content}
    </Animated.View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    chip: {
      alignItems: 'center',
      alignSelf: 'flex-start',
      borderRadius: radius.pill,
      flexDirection: 'row',
      gap: spacing.xs,
      height: HEIGHT,
      paddingHorizontal: spacing.md,
    },
    leadingIcon: {
      alignItems: 'center',
      justifyContent: 'center',
    },
    outline: {
      borderColor: colors.border.hairline,
      borderWidth: 1,
    },
    remove: {
      alignItems: 'center',
      justifyContent: 'center',
    },
  });
