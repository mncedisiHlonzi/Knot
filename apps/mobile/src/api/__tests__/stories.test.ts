import { feedQuery } from '../stories';

/**
 * The feed's query string is the contract between the app and the ranked feed:
 * `feed_type` must reach the server, and an omitted ordering must stay omitted
 * so the server keeps its chronological default (KNOT-027).
 */
describe('feedQuery', () => {
  it('asks for the ranked feed when the ordering is for_you', () => {
    expect(feedQuery({ feedType: 'for_you', limit: 20 })).toBe('?limit=20&feed_type=for_you');
  });

  it('asks for the chronological feed when the ordering is recent', () => {
    expect(feedQuery({ feedType: 'recent' })).toBe('?feed_type=recent');
  });

  it('omits feed_type when no ordering is asked for', () => {
    expect(feedQuery({})).toBe('');
  });

  it('carries the cursor alongside the ordering', () => {
    expect(feedQuery({ cursor: 'abc', feedType: 'for_you' })).toBe('?cursor=abc&feed_type=for_you');
  });
});
