import * as fs from 'fs';
import * as path from 'path';

import { DEFAULT_LANGUAGE_CODE } from '../language';

/** `apps/mobile`, three levels up from `src/config/__tests__`. */
const APP_ROOT = path.resolve(__dirname, '..', '..', '..');
const SOURCE_ROOT = path.join(APP_ROOT, 'src');
const SOURCE_EXTENSIONS = ['.ts', '.tsx'];

/**
 * Removes line and block comments so a code named in prose cannot be mistaken
 * for a code in use. The line-comment pattern keeps `://` intact, so a URL is
 * not stripped.
 */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/(^|[^:])\/\/[^\n]*/gm, '$1');
}

/** Every `.ts`/`.tsx` file under `src` (tests excluded) plus `App.tsx`. */
function sourceFiles(): string[] {
  const files: string[] = [];

  const walk = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === '__tests__' || entry.name === 'node_modules') {
          continue;
        }
        walk(full);
      } else if (SOURCE_EXTENSIONS.includes(path.extname(entry.name))) {
        files.push(full);
      }
    }
  };

  walk(SOURCE_ROOT);
  files.push(path.join(APP_ROOT, 'App.tsx'));
  return files;
}

describe('language defaults', () => {
  it('uses the canonical ISO 639-3 fallback', () => {
    expect(DEFAULT_LANGUAGE_CODE).toBe('eng');
  });

  it('declares the fallback constant in src/config/language.ts', () => {
    const source = fs.readFileSync(path.join(__dirname, '..', 'language.ts'), 'utf8');
    expect(source).toMatch(/DEFAULT_LANGUAGE_CODE\s*=\s*'eng'/);
  });

  it('never hard-codes a two-letter ISO 639-1 code in user-facing source', () => {
    // `'en'` or `"en"` as a whole token — the pre-KNOT-015d default. Matching a
    // lower-case `en` surrounded by the same quote leaves `'eng'` alone.
    const offenders: string[] = [];

    for (const file of sourceFiles()) {
      const source = stripComments(fs.readFileSync(file, 'utf8'));
      if (/(['"])en\1/.test(source)) {
        offenders.push(path.relative(APP_ROOT, file));
      }
    }

    expect(offenders).toEqual([]);
  });
});
