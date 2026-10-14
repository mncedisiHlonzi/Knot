// The API base URL is mocked so importing the client does not pull in the
// gitignored local secrets file, which is absent in CI (KNOT-011b).
jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://api.test' }));

import { PLACE_COUNTRY_ABSENT, placeQuery } from '../discovery';

describe('placeQuery', () => {
  it('is empty when nothing is asked for', () => {
    expect(placeQuery({})).toBe('');
  });

  it('omits an undefined country, so the whole place is listed', () => {
    expect(placeQuery({ country: undefined })).toBe('');
    expect(placeQuery({ country: undefined })).not.toContain('country');
  });

  it('omits an empty country, which the server reads as no filter', () => {
    expect(placeQuery({ country: '' })).toBe('');
  });

  it('sends the reserved token for a null country', () => {
    // A country-less cluster is a real filter, not an absent one.
    expect(placeQuery({ country: null })).toBe(`?country=${PLACE_COUNTRY_ABSENT}`);
    expect(PLACE_COUNTRY_ABSENT).toBe('null');
  });

  it('sends a country verbatim and URL-encoded', () => {
    expect(placeQuery({ country: 'China' })).toBe('?country=China');
    expect(placeQuery({ country: 'South Africa' })).toBe('?country=South%20Africa');
  });

  it('combines the country with the limit and the cursor', () => {
    expect(placeQuery({ country: 'China', cursor: 'abc123', limit: 20 })).toBe(
      '?limit=20&country=China&cursor=abc123',
    );
    expect(placeQuery({ country: null, cursor: 'abc123' })).toBe(
      `?country=${PLACE_COUNTRY_ABSENT}&cursor=abc123`,
    );
  });
});
