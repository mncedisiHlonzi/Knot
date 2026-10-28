import { stripImagePath, stripPosterPath } from '../mediaStripSource';

/**
 * The media strip's source rules (KNOT-036-fix4).
 *
 * The strip is a 64pt thumbnail row, so it draws the smallest rendition the server
 * generated. Every fallback below is a real case: a row written before KNOT-037 has
 * no rendition at all, an upload whose decoder or FFmpeg failed has none either,
 * and an older server does not send the field.
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

describe('stripPosterPath', () => {
  const original = '/stories/story-1/media/media-2/content';

  it('prefers the generated poster frame', () => {
    expect(stripPosterPath({ poster_url: `${original}?variant=poster` })).toBe(
      `${original}?variant=poster`,
    );
  });

  it('has no poster when the field is null, so the tile keeps its placeholder', () => {
    expect(stripPosterPath({ poster_url: null })).toBeUndefined();
  });

  it('has no poster when the field is absent', () => {
    expect(stripPosterPath({})).toBeUndefined();
  });

  it('never returns the original video path, which an Image cannot draw', () => {
    expect(stripPosterPath({ poster_url: '' })).toBeUndefined();
  });
});
