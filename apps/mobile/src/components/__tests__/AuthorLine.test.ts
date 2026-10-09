import AuthorLine from '../AuthorLine';

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
});
