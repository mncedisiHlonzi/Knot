import { mediaUploadLabel } from '../mediaUpload';

/**
 * The label a published story's card shows while its bytes are still going up
 * (KNOT-040). Publishing is optimistic, so this text is the only upload feedback
 * the reader gets — it is pinned here so it cannot drift.
 */
describe('mediaUploadLabel', () => {
  it('says nothing when no upload is running', () => {
    expect(mediaUploadLabel(undefined)).toBeUndefined();
    expect(mediaUploadLabel({ completed: 0, total: 0, failed: false })).toBeUndefined();
  });

  it('names the file being uploaded, counting from one', () => {
    expect(mediaUploadLabel({ completed: 0, total: 2, failed: false })).toBe(
      'Uploading media 1 of 2...',
    );
    expect(mediaUploadLabel({ completed: 1, total: 2, failed: false })).toBe(
      'Uploading media 2 of 2...',
    );
  });

  it('stops speaking once every byte is up, because the server is encoding then', () => {
    expect(mediaUploadLabel({ completed: 2, total: 2, failed: false })).toBeUndefined();
    expect(mediaUploadLabel({ completed: 1, total: 1, failed: false })).toBeUndefined();
  });

  it('reports a failure instead of progress', () => {
    expect(mediaUploadLabel({ completed: 1, total: 2, failed: true })).toBe('Media upload failed');
    expect(mediaUploadLabel({ completed: 2, total: 2, failed: true })).toBe('Media upload failed');
  });
});
