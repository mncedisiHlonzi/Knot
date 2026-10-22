import React, { useCallback, useEffect, useRef } from 'react';
import { Modal, Pressable, StyleSheet, Text, View } from 'react-native';

import { elevation, radius, spacing, typeScale, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';
import Button from './Button';

type ConfirmSheetProps = {
  /** Whether the sheet is shown. */
  readonly visible: boolean;
  /** The question, drawn as the sheet's heading. */
  readonly title: string;
  /**
   * What confirming will actually do, in one or two sentences. The caller owns
   * this wording, because only the caller knows whether the effect is permanent.
   */
  readonly message: string;
  /** The confirming action's label. */
  readonly confirmLabel: string;
  /** The dismissing action's label. It is the safe choice, so it is drawn first. */
  readonly cancelLabel: string;
  /**
   * Called when the confirming action is chosen. At most once per showing, so a
   * double tap cannot apply the caller's effect twice (KNOT-032-fix1).
   */
  readonly onConfirm: () => void;
  /**
   * Called when the person keeps things as they are: the dismissing button, the
   * backdrop, or Android's hardware back button.
   */
  readonly onCancel: () => void;
  /**
   * Draws the confirming action in the danger ink, for a choice that takes
   * something away. The dismissing action is always the outlined one.
   */
  readonly destructive?: boolean;
  /** Test identifier for the sheet surface. */
  readonly testID?: string;
};

/**
 * A bottom-sheet confirmation: a question, what the answer will do, and two
 * choices — keep, or go ahead.
 *
 * It exists because the feed gained quick actions that change what the reader can
 * see, and a one-tap destructive affordance needs a pause with an explicit answer
 * (KNOT-032-fix1). It is the same sheet treatment the profile's options sheet and
 * the feed's post options use — canvas fill, `radius.xxl` top corners, elevation 3
 * — so a confirmation looks like the rest of the app rather than like a platform
 * alert, and it reuses `Button` rather than drawing its own actions.
 *
 * Dismissing is deliberately the frictionless path: the backdrop and the hardware
 * back button both mean "keep things as they are", and neither can confirm.
 */
export default function ConfirmSheet({
  visible,
  title,
  message,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
  destructive = false,
  testID,
}: ConfirmSheetProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  // The confirming action runs at most once per showing. A double tap, or a tap
  // that lands as the sheet slides away, is ignored, so the caller's effect can
  // never be applied twice. The guard resets when the sheet is hidden again, so
  // the next showing starts clean (KNOT-032-fix1).
  const committed = useRef(false);

  useEffect(() => {
    if (!visible) {
      committed.current = false;
    }
  }, [visible]);

  const handleConfirm = useCallback((): void => {
    if (committed.current) {
      return;
    }

    committed.current = true;
    onConfirm();
  }, [onConfirm]);

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onCancel}>
      <Pressable
        style={styles.backdrop}
        onPress={onCancel}
        accessibilityRole="button"
        accessibilityLabel="Dismiss the confirmation"
      >
        {/* The sheet swallows its own taps, so choosing an action never falls
            through to the backdrop and dismisses the sheet first. */}
        <Pressable style={styles.sheet} onPress={() => undefined} testID={testID}>
          <View style={styles.handleArea}>
            <View style={styles.handle} />
          </View>

          <View style={styles.body}>
            <Text style={[typeScale.h3, styles.title]} accessibilityRole="header">
              {title}
            </Text>
            <Text style={[typeScale.bodyS, styles.message]}>{message}</Text>

            <View style={styles.actions}>
              <Button
                variant="secondary"
                fullWidth
                label={cancelLabel}
                onPress={onCancel}
                testID="confirm-cancel"
              />
              <Button
                variant={destructive ? 'danger' : 'primary'}
                fullWidth
                label={confirmLabel}
                onPress={handleConfirm}
                testID="confirm-accept"
              />
            </View>
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    actions: {
      gap: spacing.sm,
      marginTop: spacing.lg,
    },
    backdrop: {
      backgroundColor: colors.scrim.overlay,
      flex: 1,
      justifyContent: 'flex-end',
    },
    body: {
      paddingHorizontal: spacing.lg,
    },
    handle: {
      backgroundColor: colors.border.hairline,
      borderRadius: radius.pill,
      height: 4,
      width: 32,
    },
    handleArea: {
      alignItems: 'center',
      paddingBottom: spacing.sm,
      paddingTop: spacing.sm,
    },
    // The explanation takes the secondary ink and the smaller body size, so the
    // question stays the loudest thing in the sheet.
    message: {
      color: colors.text.secondary,
      marginTop: spacing.sm,
    },
    // The sheet: rounded top corners, the canvas fill, and the floating-bar shadow
    // (elevation 3, whose offset points up) — the profile options sheet's treatment.
    sheet: {
      ...elevation[3],
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      paddingBottom: spacing.xl,
    },
    title: {
      color: colors.text.primary,
    },
  });
