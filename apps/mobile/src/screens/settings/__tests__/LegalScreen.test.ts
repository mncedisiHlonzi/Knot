import Header from '../../../components/Header';
import LegalScreen from '../LegalScreen';

/**
 * `LegalScreen` is stateful only through `useMemo`, so the suite stubs the hook
 * primitives the project's other screen tests stub and then walks the element tree
 * structurally (KNOT-035).
 *
 * The body is drawn by a file-local `Block` element per parsed block. Rather than
 * expanding those, the assertions read the `block` prop each one was handed, which
 * is exactly what the screen decided to render.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useMemo: (factory: () => unknown) => factory(),
}));

jest.mock('react-native', () => ({
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({ ArrowLeft: 'ArrowLeft' }));

type ElementLike = {
  readonly type?: unknown;
  readonly props?: Record<string, unknown>;
};

function walk(node: unknown, visit: (element: ElementLike) => void): void {
  if (Array.isArray(node)) {
    for (const child of node) {
      walk(child, visit);
    }
    return;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return;
  }

  const element = node as ElementLike;
  if (element.type !== undefined) {
    visit(element);
  }
  walk(element.props?.children, visit);
}

type BlockLike = {
  readonly kind: 'heading' | 'paragraph' | 'bullet';
  readonly level?: 1 | 2;
  readonly text?: string;
  readonly runs?: ReadonlyArray<{ readonly text: string; readonly bold: boolean }>;
};

/** Every block the screen asked its `Block` element to draw. */
function blocks(tree: unknown): readonly BlockLike[] {
  const found: BlockLike[] = [];
  walk(tree, (element) => {
    const type = element.type as { name?: string } | undefined;
    if (typeof type === 'function' && type.name === 'Block') {
      found.push(element.props?.block as BlockLike);
    }
  });
  return found;
}

describe('LegalScreen', () => {
  it('titles itself with the document it was asked for', () => {
    let header: ElementLike | undefined;
    walk(LegalScreen({ documentType: 'privacy', onBack: () => undefined }), (element) => {
      if (element.type === Header) {
        header = element;
      }
    });

    expect(header?.props?.title).toBe('Privacy Policy');

    let termsHeader: ElementLike | undefined;
    walk(LegalScreen({ documentType: 'terms', onBack: () => undefined }), (element) => {
      if (element.type === Header) {
        termsHeader = element;
      }
    });

    expect(termsHeader?.props?.title).toBe('Terms of Service');
  });

  it('renders the Privacy Policy body without its markdown syntax', () => {
    const drawn = blocks(LegalScreen({ documentType: 'privacy', onBack: () => undefined }));
    const headings = drawn.filter((block) => block.kind === 'heading').map((block) => block.text);

    // The document's own title is not drawn: the header carries it.
    expect(headings).toContain('1. What We Collect');
    expect(headings).not.toContain('Knot — Privacy Policy');

    const text = drawn
      .flatMap((block) =>
        block.kind === 'heading' ? [block.text ?? ''] : (block.runs ?? []).map((run) => run.text),
      )
      .join('\n');
    expect(text).not.toContain('**');
    expect(text).toContain('Argon2id');
  });

  it('renders the Terms of Service body, bullets included', () => {
    const drawn = blocks(LegalScreen({ documentType: 'terms', onBack: () => undefined }));

    expect(drawn.some((block) => block.kind === 'heading' && block.text === '9. Contact')).toBe(
      true,
    );
    expect(drawn.some((block) => block.kind === 'bullet')).toBe(true);
  });

  it('offers a way back', () => {
    const onBack = jest.fn();

    let header: ElementLike | undefined;
    walk(LegalScreen({ documentType: 'privacy', onBack }), (element) => {
      if (element.type === Header) {
        header = element;
      }
    });

    const left = header?.props?.left as ElementLike | undefined;
    expect(left?.props?.onPress).toBe(onBack);
  });
});
