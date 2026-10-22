import ReportAction from '../ReportAction';
import { colors } from '../../theme';

// ReportAction is stateful (it owns the modal's visibility), so this suite uses
// the project's renderer-less pattern: the hook primitives are stubbed and the
// component is called as a plain function (KNOT-020j-fix2).
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useState: (initial: unknown) => [initial, () => undefined],
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Alert: { alert: jest.fn() },
  Modal: 'Modal',
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  TextInput: 'TextInput',
  View: 'View',
}));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Collects every element of a given type in a directly-called tree. */
function elementsOfType(node: unknown, type: unknown, found: ElementLike[] = []): ElementLike[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elementsOfType(child, type, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as ElementLike;
  if (element.type === type) {
    found.push(element);
  }
  elementsOfType(element.props?.children, type, found);

  return found;
}

/** The visible text of a Text element, which may be a bare string. */
function textOf(element: ElementLike): unknown {
  return element.props?.children;
}

describe('ReportAction', () => {
  it('labels the action in words rather than an ellipsis', () => {
    const tree = ReportAction({ token: 'token', entityType: 'comment', entityId: 'comment-1' });
    const labels = elementsOfType(tree, 'Text').map(textOf);

    expect(labels).toContain('Report');
    // The old trigger was the literal glyph; it must not come back.
    expect(labels).not.toContain('⋯');
  });

  it('draws the label in the danger ink and names the action for a screen reader', () => {
    const tree = ReportAction({ token: 'token', entityType: 'comment', entityId: 'comment-1' });
    const label = elementsOfType(tree, 'Text').find((element) => textOf(element) === 'Report');
    const style = label?.props?.style as readonly Record<string, unknown>[] | undefined;
    const trigger = elementsOfType(tree, 'Pressable').find(
      (element) => element.props?.accessibilityLabel === 'Report content',
    );

    expect(style?.[style.length - 1]).toMatchObject({ color: colors.state.danger });
    expect(trigger).toBeDefined();
  });

  it('offers no trigger without a token, because reporting needs an account', () => {
    expect(ReportAction({ entityType: 'comment', entityId: 'comment-1' })).toBeNull();
  });
});
