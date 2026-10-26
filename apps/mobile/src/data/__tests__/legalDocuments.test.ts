import { readFileSync } from 'fs';
import path from 'path';

import {
  PRIVACY_POLICY,
  TERMS_OF_SERVICE,
  legalDocumentText,
  legalDocumentTitle,
} from '../legalDocuments';

/**
 * The app bundles its own copy of the two legal documents because it cannot read a
 * repository file at runtime and no endpoint serves them (KNOT-035). A copy is a
 * drift risk, so this suite reads the canonical markdown straight off disk and
 * fails the moment the two disagree — which is what makes the bundled text
 * trustworthy rather than merely convenient.
 *
 * The test runs in Node (the project's `testEnvironment`), so `fs` is available and
 * this is the one suite that touches the repository itself.
 */
const REPO_ROOT = path.join(__dirname, '../../../../..');

function canonical(fileName: string): string {
  return readFileSync(path.join(REPO_ROOT, 'docs', fileName), 'utf8');
}

describe('legalDocuments', () => {
  it('bundles the Privacy Policy exactly as docs/PRIVACY_POLICY.md holds it', () => {
    expect(PRIVACY_POLICY).toBe(canonical('PRIVACY_POLICY.md'));
  });

  it('bundles the Terms of Service exactly as docs/TERMS_OF_SERVICE.md holds it', () => {
    expect(TERMS_OF_SERVICE).toBe(canonical('TERMS_OF_SERVICE.md'));
  });

  it('resolves each document to its own text', () => {
    expect(legalDocumentText('privacy')).toBe(PRIVACY_POLICY);
    expect(legalDocumentText('terms')).toBe(TERMS_OF_SERVICE);
  });

  it('names each document the way the screens and the consent row do', () => {
    expect(legalDocumentTitle('privacy')).toBe('Privacy Policy');
    expect(legalDocumentTitle('terms')).toBe('Terms of Service');
  });
});
