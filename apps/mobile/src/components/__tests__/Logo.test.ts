import Logo from '../Logo';
import Logomark from '../Logomark';
import Wordmark from '../Wordmark';

/**
 * The logo components render brand SVG files. Metro turns a `.svg` import into a
 * component via `react-native-svg-transformer`; Jest maps every `.svg` specifier
 * to `src/assets/__mocks__/svgMock.ts` (see `jest.config.js`), so these imports
 * resolve and each component can be called directly as a plain function — the
 * established pattern, since the project has no renderer (KNOT-020b).
 */
describe('Logomark', () => {
  it('renders square, at the default size and at a given size', () => {
    expect(Logomark({}).props.width).toBe(32);
    expect(Logomark({ size: 48 }).props.width).toBe(48);
    expect(Logomark({ size: 48 }).props.height).toBe(48);
  });

  it('renders every variant', () => {
    for (const variant of ['primary', 'light', 'dark', 'mono-light', 'mono-dark'] as const) {
      expect(Logomark({ variant })).toBeTruthy();
    }
  });
});

describe('Wordmark', () => {
  it('renders at the default height, width following the asset aspect ratio', () => {
    const element = Wordmark({});
    expect(element.props.height).toBe(24);
    expect(element.props.width).toBe(60);
  });

  it('renders both variants', () => {
    expect(Wordmark({ variant: 'dark' })).toBeTruthy();
    expect(Wordmark({ variant: 'light' })).toBeTruthy();
  });
});

describe('Logo', () => {
  it('renders at the default height, width following the asset aspect ratio', () => {
    const element = Logo({});
    expect(element.props.height).toBe(32);
    expect(element.props.width).toBe(120);
  });

  it('renders every variant', () => {
    for (const variant of ['primary', 'light', 'dark', 'mono-light', 'mono-dark'] as const) {
      expect(Logo({ variant })).toBeTruthy();
    }
  });
});
