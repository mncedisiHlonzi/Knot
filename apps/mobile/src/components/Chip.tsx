import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { X } from 'phosphor-react-native';

import { colors, radius, spacing, typeScale } from '../theme';

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

  const fillStyle = selected
    ? styles.selected
    : variant === 'outline'
      ? styles.outline
      : styles.default;

  const content = (
    <>
      {icon !== undefined ? <View style={styles.leadingIcon}>{icon}</View> : null}
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

  if (onPress !== undefined) {
    return (
      <Pressable
        onPress={onPress}
        style={[styles.chip, fillStyle]}
        hitSlop={{ bottom: VERTICAL_HIT_SLOP, top: VERTICAL_HIT_SLOP }}
        accessibilityRole="button"
        accessibilityState={{ selected }}
        accessibilityLabel={label}
        testID={testID}
      >
        {content}
      </Pressable>
    );
  }

  return (
    <View style={[styles.chip, fillStyle]} testID={testID}>
      {content}
    </View>
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
  default: {
    backgroundColor: colors.bg.sunken,
  },
  leadingIcon: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  outline: {
    backgroundColor: 'transparent',
    borderColor: colors.border.hairline,
    borderWidth: 1,
  },
  remove: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  selected: {
    backgroundColor: colors.brand.tint,
  },
});
