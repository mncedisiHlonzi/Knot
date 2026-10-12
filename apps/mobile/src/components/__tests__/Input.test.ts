import { resolveUnderline } from '../Input';

// Input imports React Native components at module load; stub them so the Flow
// source is not parsed. The component itself is not rendered here.
jest.mock('react-native', () => ({
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  TextInput: 'TextInput',
  View: 'View',
}));

/**
 * Input is a stateful component and there is no renderer in this project, so the
 * one piece of logic it carries — which underline state wins — is exported and
 * tested here (KNOT-020b).
 */
describe('resolveUnderline', () => {
  it('reports the resting state when neither focused nor errored', () => {
    expect(resolveUnderline(false, false)).toBe('rest');
  });

  it('reports focus when focused without an error', () => {
    expect(resolveUnderline(true, false)).toBe('focus');
  });

  it('lets an error outrank focus', () => {
    expect(resolveUnderline(true, true)).toBe('error');
    expect(resolveUnderline(false, true)).toBe('error');
  });
});
