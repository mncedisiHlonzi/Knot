// The API base URL is mocked so importing the client does not pull in the
// gitignored local secrets file, which is absent in CI (KNOT-011b).
jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://api.test' }));

import {
  BLOCKED_ERROR_CODE,
  BLOCKED_MESSAGE,
  MAX_REPORT_REASON_LENGTH,
  REPORT_CATEGORIES,
  reportReasonError,
} from '../moderation';

describe('report categories', () => {
  it('offers the six categories, ending with other', () => {
    const values = REPORT_CATEGORIES.map((category) => category.value);
    expect(values).toEqual([
      'harassment',
      'hate_speech',
      'misinformation',
      'spam',
      'sensitive_content',
      'other',
    ]);
  });
});

describe('reportReasonError', () => {
  it('requires a reason for the other category', () => {
    expect(reportReasonError('other', '   ')).toBeDefined();
  });

  it('accepts a reason for the other category', () => {
    expect(reportReasonError('other', 'something specific')).toBeUndefined();
  });

  it('treats the reason as optional for a named category', () => {
    expect(reportReasonError('harassment', '')).toBeUndefined();
  });

  it('rejects a reason longer than the server allows', () => {
    const tooLong = 'x'.repeat(MAX_REPORT_REASON_LENGTH + 1);
    expect(reportReasonError('spam', tooLong)).toBeDefined();
  });
});

describe('blocked error mapping', () => {
  it('names the server code and the user-facing message', () => {
    expect(BLOCKED_ERROR_CODE).toBe('blocked');
    expect(BLOCKED_MESSAGE).toBe('You cannot interact with this content.');
  });
});
