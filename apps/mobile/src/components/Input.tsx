import React, { useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';

import { colors, spacing, typeScale } from '../theme';

type InputProps = {
  /** A label drawn above the field, upper-cased by the overline style. */
  readonly label?: string;
  /** The field's value. */
  readonly value: string;
  /** Called on every keystroke. */
  readonly onChangeText: (text: string) => void;
  /** Placeholder text, shown in the disabled ink. */
  readonly placeholder?: string;
  /** Grows the field for long-form text (uses the reading type style). */
  readonly multiline?: boolean;
  /** The keyboard to raise. Defaults to the platform default. */
  readonly keyboardType?: 'default' | 'email-address' | 'numeric' | 'phone-pad';
  /** Masks the value, for a password. */
  readonly secureTextEntry?: boolean;
  /** Focuses the field on mount. */
  readonly autoFocus?: boolean;
  /** When false, the field is read-only. Defaults to true. */
  readonly editable?: boolean;
  /** How the keyboard capitalises what is typed. Defaults to `sentences`. */
  readonly autoCapitalize?: 'none' | 'sentences' | 'words' | 'characters';
  /** Whether the keyboard autocorrects. Defaults to true. */
  readonly autoCorrect?: boolean;
  /** The label on the keyboard's return key. Defaults to the platform's own. */
  readonly returnKeyType?: 'default' | 'done' | 'go' | 'next' | 'search' | 'send';
  /**
   * Overrides the field's accessibility label. When omitted the label falls back
   * to the `label` prop and then the placeholder, as it always has.
   */
  readonly accessibilityLabel?: string;
  /**
   * A node drawn inside the field's leading edge — a magnifier glyph on a search
   * field, for instance. When present it offsets the text area to its right, and
   * the field's underline spans the icon and the text together.
   */
  readonly leadingIcon?: React.ReactNode;
  /** An error message. Its presence turns the underline red and shows the text. */
  readonly error?: string;
  /** Helper text, shown below the field only when there is no error. */
  readonly helper?: string;
  /** Test identifier. */
  readonly testID?: string;
};

/** The three underline states a field can be in. */
export type InputUnderline = 'error' | 'focus' | 'rest';

/**
 * Resolves the field's underline state from its focus and error state.
 *
 * Error outranks focus: a field with a problem stays red even while it is being
 * edited, so the message and the field never disagree. It is exported so the
 * precedence can be tested without rendering.
 */
export function resolveUnderline(focused: boolean, hasError: boolean): InputUnderline {
  if (hasError) {
    return 'error';
  }
  return focused ? 'focus' : 'rest';
}

/**
 * An underline text field, the one text input of the design system.
 *
 * It has no filled background: the field is delineated by a single bottom
 * hairline, which thickens to the brand colour on focus and turns to the danger
 * colour on error — so the field's state reads without a border box. The label and
 * helper text carry the quieter inks; the placeholder uses the disabled ink.
 */
export default function Input({
  label,
  value,
  onChangeText,
  placeholder,
  multiline = false,
  keyboardType,
  secureTextEntry,
  autoFocus,
  editable = true,
  autoCapitalize = 'sentences',
  autoCorrect = true,
  returnKeyType,
  accessibilityLabel,
  leadingIcon,
  error,
  helper,
  testID,
}: InputProps): React.ReactElement {
  const [focused, setFocused] = useState(false);

  const message = typeof error === 'string' && error !== '' ? error : undefined;
  const hasError = message !== undefined;

  const underlineState = resolveUnderline(focused, hasError);
  const underlineColor =
    underlineState === 'error'
      ? colors.state.danger
      : underlineState === 'focus'
        ? colors.brand.primary
        : colors.border.hairline;
  const underlineWidth = focused ? 2 : 1;

  const underline = { borderBottomColor: underlineColor, borderBottomWidth: underlineWidth };
  const fieldStyle = multiline
    ? [typeScale.reading, styles.field, styles.multiline]
    : [typeScale.body, styles.field];

  // With a leading icon the underline moves onto the row that holds the icon and
  // the text together, so a single hairline runs under both; without one it stays
  // on the field itself, exactly as before (KNOT-020g).
  const hasLeadingIcon = leadingIcon !== undefined;

  const field = (
    <TextInput
      style={hasLeadingIcon ? [fieldStyle, styles.fieldInline] : [fieldStyle, underline]}
      value={value}
      onChangeText={onChangeText}
      placeholder={placeholder}
      placeholderTextColor={colors.text.disabled}
      multiline={multiline}
      keyboardType={keyboardType}
      secureTextEntry={secureTextEntry}
      autoFocus={autoFocus}
      editable={editable}
      autoCapitalize={autoCapitalize}
      autoCorrect={autoCorrect}
      returnKeyType={returnKeyType}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      accessibilityLabel={accessibilityLabel ?? label ?? placeholder}
      accessibilityState={{ disabled: !editable }}
      testID={testID}
    />
  );

  return (
    <View style={styles.container}>
      {label !== undefined ? <Text style={[typeScale.overline, styles.label]}>{label}</Text> : null}

      {hasLeadingIcon ? (
        <View style={[styles.fieldRow, underline]}>
          <View style={styles.leadingIcon}>{leadingIcon}</View>
          {field}
        </View>
      ) : (
        field
      )}

      {message !== undefined ? (
        <Text style={[typeScale.caption, styles.error]}>{message}</Text>
      ) : helper !== undefined ? (
        <Text style={[typeScale.caption, styles.helper]}>{helper}</Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    alignSelf: 'stretch',
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.xs,
  },
  field: {
    color: colors.text.primary,
    paddingVertical: spacing.sm,
  },
  // The text takes the row's width once a leading icon has taken its own.
  fieldInline: {
    flex: 1,
  },
  // The row that holds a leading icon and the field, sharing one underline.
  fieldRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  helper: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  label: {
    color: colors.text.secondary,
    marginBottom: spacing.xs,
  },
  leadingIcon: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  multiline: {
    minHeight: 96,
    textAlignVertical: 'top',
  },
});
