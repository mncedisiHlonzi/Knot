import * as fs from 'fs';
import * as path from 'path';

import { buildQueryString } from '../query';

/** `apps/mobile`, three levels up from `src/api/__tests__`. */
const APP_ROOT = path.resolve(__dirname, '..', '..', '..');
const SOURCE_ROOT = path.join(APP_ROOT, 'src');
const SOURCE_EXTENSIONS = ['.ts', '.tsx'];

/**
 * Removes line and block comments so a mention in prose is not a match. The
 * line-comment pattern keeps `://` intact, so a URL is not stripped.
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

describe('buildQueryString', () => {
  it('returns an empty string when there is nothing to add', () => {
    expect(buildQueryString({})).toBe('');
    expect(buildQueryString({ cursor: undefined })).toBe('');
    expect(buildQueryString({ cursor: '' })).toBe('');
  });

  it('joins and percent-encodes its parameters', () => {
    expect(buildQueryString({ cursor: 'a b/c', limit: 20 })).toBe('?cursor=a%20b%2Fc&limit=20');
  });

  it('keeps a zero rather than dropping it', () => {
    expect(buildQueryString({ limit: 0 })).toBe('?limit=0');
  });
});

describe('URLSearchParams mutation', () => {
  // React Native's URLSearchParams polyfill has no mutation methods, so calling
  // .set/.append/.delete throws at runtime on a device. TypeScript and ESLint do
  // not catch it, so this scan does (KNOT-017a-fix).
  it('is never used in user-facing source', () => {
    const offenders: string[] = [];

    for (const file of sourceFiles()) {
      const source = stripComments(fs.readFileSync(file, 'utf8'));
      const constructs = /new\s+URLSearchParams/.test(source);
      const mutates = /\.(set|append|delete)\s*\(/.test(source);
      if (constructs && mutates) {
        offenders.push(path.relative(APP_ROOT, file));
      }
    }

    expect(offenders).toEqual([]);
  });
});
