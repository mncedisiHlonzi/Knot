import AuthorLine from '../AuthorLine';
import Avatar from '../Avatar';
import RootedBadge from '../RootedBadge';

// AuthorLine imports React Native components to render with, but these tests call
// the component as a plain function and never render, so the native modules are
// stubbed. Stubbing also keeps the test independent of the gitignored
// `secrets.local` module that `src/config/api` pulls in (KNOT-ADR-027).
jest.mock('react-native', () => ({
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

/**
 * Finds the first element of the given component type in a directly-called
 * component's element tree. There is no renderer, so a structural search is how
 * a parent's choice of child component is proven (KNOT-020c-fix4).
 */
function findElementByType(
  node: unknown,
  type: unknown,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElementByType(child, type);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as { type?: unknown; props?: { children?: unknown } };
  if (element.type === type) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findElementByType(element.props.children, type);
  }
  return undefined;
}

/**
 * AuthorLine is a plain function component with no hooks, so it can be invoked
 * directly. These assertions are exactly the regression that crashed the Feed:
 * missing author fields must not throw, and a line with nothing in it must render
 * null rather than an empty row. Rendering it through `react-test-renderer` would
 * require a new dependency, which a bug fix must not add.
 */
describe('AuthorLine', () => {
  it('renders nothing when every author field is missing', () => {
    expect(
      AuthorLine({
        displayName: undefined,
        avatarUrl: undefined,
        createdAt: undefined,
        rooted: null,
      }),
    ).toBeNull();
  });

  it('does not throw when the author fields are null', () => {
    expect(() =>
      AuthorLine({ displayName: null, avatarUrl: null, createdAt: null, rooted: null }),
    ).not.toThrow();
  });

  it('does not throw on a non-string display name', () => {
    expect(() =>
      AuthorLine({
        displayName: 42 as unknown as string,
        avatarUrl: undefined,
        createdAt: '2026-10-09T12:00:00.000Z',
      }),
    ).not.toThrow();
  });

  it('still renders when only the timestamp is present', () => {
    expect(
      AuthorLine({
        displayName: undefined,
        avatarUrl: undefined,
        createdAt: '2026-10-09T12:00:00.000Z',
      }),
    ).not.toBeNull();
  });

  it('renders without onPress and never throws', () => {
    const props = {
      displayName: 'Ada Lovelace',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
    };

    expect(() => AuthorLine(props)).not.toThrow();
    expect(AuthorLine(props)).not.toBeNull();
  });

  it('accepts an onPress without invoking it during render', () => {
    const onPress = jest.fn();

    const element = AuthorLine({
      displayName: 'Ada Lovelace',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      onPress,
    });

    expect(element).not.toBeNull();
    expect(onPress).not.toHaveBeenCalled();
  });

  // The Feed's two-line form (KNOT-020c-fix2): the Rooted summary leaves the
  // identity line and becomes the quiet badge — no dot, no pill, no mark — on its
  // own line beneath the name and time (KNOT-020c-fix3, KNOT-020c-fix4).
  it('renders the quiet Rooted line on its own row when showRootedOnNewLine is set', () => {
    const element = AuthorLine({
      displayName: 'MaEnzy',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      rooted: { place: 'Manguzi', duration_bucket: 'lifelong' },
      showRootedOnNewLine: true,
    });

    const badge = findElementByType(element, RootedBadge);
    expect(badge).toBeDefined();
    expect(badge?.props?.variant).toBe('quiet');
    expect(badge?.props?.place).toBe('Manguzi');
    expect(badge?.props?.durationBucket).toBe('lifelong');
    // The Feed drops the duration, so the line reads "Rooted · Manguzi" (fix5).
    expect(badge?.props?.showDuration).toBe(false);
  });

  it('renders no Rooted line when the author has no signal', () => {
    const element = AuthorLine({
      displayName: 'MaEnzy',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      rooted: null,
      showRootedOnNewLine: true,
    });

    expect(findElementByType(element, RootedBadge)).toBeUndefined();
  });

  // Item 1 (KNOT-020c-fix5): the Feed draws a 48-point avatar, which the existing
  // `large` size already maps to.
  it('draws the Feed avatar at the large size', () => {
    const element = AuthorLine({
      displayName: 'MaEnzy',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      size: 'large',
    });

    expect(findElementByType(element, Avatar)?.props?.size).toBe('lg');
  });

  // Item 2 (KNOT-020c-fix5): the quiet Rooted line lives in the author's text
  // column, beside the avatar — not as a full-width line under it — so it shares
  // the name's left edge rather than the avatar's.
  it('hangs the quiet Rooted line off the text column beside the avatar', () => {
    const element = AuthorLine({
      displayName: 'MaEnzy',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      rooted: { place: 'Manguzi', duration_bucket: 'lifelong' },
      showRootedOnNewLine: true,
    }) as unknown as { props: { children: readonly unknown[] } };

    // Two children: the avatar slot and the text column.
    expect(element.props.children).toHaveLength(2);
    const [avatarSlot, details] = element.props.children as [
      unknown,
      { props: { children: readonly unknown[] } },
    ];
    expect(findElementByType(avatarSlot, Avatar)).toBeDefined();

    // The column holds the name row, then the Rooted line.
    const column = details.props.children;
    expect(findElementByType(column[0], 'Text')).toBeDefined();
    expect((column[1] as { type: unknown }).type).toBe(RootedBadge);
  });

  // The Story Detail's two-line block (KNOT-025-fix1): name + inline Rooted on
  // line one, language + relative time on line two, both in the column beside the
  // avatar so the second line starts at the name's edge.
  describe('the detail layout', () => {
    const detail = {
      displayName: 'Nomsa Khan',
      avatarUrl: null,
      createdAt: '2026-10-09T12:00:00.000Z',
      rooted: { place: 'Oaxaca', duration_bucket: 'many_years' },
      size: 'large' as const,
      layout: 'detail' as const,
      languageLabel: 'Zapotec',
    };

    it('puts the name and the Rooted badge on the first line', () => {
      const element = AuthorLine(detail) as unknown as {
        props: { children: readonly unknown[] };
      };

      // Two children: the avatar slot and the text column.
      expect(element.props.children).toHaveLength(2);
      expect(findElementByType(element.props.children[0], Avatar)?.props?.size).toBe('lg');

      const details = element.props.children[1] as {
        props: { children: readonly unknown[] };
      };
      const [nameRow] = details.props.children;
      const badge = findElementByType(nameRow, RootedBadge);

      expect(badge).toBeDefined();
      // The duration is dropped so a typical name and place stay on one line.
      expect(badge?.props?.showDuration).toBe(false);
      // The name truncates rather than pushing the badge off the row.
      expect(findElementByType(nameRow, 'Text')?.props?.numberOfLines).toBe(1);
    });

    it('puts the language and relative time on the second line', () => {
      const element = AuthorLine(detail) as unknown as {
        props: { children: readonly unknown[] };
      };

      const details = element.props.children[1] as {
        props: { children: readonly unknown[] };
      };
      const meta = details.props.children[1] as {
        type: unknown;
        props: { children: unknown };
      };

      expect(meta.type).toBe('Text');
      expect(typeof meta.props.children).toBe('string');
      expect(meta.props.children as string).toMatch(/^Zapotec · /);
    });

    it('keeps the inline layout rendering its identity, badge, and time', () => {
      const element = AuthorLine({ ...detail, layout: 'inline' }) as unknown as {
        props: { children: readonly unknown[] };
      };

      // Identity, Rooted badge, and time on one row.
      expect(element.props.children).toHaveLength(3);
      expect(findElementByType(element, RootedBadge)).toBeDefined();
    });
  });
});
