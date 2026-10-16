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
import { colors, radius, spacing, typeScale } from '../theme';

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
          <Text style={[typeScale.h2, styles.title]}>Report content</Text>
          <Text style={[typeScale.bodyS, styles.subtitle]}>
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
                <Text style={[typeScale.body, styles.categoryLabel]}>{option.label}</Text>
              </Pressable>
            ))}
          </ScrollView>

          <Text style={[typeScale.label, styles.label]}>
            Reason {category === 'other' ? '(required)' : '(optional)'}
          </Text>
          <TextInput
            style={[typeScale.body, styles.input, styles.multiline]}
            value={reason}
            onChangeText={setReason}
            multiline
            placeholder="Add any detail that would help a moderator"
            placeholderTextColor={colors.text.secondary}
          />

          {error !== undefined ? (
            <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
          ) : null}

          <View style={styles.actions}>
            <Pressable style={[styles.button, styles.cancel]} onPress={close} disabled={submitting}>
              <Text style={[typeScale.button, styles.cancelText]}>Cancel</Text>
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
                <ActivityIndicator color={colors.text.onBrand} />
              ) : (
                <Text style={[typeScale.button, styles.submitText]}>Submit</Text>
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
  // The modal scrim: the one documented overlay colour, so it is a token rather
  // than a literal (KNOT-020g-fix15).
  backdrop: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
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
    borderColor: colors.border.hairline,
    borderWidth: 1,
    marginRight: spacing.sm,
  },
  cancelText: {
    color: colors.text.primary,
  },
  card: {
    backgroundColor: colors.bg.paper,
    borderColor: colors.border.hairline,
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
    color: colors.state.danger,
    marginTop: spacing.md,
  },
  input: {
    backgroundColor: colors.bg.sunken,
    borderColor: colors.border.hairline,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  label: {
    color: colors.text.secondary,
    marginBottom: spacing.xs,
    marginTop: spacing.lg,
  },
  multiline: {
    minHeight: 80,
    textAlignVertical: 'top',
  },
  radio: {
    alignItems: 'center',
    borderColor: colors.border.hairline,
    borderRadius: 10,
    borderWidth: 2,
    height: 20,
    justifyContent: 'center',
    width: 20,
  },
  radioDot: {
    backgroundColor: colors.brand.primary,
    borderRadius: 5,
    height: 10,
    width: 10,
  },
  radioOn: {
    borderColor: colors.brand.primary,
  },
  submit: {
    backgroundColor: colors.brand.primary,
  },
  // A primary fill takes the on-brand ink, which is the design system's fix for
  // the dark-on-indigo label this used to draw (KNOT-020b).
  submitText: {
    color: colors.text.onBrand,
  },
  subtitle: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
  },
  title: {
    color: colors.text.primary,
  },
});
