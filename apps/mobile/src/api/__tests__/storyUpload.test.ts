jest.mock('../client', () => ({ request: jest.fn() }));

import { request } from '../client';
import { MAX_IMAGE_BYTES } from '../../config/dev';
import { putMediaBytes, requestMediaUploadUrl, uploadStoryMedia } from '../stories';

/**
 * The direct-to-storage upload (KNOT-040) is three steps, and only the first and
 * last touch the API. These tests pin that split: the bytes must go to the signed
 * URL, never through `request`, and the notify call must carry the object key the
 * URL came with.
 */
const STORY_ID = '55555555-5555-4555-8555-555555555555';
const TOKEN = 'test-token';
const UPLOAD_URL = 'https://objects.test/bucket/key?X-Amz-Signature=abc';
const OBJECT_KEY = `story-media/${STORY_ID}/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa_original.jpg`;

const mockRequest = request as jest.Mock;
const mockFetch = jest.fn();

beforeEach(() => {
  mockRequest.mockReset();
  mockFetch.mockReset();
  (globalThis as unknown as { fetch: typeof mockFetch }).fetch = mockFetch;
});

/** Answers the file read and the upload with the responses the flow expects. */
function stubFetchForUpload(): void {
  mockFetch
    .mockResolvedValueOnce({ blob: async () => ({ size: 12 }) })
    .mockResolvedValueOnce({ ok: true, status: 200 });
}

describe('requestMediaUploadUrl', () => {
  it('asks for a signed URL for the story and content type', async () => {
    mockRequest.mockResolvedValue({
      upload_url: UPLOAD_URL,
      object_key: OBJECT_KEY,
      expires_in: 900,
    });

    const target = await requestMediaUploadUrl(STORY_ID, 'image/jpeg', TOKEN);

    expect(target).toEqual({ upload_url: UPLOAD_URL, object_key: OBJECT_KEY, expires_in: 900 });
    expect(mockRequest).toHaveBeenCalledWith('/media/upload-url', {
      method: 'POST',
      body: {
        story_id: STORY_ID,
        filename: 'story-media.jpg',
        content_type: 'image/jpeg',
      },
      token: TOKEN,
    });
  });
});

describe('putMediaBytes', () => {
  it('reads the picked file and PUTs its bytes to the signed URL', async () => {
    stubFetchForUpload();

    await putMediaBytes(UPLOAD_URL, 'file:///tmp/photo.jpg', 'image/jpeg');

    expect(mockFetch).toHaveBeenNthCalledWith(1, 'file:///tmp/photo.jpg');
    expect(mockFetch).toHaveBeenNthCalledWith(2, UPLOAD_URL, {
      method: 'PUT',
      body: { size: 12 },
      headers: { 'Content-Type': 'image/jpeg' },
    });
  });

  it('reports a refusal from storage as a readable error', async () => {
    mockFetch
      .mockResolvedValueOnce({ blob: async () => ({ size: 12 }) })
      .mockResolvedValueOnce({ ok: false, status: 403 });

    await expect(putMediaBytes(UPLOAD_URL, 'file:///tmp/photo.jpg', 'image/jpeg')).rejects.toThrow(
      'Storage refused the upload (403).',
    );
  });

  it('reports an unreachable store as a readable error', async () => {
    mockFetch
      .mockResolvedValueOnce({ blob: async () => ({ size: 12 }) })
      .mockImplementationOnce(() => Promise.reject(new Error('network down')));

    await expect(putMediaBytes(UPLOAD_URL, 'file:///tmp/photo.jpg', 'image/jpeg')).rejects.toThrow(
      'could not reach storage',
    );
  });

  it('refuses a file it cannot read', async () => {
    mockFetch.mockImplementationOnce(() => Promise.reject(new Error('no access')));

    await expect(putMediaBytes(UPLOAD_URL, 'content://missing', 'image/jpeg')).rejects.toThrow(
      'could not be read',
    );
  });

  it('refuses an empty file', async () => {
    mockFetch.mockResolvedValueOnce({ blob: async () => ({ size: 0 }) });

    await expect(putMediaBytes(UPLOAD_URL, 'file:///tmp/empty.jpg', 'image/jpeg')).rejects.toThrow(
      'is empty',
    );
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });
});

describe('uploadStoryMedia', () => {
  it('signs, uploads, then reports the object key', async () => {
    mockRequest
      .mockResolvedValueOnce({
        upload_url: UPLOAD_URL,
        object_key: OBJECT_KEY,
        expires_in: 900,
      })
      .mockResolvedValueOnce({ media: { id: 'media-1' } });
    stubFetchForUpload();

    const media = await uploadStoryMedia(
      STORY_ID,
      'file:///tmp/photo.jpg',
      'image/jpeg',
      'camera',
      TOKEN,
      1024,
      { width: 300, height: 400 },
    );

    expect(media).toEqual({ id: 'media-1' });
    expect(mockRequest).toHaveBeenNthCalledWith(1, '/media/upload-url', {
      method: 'POST',
      body: {
        story_id: STORY_ID,
        filename: 'story-media.jpg',
        content_type: 'image/jpeg',
      },
      token: TOKEN,
    });
    // The bytes went to the signed URL, not to the API.
    expect(mockFetch).toHaveBeenNthCalledWith(2, UPLOAD_URL, {
      method: 'PUT',
      body: { size: 12 },
      headers: { 'Content-Type': 'image/jpeg' },
    });
    expect(mockRequest).toHaveBeenNthCalledWith(2, `/stories/${STORY_ID}/media`, {
      method: 'POST',
      body: {
        object_key: OBJECT_KEY,
        source: 'camera',
        width: 300,
        height: 400,
      },
      token: TOKEN,
    });
  });

  it('carries a video duration through to the notify call', async () => {
    mockRequest
      .mockResolvedValueOnce({
        upload_url: UPLOAD_URL,
        object_key: OBJECT_KEY,
        expires_in: 900,
      })
      .mockResolvedValueOnce({ media: { id: 'media-2' } });
    stubFetchForUpload();

    await uploadStoryMedia(STORY_ID, 'file:///tmp/clip.mp4', 'video/mp4', 'gallery', TOKEN, 2048, {
      durationMs: 3000,
    });

    expect(mockRequest).toHaveBeenNthCalledWith(2, `/stories/${STORY_ID}/media`, {
      method: 'POST',
      body: { object_key: OBJECT_KEY, source: 'gallery', duration_ms: 3000 },
      token: TOKEN,
    });
  });

  it('does not call a failed upload successful when the notify call fails', async () => {
    mockRequest
      .mockResolvedValueOnce({
        upload_url: UPLOAD_URL,
        object_key: OBJECT_KEY,
        expires_in: 900,
      })
      .mockRejectedValueOnce(new Error('notify failed'));
    stubFetchForUpload();

    await expect(
      uploadStoryMedia(STORY_ID, 'file:///tmp/photo.jpg', 'image/jpeg', 'camera', TOKEN, 1024),
    ).rejects.toThrow('notify failed');
  });

  it('refuses an oversized file before asking for a URL', async () => {
    await expect(
      uploadStoryMedia(
        STORY_ID,
        'file:///tmp/huge.jpg',
        'image/jpeg',
        'camera',
        TOKEN,
        MAX_IMAGE_BYTES + 1,
      ),
    ).rejects.toThrow('larger than');

    expect(mockRequest).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
  });
});
