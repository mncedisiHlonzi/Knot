import Header from '../Header';

// Header renders React Native views; stub them so the Flow source is not parsed.
jest.mock('react-native', () => ({
  StyleSheet: { create: (styles: unknown) => styles, absoluteFillObject: {} },
  Text: 'Text',
  View: 'View',
}));

describe('Header', () => {
  it('renders a title', () => {
    expect(Header({ title: 'Comments' })).toBeTruthy();
  });

  it('renders left and right slots and a title-less bar', () => {
    expect(Header({ title: 'x', left: null, right: null })).toBeTruthy();
    expect(Header({})).toBeTruthy();
  });

  // Header is static (KNOT-020c-fix4): the pinned form paints an opaque canvas
  // fill and nothing about it is scroll-linked, so calling it directly proves the
  // composition without a renderer. children[0] is the fill; it is null when the
  // header is floating over content.
  it('paints an opaque fill when pinned and nothing when floating', () => {
    const pinned = Header({ pinned: true });
    expect(pinned.props.children[0]).not.toBeNull();

    const floating = Header({ title: 'Comments' });
    expect(floating.props.children[0]).toBeNull();
  });

  /**
   * The row's children, in order: the two side slots, the middle slot, and — for a
   * centred title — the out-of-flow title container (KNOT-020k-fix11).
   */
  function rowChildren(header: { props?: { children?: unknown } }): unknown[] {
    const container = header.props?.children as unknown[];
    const row = container[1] as { props?: { children?: unknown } };
    return row.props?.children as unknown[];
  }

  it('centres the title out of flow by default', () => {
    const [, middle, , outOfFlow] = rowChildren(Header({ title: 'Inquiries' }));

    // The middle slot stays a flexible spacer; the title sits above the row.
    expect((middle as { type?: unknown }).type).toBe('View');
    expect((outOfFlow as { props?: { style?: { position?: string } } }).props?.style).toMatchObject(
      { position: 'absolute' },
    );
  });

  it('draws the title in flow when it is left-aligned', () => {
    const [, middle, , outOfFlow] = rowChildren(Header({ title: 'Inquiries', titleAlign: 'left' }));

    // The title itself is the middle child now, so it starts on the bar's gutter.
    expect((middle as { type?: unknown }).type).toBe('Text');
    expect((middle as { props?: { children?: unknown } }).props?.children).toBe('Inquiries');
    // …and nothing is drawn out of flow above it.
    expect(outOfFlow).toBeNull();
  });
});
