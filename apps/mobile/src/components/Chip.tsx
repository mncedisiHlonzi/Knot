import React, { useEffect, useRef } from 'react';
import { Animated, Easing, Pressable, StyleSheet, Text } from 'react-native';
import { X } from 'phosphor-react-native';

import { colors, motion, radius, spacing, typeScale } from '../theme';

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
 * The chip paints its own fill through an animated value, so a selection is a
 * colour change rather than a swapped style. `backgroundColor` cannot run on the
 * native driver, so this one animation is JS-driven; the icon's spring is native.
 */
const AnimatedChip = Animated.createAnimatedComponent(Pressable);

export default function Chip({
  label,
  selected = false,
  onPress,
  onRemove,
  variant = 'default',
  icon,
  testID,
}: ChipProps): React.ReactElement {
  const textColor = selected
    ? colors.brand.primary
    : variant === 'outline'
      ? colors.text.primary
      : colors.text.secondary;

  // 0 is the resting fill, 1 the brand tint; the chip slides between them so a
  // tap reads as a surface change in motion (KNOT-020h).
  const fill = useRef(new Animated.Value(selected ? 1 : 0)).current;
  useEffect(() => {
    Animated.timing(fill, {
      toValue: selected ? 1 : 0,
      duration: motion.durations.small,
      easing: Easing.bezier(...motion.easing.standard),
      useNativeDriver: false,
    }).start();
  }, [fill, selected]);

  // The icon springs in when the chip becomes selected, which gives the tap a
  // point of arrival beyond the fill alone.
  const iconScale = useRef(new Animated.Value(selected ? 1 : ICON_REST_SCALE)).current;
  useEffect(() => {
    Animated.spring(iconScale, {
      toValue: selected ? 1 : ICON_REST_SCALE,
      speed: 20,
      bounciness: 9,
      useNativeDriver: true,
    }).start();
  }, [iconScale, selected]);

  const fillColor = fill.interpolate({
    inputRange: [0, 1],
    outputRange: [variant === 'outline' ? 'transparent' : colors.bg.sunken, colors.brand.tint],
  });

  const content = (
    <>
      {icon !== undefined ? (
        <Animated.View style={[styles.leadingIcon, { transform: [{ scale: iconScale }] }]}>
          {icon}
        </Animated.View>
      ) : null}
      <Text style={[typeScale.caption, { color: textColor }]} numberOfLines={1}>
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

  // The fill is always the animated one; the outline variant only adds its border.
  const chipStyle = [
    styles.chip,
    variant === 'outline' ? styles.outline : null,
    { backgroundColor: fillColor },
  ];

  if (onPress !== undefined) {
    return (
      <AnimatedChip
        onPress={onPress}
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

const styles = StyleSheet.create({
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
