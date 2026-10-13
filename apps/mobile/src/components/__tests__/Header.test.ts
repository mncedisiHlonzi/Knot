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
});
