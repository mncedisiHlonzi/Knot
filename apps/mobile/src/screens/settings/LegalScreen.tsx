import React, { useMemo } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View, type TextStyle } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import Header from '../../components/Header';
import {
  legalDocumentText,
  legalDocumentTitle,
  type LegalDocument,
} from '../../data/legalDocuments';
import {
  fontWeights,
  layout,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
  type ThemeColors,
} from '../../theme';
import { legalDocumentBlocks, type LegalBlock, type LegalRun } from './legalMarkdown';

type LegalScreenProps = {
  /** Which approved document to show. */
  readonly documentType: LegalDocument;
  /** Called when the person leaves the document. */
  readonly onBack: () => void;
};

/**
 * Draws one legal document: the Terms of Service or the Privacy Policy
 * (KNOT-035).
 *
 * It is reached from Settings and from the consent row on the Register form, and
 * it is the same screen in both places, so what a person consents to is exactly
 * what they can read afterwards.
 *
 * The text is the founder-approved wording, bundled with the build, and is
 * rendered by the small parser in `legalMarkdown.ts` rather than by a markdown
 * library: the app adds no dependency for four constructs, and every style here
 * comes from the theme tokens. The leading `# Knot — …` title is dropped because
 * the header already names the document.
 */
export default function LegalScreen({
  documentType,
  onBack,
}: LegalScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);

  const blocks = useMemo(
    () => legalDocumentBlocks(legalDocumentText(documentType)),
    [documentType],
  );

  return (
    <View style={styles.container}>
      <Header
        title={legalDocumentTitle(documentType)}
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Back"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      <ScrollView contentContainerStyle={styles.content}>
        {blocks.map((block, index) => (
          <Block key={blockKey(block, index)} block={block} styles={styles} />
        ))}
      </ScrollView>
    </View>
  );
}

/**
 * A stable identity for a block.
 *
 * The position and the opening words together survive nothing here — the
 * documents are static — but they keep two identical short blocks (two "You can…"
 * bullets) from colliding while staying readable in a React tree.
 */
function blockKey(block: LegalBlock, index: number): string {
  const opening = block.kind === 'heading' ? block.text : (block.runs[0]?.text ?? '');
  return `${index}-${opening.slice(0, 24)}`;
}

/**
 * Renders one block.
 *
 * It is a plain function returning an element rather than a component: the screen
 * has no per-block state, and the project's tests call screens directly, so a
 * component here would only add an indirection to expand.
 */
function Block({
  block,
  styles,
}: {
  readonly block: LegalBlock;
  readonly styles: ReturnType<typeof createStyles>;
}): React.ReactElement {
  if (block.kind === 'heading') {
    return (
      <Text style={[block.level === 1 ? typeScale.h2 : typeScale.h3, styles.heading]}>
        {block.text}
      </Text>
    );
  }

  if (block.kind === 'bullet') {
    return (
      <View style={styles.bulletRow}>
        <Text style={[typeScale.reading, styles.bulletMarker]}>•</Text>
        <Text style={[typeScale.reading, styles.body]}>{runs(block.runs, styles.bold)}</Text>
      </View>
    );
  }

  return <Text style={[typeScale.reading, styles.body]}>{runs(block.runs, styles.bold)}</Text>;
}

/** Turns inline runs into nested `Text`, so bold is a span inside the paragraph. */
function runs(items: readonly LegalRun[], boldStyle: TextStyle): readonly React.ReactNode[] {
  return items.map((run, index) =>
    run.bold ? (
      <Text key={`run-${index}`} style={boldStyle}>
        {run.text}
      </Text>
    ) : (
      <Text key={`run-${index}`}>{run.text}</Text>
    ),
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    body: {
      color: colors.text.primary,
      flexShrink: 1,
    },
    bold: {
      fontWeight: fontWeights.bold,
    },
    // The marker hangs in the gutter beside the bullet's text, so a wrapped line
    // stays aligned with the first one.
    bulletMarker: {
      color: colors.text.tertiary,
    },
    bulletRow: {
      flexDirection: 'row',
      gap: spacing.sm,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    // The reading column: one gutter, one rhythm, and no cards — a legal document
    // is a continuous text, not a list of sections to browse.
    content: {
      gap: spacing.md,
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.lg,
    },
    heading: {
      color: colors.text.primary,
      marginTop: spacing.md,
    },
    // A 44-point tap target for the header's back control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
  });
