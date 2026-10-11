import React, { useState } from 'react';
import { Pressable, StyleSheet, Text } from 'react-native';

import { ReportEntityType } from '../api/moderation';
import { colors, fontSizes, spacing } from '../theme';
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
 * A compact overflow control that opens the report flow for one piece of content.
 *
 * It is the "⋯" menu the action rows carry (KNOT-017a). One item today — Report —
 * with Block joining it on a user profile. The menu is a single tap for now
 * because there is a single item; a full menu is deferred until a second action
 * needs it.
 */
export default function ReportAction({
  token,
  entityType,
  entityId,
  onReported,
}: ReportActionProps): React.ReactElement | null {
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
        <Text style={styles.triggerText}>⋯</Text>
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

const styles = StyleSheet.create({
  trigger: {
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  triggerText: {
    color: colors.text.secondary,
    fontSize: fontSizes.lg,
    fontWeight: '700',
  },
});
