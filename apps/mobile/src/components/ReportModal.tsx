import React, { useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import {
  REPORT_CATEGORIES,
  ReportCategory,
  ReportEntityType,
  describeModerationError,
  reportReasonError,
  reportsApi,
} from '../api/moderation';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../theme';

type ReportModalProps = {
  /** Whether the modal is shown. */
  readonly visible: boolean;
  /** The signed-in user's access token. The reporter is derived from it. */
  readonly token: string;
  /** The kind of content being reported. */
  readonly entityType: ReportEntityType;
  /** The id of the content being reported. */
  readonly entityId: string;
  /** Called when the modal should close, whether or not a report was sent. */
  readonly onClose: () => void;
  /** Called after a report has been submitted successfully. */
  readonly onSubmitted: () => void;
};

/**
 * Reports a piece of content to the moderators.
 *
 * A report names a category and, only for the "other" category, a free-text
 * reason. The reporter is taken from the access token by the server, so there is
 * nothing to spoof: a user cannot report as someone else.
 *
 * Reports are private to the reporter and the moderator queue; nothing about who
 * reported what is shown to the reported user.
 */
export default function ReportModal({
  visible,
  token,
  entityType,
  entityId,
  onClose,
  onSubmitted,
}: ReportModalProps): React.ReactElement {
  const [category, setCategory] = useState<ReportCategory>('harassment');
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  function reset(): void {
    setCategory('harassment');
    setReason('');
    setError(undefined);
    setSubmitting(false);
  }

  function close(): void {
    reset();
    onClose();
  }

  async function handleSubmit(): Promise<void> {
    const problem = reportReasonError(category, reason);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setSubmitting(true);
    setError(undefined);

    try {
      await reportsApi.createReport(
        {
          entity_type: entityType,
          entity_id: entityId,
          category,
          ...(reason.trim() === '' ? {} : { reason: reason.trim() }),
        },
        token,
      );
      reset();
      onSubmitted();
      Alert.alert('Report submitted', 'Thank you — a moderator will review it.');
    } catch (caught) {
      setError(describeModerationError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={close}>
      <View style={styles.backdrop}>
        <View style={styles.card}>
          <Text style={styles.title}>Report content</Text>
          <Text style={styles.subtitle}>
            Tell the moderators what is wrong. Reports are private.
          </Text>

          <ScrollView style={styles.categories} keyboardShouldPersistTaps="handled">
            {REPORT_CATEGORIES.map((option) => (
              <Pressable
                key={option.value}
                style={styles.categoryRow}
                onPress={() => setCategory(option.value)}
                accessibilityRole="radio"
                accessibilityState={{ selected: category === option.value }}
                accessibilityLabel={option.label}
              >
                <View style={[styles.radio, category === option.value ? styles.radioOn : null]}>
                  {category === option.value ? <View style={styles.radioDot} /> : null}
                </View>
                <Text style={styles.categoryLabel}>{option.label}</Text>
              </Pressable>
            ))}
          </ScrollView>

          <Text style={styles.label}>
            Reason {category === 'other' ? '(required)' : '(optional)'}
          </Text>
          <TextInput
            style={[styles.input, styles.multiline]}
            value={reason}
            onChangeText={setReason}
            multiline
            placeholder="Add any detail that would help a moderator"
            placeholderTextColor={colors.text.secondary}
          />

          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

          <View style={styles.actions}>
            <Pressable style={[styles.button, styles.cancel]} onPress={close} disabled={submitting}>
              <Text style={styles.cancelText}>Cancel</Text>
            </Pressable>
            <Pressable
              style={[styles.button, styles.submit, submitting ? styles.disabled : null]}
              onPress={() => {
                void handleSubmit();
              }}
              disabled={submitting}
              accessibilityRole="button"
              accessibilityLabel="Submit report"
            >
              {submitting ? (
                <ActivityIndicator color={colors.text.primary} />
              ) : (
                <Text style={styles.submitText}>Submit</Text>
              )}
            </Pressable>
          </View>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  actions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    marginTop: spacing.lg,
  },
  backdrop: {
    alignItems: 'center',
    backgroundColor: 'rgba(0, 0, 0, 0.6)',
    flex: 1,
    justifyContent: 'center',
    padding: spacing.lg,
  },
  button: {
    alignItems: 'center',
    borderRadius: radius.md,
    justifyContent: 'center',
    minWidth: 96,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  cancel: {
    borderColor: colors.border.default,
    borderWidth: 1,
    marginRight: spacing.sm,
  },
  cancelText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
  },
  card: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.lg,
    borderWidth: 1,
    maxHeight: '85%',
    padding: spacing.lg,
    width: '100%',
  },
  categories: {
    marginTop: spacing.md,
    maxHeight: 220,
  },
  categoryLabel: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    marginLeft: spacing.sm,
  },
  categoryRow: {
    alignItems: 'center',
    flexDirection: 'row',
    paddingVertical: spacing.sm,
  },
  disabled: {
    opacity: 0.6,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginTop: spacing.md,
  },
  input: {
    backgroundColor: colors.bg.secondary,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  label: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
    marginBottom: spacing.xs,
    marginTop: spacing.lg,
  },
  multiline: {
    minHeight: 80,
    textAlignVertical: 'top',
  },
  radio: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: 10,
    borderWidth: 2,
    height: 20,
    justifyContent: 'center',
    width: 20,
  },
  radioDot: {
    backgroundColor: colors.brand.purple,
    borderRadius: 5,
    height: 10,
    width: 10,
  },
  radioOn: {
    borderColor: colors.brand.purple,
  },
  submit: {
    backgroundColor: colors.brand.purple,
  },
  submitText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  subtitle: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
    marginTop: spacing.xs,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});
