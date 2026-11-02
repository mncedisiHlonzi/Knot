import { stripImagePath } from '../mediaStripSource';

/**
 * The media strip's source rule (KNOT-036-fix4, KNOT-037-fix10).
 *
 * The strip is a 64pt thumbnail row, so it draws the smallest rendition the server
 * generated. Both fallbacks below are real cases: a row written before KNOT-037 has
 * no rendition at all, and an older server does not send the field.
 *
 * A video has no tile source to resolve: the server stopped generating a poster
 * frame (KNOT-037-fix10), so the tile keeps its placeholder and play disc.
 */
describe('stripImagePath', () => {
  const original = '/stories/story-1/media/media-1/content';

  it('prefers the generated thumbnail', () => {
    expect(stripImagePath({ url: original, thumbnail_url: `${original}?variant=thumb` })).toBe(
      `${original}?variant=thumb`,
    );
  });

  it('falls back to the original when the thumbnail is null', () => {
    expect(stripImagePath({ url: original, thumbnail_url: null })).toBe(original);
  });

  it('falls back to the original when the field is absent', () => {
    expect(stripImagePath({ url: original })).toBe(original);
  });

  it('treats a blank thumbnail as absent', () => {
    expect(stripImagePath({ url: original, thumbnail_url: '' })).toBe(original);
  });
});
