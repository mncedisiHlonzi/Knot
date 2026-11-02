import convertToProxyUrl from 'react-native-video-cache';

import { cachedVideoUri, isCachedVideoUri } from '../videoCache';

/**
 * The stream cache's rules (KNOT-037-fix10).
 *
 * `react-native-video-cache` is mocked as a plain `jest.fn`, so these tests pin
 * WHAT the app asks the native proxy for, and what it does when the proxy is not
 * there. The real conversion is a native call and cannot run here.
 */
jest.mock('react-native-video-cache', () => ({
  __esModule: true,
  default: jest.fn((url: string) => `http://127.0.0.1:8089/proxy?url=${url}`),
  convertAsync: async (url: string) => url,
}));

const convert = convertToProxyUrl as unknown as jest.Mock;

const STREAM = 'http://10.0.2.2:8080/stories/s/media/m/content?variant=stream';

beforeEach(() => {
  convert.mockClear();
});

describe('isCachedVideoUri', () => {
  it('recognises the local proxy address', () => {
    expect(isCachedVideoUri('http://127.0.0.1:8089/proxy?url=x')).toBe(true);
    expect(isCachedVideoUri('http://localhost:8089/x')).toBe(true);
  });

  it('does not mistake a remote stream for the proxy', () => {
    expect(isCachedVideoUri(STREAM)).toBe(false);
  });
});

describe('cachedVideoUri', () => {
  it('converts a remote stream to the local proxy address', () => {
    expect(cachedVideoUri(STREAM)).toBe(`http://127.0.0.1:8089/proxy?url=${STREAM}`);
    expect(convert).toHaveBeenCalledWith(STREAM);
  });

  it('never wraps an address that is already the proxy', () => {
    const proxied = 'http://127.0.0.1:8089/proxy?url=x';

    expect(cachedVideoUri(proxied)).toBe(proxied);
    expect(convert).not.toHaveBeenCalled();
  });

  it('leaves a non-HTTP address alone', () => {
    expect(cachedVideoUri('file:///tmp/clip.mp4')).toBe('file:///tmp/clip.mp4');
    expect(convert).not.toHaveBeenCalled();
  });

  it('leaves an empty address alone', () => {
    expect(cachedVideoUri('')).toBe('');
    expect(convert).not.toHaveBeenCalled();
  });

  it('falls back to the original stream when the proxy cannot start', () => {
    convert.mockImplementationOnce(() => {
      throw new Error('native module missing');
    });

    expect(cachedVideoUri(STREAM)).toBe(STREAM);
  });

  it('falls back to the original stream when the proxy answers nothing', () => {
    convert.mockReturnValueOnce('');

    expect(cachedVideoUri(STREAM)).toBe(STREAM);
  });
});
