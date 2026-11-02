import convertToProxyUrl from 'react-native-video-cache';

/**
 * Local caching for story video streams (KNOT-037-fix10).
 *
 * A story's video is served from `?variant=stream` — a bespoke MP4 address per
 * media item. Without a cache, scrolling a video out of view and back downloads
 * the whole stream again, which is the one place the derivative pipeline still
 * costs the reader bandwidth twice for the same bytes.
 *
 * `react-native-video-cache` starts a local proxy and hands back a `127.0.0.1`
 * address for the same object. The player then reads through the proxy, which keeps
 * a disk copy: the first play downloads the stream, every later play of that URL is
 * served from the device. The conversion is a synchronous native call, so it costs
 * nothing on the JS thread and needs no loading state.
 *
 * Everything here is deliberately total: a URL that is already local, an address
 * that is not HTTP, and a build without the native module (a Jest run, or a dev
 * client linked before the dependency was added) all return the original address
 * unchanged. A cache that cannot be started must never stop a video from playing.
 *
 * ⚠️ The proxy speaks cleartext HTTP to `127.0.0.1`, which is why
 * `android/app/src/main/res/xml/network_security_config.xml` permits cleartext for
 * loopback only. A release build without that file would have every cached stream
 * refused by the platform.
 */

/** An address the proxy can serve: plain HTTP or HTTPS. */
const CACHEABLE_URI = /^https?:\/\//i;

/** An address that is already the local proxy, and must not be wrapped again. */
const LOOPBACK_URI = /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?(\/|$)/i;

/** Whether a URI already points at the local caching proxy. */
export function isCachedVideoUri(uri: string): boolean {
  return LOOPBACK_URI.test(uri);
}

/**
 * The URI a video player should load for `uri`.
 *
 * A cacheable remote stream becomes its local proxy address; anything else is
 * returned unchanged, including the failure case.
 */
export function cachedVideoUri(uri: string): string {
  if (typeof uri !== 'string' || !CACHEABLE_URI.test(uri) || LOOPBACK_URI.test(uri)) {
    return uri;
  }

  try {
    const proxied = convertToProxyUrl(uri);
    if (typeof proxied === 'string' && proxied !== '') {
      return proxied;
    }
  } catch {
    // The native module is missing (Jest, an unlinked build). Stream directly.
  }

  return uri;
}
