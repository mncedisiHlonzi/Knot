import React, { useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import {
  MAX_PREFERRED_LANGUAGES,
  languageName,
  searchLanguages,
  toggleLanguage,
  type Language,
} from '../data/languages';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

/** How many matches to list at once; the user narrows the list by typing. */
const MAX_RESULTS = 20;

/** Props for picking exactly one language. */
type SingleLanguagePickerProps = {
  readonly mode: 'single';
  /** The chosen code, or '' when nothing is chosen yet. */
  readonly selected: string;
  readonly onSelect: (language: Language) => void;
  /** Clears the choice, returning the field to its empty state. */
  readonly onClear: () => void;
  readonly placeholder?: string;
};

/** Props for picking several languages. */
type MultipleLanguagePickerProps = {
  readonly mode: 'multiple';
  /** The chosen codes, in the order the user chose them. */
  readonly selected: readonly string[];
  readonly onChange: (codes: readonly string[]) => void;
  /** The most codes that may be chosen. Defaults to the server's own cap. */
  readonly max?: number;
  readonly placeholder?: string;
};

export type LanguagePickerProps = SingleLanguagePickerProps | MultipleLanguagePickerProps;

/**
 * A searchable picker over the canonical language list.
 *
 * It is deliberately not a text field for a language name: the value stored is an
 * ISO 639-3 code, and the only codes that exist are the ones the server accepts
 * (KNOT-ADR-046). Searching the list on the device means a typo can never become
 * a stored language, and no request is needed to fill the list.
 *
 * In `single` mode, choosing a language replaces the current one and the chip's
 * remove control clears the field. In `multiple` mode the chips accumulate up to
 * `max`, and tapping a chosen language again removes it.
 */
export default function LanguagePicker(props: LanguagePickerProps): React.ReactElement {
  const [query, setQuery] = useState('');

  // Narrow once, into consts, so the handlers below keep the narrowed type.
  const single = props.mode === 'single' ? props : null;
  const multiple = props.mode === 'multiple' ? props : null;

  const selected: readonly string[] =
    single !== null
      ? single.selected === ''
        ? []
        : [single.selected]
      : (multiple?.selected ?? []);
  const max = multiple !== null ? (multiple.max ?? MAX_PREFERRED_LANGUAGES) : 1;
  const atMax = selected.length >= max;

  const trimmedQuery = query.trim();
  const results = trimmedQuery === '' ? [] : searchLanguages(trimmedQuery).slice(0, MAX_RESULTS);

  function choose(language: Language): void {
    if (single !== null) {
      single.onSelect(language);
      setQuery('');
      return;
    }

    if (multiple === null) {
      return;
    }

    // Tapping a chosen language removes it, so a chip is always the way back out.
    if (multiple.selected.includes(language.code)) {
      multiple.onChange(toggleLanguage(multiple.selected, language.code));
      setQuery('');
      return;
    }

    // At the cap a new language is ignored rather than replacing a choice, and the
    // query is left in place so the hint above it still explains why.
    if (!atMax) {
      multiple.onChange(toggleLanguage(multiple.selected, language.code));
      setQuery('');
    }
  }

  function remove(code: string): void {
    if (single !== null) {
      single.onClear();
      return;
    }

    if (multiple !== null) {
      multiple.onChange(toggleLanguage(multiple.selected, code));
    }
  }

  return (
    <View>
      {selected.length > 0 ? (
        <View style={styles.chips}>
          {selected.map((code) => (
            <View key={code} style={styles.chip}>
              <Text style={styles.chipText}>{languageName(code)}</Text>
              <Pressable
                style={styles.chipRemove}
                onPress={() => remove(code)}
                accessibilityRole="button"
                accessibilityLabel={`Remove ${languageName(code)}`}
                hitSlop={spacing.sm}
              >
                <Text style={styles.chipRemoveText}>×</Text>
              </Pressable>
            </View>
          ))}
        </View>
      ) : null}

      <TextInput
        style={styles.input}
        value={query}
        onChangeText={setQuery}
        placeholder={props.placeholder ?? 'Search for a language'}
        placeholderTextColor={colors.text.secondary}
        autoCorrect={false}
        autoCapitalize="none"
        accessibilityLabel="Search languages"
      />

      {atMax ? (
        <Text style={styles.hint}>
          {`You can choose up to ${max} languages. Tap one to remove it.`}
        </Text>
      ) : null}

      {trimmedQuery !== '' && results.length === 0 ? (
        <Text style={styles.empty}>{`No language matches “${trimmedQuery}”.`}</Text>
      ) : null}

      {results.length > 0 ? (
        <View style={styles.results}>
          {results.map((language) => {
            const chosen = selected.includes(language.code);
            return (
              <Pressable
                key={language.code}
                style={styles.result}
                onPress={() => choose(language)}
                accessibilityRole="button"
                accessibilityState={{ selected: chosen }}
              >
                <Text style={styles.resultName}>{language.name}</Text>
                <Text style={chosen ? styles.resultCodeChosen : styles.resultCode}>
                  {chosen ? `${language.code} ✓` : language.code}
                </Text>
              </Pressable>
            );
          })}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  chip: {
    alignItems: 'center',
    backgroundColor: colors.bg.surface,
    borderColor: colors.brand.purple,
    borderRadius: radius.pill,
    borderWidth: 1,
    flexDirection: 'row',
    marginBottom: spacing.xs,
    marginRight: spacing.xs,
    paddingLeft: spacing.md,
    paddingRight: spacing.sm,
    paddingVertical: spacing.xs,
  },
  chipRemove: {
    paddingHorizontal: spacing.xs,
  },
  chipRemoveText: {
    color: colors.text.secondary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  chipText: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  chips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    marginBottom: spacing.xs,
  },
  empty: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  result: {
    alignItems: 'center',
    borderTopColor: colors.border.subtle,
    borderTopWidth: 1,
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  resultCode: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
  },
  resultCodeChosen: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  resultName: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  results: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.xs,
    overflow: 'hidden',
  },
});
