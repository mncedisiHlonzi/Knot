import type { FC } from 'react';
import type { SvgProps } from 'react-native-svg';

/**
 * Jest stand-in for `.svg` imports (KNOT-020b-fix).
 *
 * Metro turns a `.svg` import into a component via `react-native-svg-transformer`,
 * but Jest has no such transform — and the components that import an SVG are
 * otherwise unrelated to the test. `jest.config.js` maps every `.svg` specifier to
 * this module so those imports still load: it renders nothing, which is all the
 * direct-call component tests need (no renderer runs).
 */
const SvgMock: FC<SvgProps> = () => null;

export default SvgMock;
