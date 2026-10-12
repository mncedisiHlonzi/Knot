/**
 * TypeScript declarations for direct `.svg` imports (KNOT-020b-fix).
 *
 * `react-native-svg-transformer` (a devDependency, wired into `metro.config.js`)
 * rewrites an imported `.svg` file into a React component that renders with
 * `react-native-svg`. Metro does that at bundle time, but TypeScript has no such
 * transform, so this ambient declaration tells it the shape of the import:
 * `import Logomark from '../../assets/brand/logo-logomark.svg'` yields a component
 * taking the usual `SvgProps`.
 *
 * Jest has no Metro transform either; `jest.config.js` maps every `.svg` specifier
 * to `src/assets/__mocks__/svgMock.ts` so the same imports load under test.
 */
declare module '*.svg' {
  import type { FC } from 'react';
  import type { SvgProps } from 'react-native-svg';

  const content: FC<SvgProps>;
  export default content;
}
