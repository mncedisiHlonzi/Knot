import js from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    // Config files are plain CommonJS and are not part of the app surface; the
    // native projects are generated and are not linted by ESLint.
    ignores: [
      'node_modules/**',
      'coverage/**',
      'dist/**',
      'ios/**',
      'android/**',
      'babel.config.js',
      'jest.config.js',
      'metro.config.js',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
);
