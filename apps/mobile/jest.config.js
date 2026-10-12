/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['**/__tests__/**/*.test.ts'],
  // Jest resolves modules through Node, which does not read the committed
  // `secrets.local.d.ts` type shim. The gitignored `secrets.local.ts` is
  // therefore absent on a fresh clone (CI), so importing `./secrets.local`
  // fails. Map every import of it — from anywhere in the tree — to a
  // committed, test-only placeholder (KNOT-018-fix). The mock wins even when
  // the real local file exists, so tests are identical on every machine.
  // Metro turns a `.svg` import into a component via `react-native-svg-transformer`
  // (see metro.config.js), but Jest has no such transform. Map every `.svg`
  // specifier to a committed stub that renders nothing, so a component importing a
  // brand asset still loads under test (KNOT-020b-fix).
  moduleNameMapper: {
    '^.*/secrets\\.local$': '<rootDir>/src/config/__mocks__/secrets.local.ts',
    '\\.svg$': '<rootDir>/src/assets/__mocks__/svgMock.ts',
  },
};
