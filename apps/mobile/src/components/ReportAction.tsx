import React, { useState } from 'react';
import { Pressable, StyleSheet, Text } from 'react-native';

import { ReportEntityType } from '../api/moderation';
import { spacing, typeScale, useThemedStyles, type ThemeColors } from '../theme';
import ReportModal from './ReportModal';

type ReportActionProps = {
  /** The signed-in user's access token. Reporting needs one; without it the
   * action renders nothing, because an anonymous reader cannot report. */
  readonly token?: string;
  /** The kind of content this action reports. */
  readonly entityType: ReportEntityType;
  /** The id of the content this action reports. */
  readonly entityId: string;
  /** Called after a report is submitted, so a parent can react (e.g. refresh). */
  readonly onReported?: () => void;
};

/**
 * The report action for one piece of content: a small text button reading
 * "Report".
 *
 * It used to be a "⋯" menu (KNOT-017a) with Report as its only item, which made a
 * consequential action look like an ambiguous overflow. It is now named on screen
 * and takes the danger ink every other destructive action uses, so a reader knows
 * what will happen before tapping (KNOT-020j-fix2). `hitSlop` keeps the text at
 * the design system's 44pt touch target without making the row taller.
 */
export default function ReportAction({
  token,
  entityType,
  entityId,
  onReported,
}: ReportActionProps): React.ReactElement | null {
  const styles = useThemedStyles(createStyles);
  const [visible, setVisible] = useState(false);

  // Reporting is a signed-in action. A public read with no token shows no
  // trigger at all rather than one that cannot work.
  if (token === undefined) {
    return null;
  }

  return (
    <>
      <Pressable
        style={styles.trigger}
        onPress={() => setVisible(true)}
        accessibilityRole="button"
        accessibilityLabel="Report content"
        hitSlop={spacing.sm}
      >
        <Text style={[typeScale.label, styles.triggerText]}>Report</Text>
      </Pressable>
      <ReportModal
        visible={visible}
        token={token}
        entityType={entityType}
        entityId={entityId}
        onClose={() => setVisible(false)}
        onSubmitted={() => {
          setVisible(false);
          onReported?.();
        }}
      />
    </>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    trigger: {
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: 32,
      paddingHorizontal: spacing.sm,
      paddingVertical: spacing.xs,
    },
    // The destructive-action ink, as every other report/delete control uses.
    triggerText: {
      color: colors.state.danger,
    },
  });
