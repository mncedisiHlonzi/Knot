import Header from '../Header';

// Header renders React Native views; stub them so the Flow source is not parsed.
jest.mock('react-native', () => ({
  Animated: { View: 'Animated.View', Value: class AnimatedValue {} },
  StyleSheet: { create: (styles: unknown) => styles, absoluteFillObject: {} },
  Text: 'Text',
  View: 'View',
}));

// Header is stateful (a scroll listener tracks whether it is lifted). There is no
// renderer in this project, so the hooks are stubbed to their initial values and
// the component is called directly to prove it renders without throwing
// (KNOT-020b).
jest.mock('react', () => {
  const actual = jest.requireActual('react');
  return {
    ...actual,
    useEffect: () => undefined,
    useState: (initial: unknown) => [initial, () => undefined],
  };
});

describe('Header', () => {
  it('renders a title with no scroll offset', () => {
    expect(Header({ title: 'Comments' })).toBeTruthy();
  });

  it('renders left and right slots and a title-less bar', () => {
    expect(Header({ title: 'x', left: null, right: null })).toBeTruthy();
    expect(Header({})).toBeTruthy();
  });
});
