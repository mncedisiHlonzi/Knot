import { useCallback, useState } from 'react';

import { resolveMediaAspectRatio } from './mediaFrame';

/** A media's natural pixel size, as its on-load event reports it. */
type MediaSize = {
  readonly width: number;
  readonly height: number;
};

/** What a frame needs to draw media at its natural shape. */
export type MediaAspectRatio = {
  /**
   * The ratio to frame the media with, or `null` when neither the payload nor the
   * loaded media has reported a shape yet. A caller draws a placeholder frame
   * while it is `null`.
   */
  readonly aspectRatio: number | null;
  /**
   * Records the media's natural size. Wire it to the media's own load event —
   * `Image`'s `onLoad` or `FeedVideo`'s pass-through — and the frame re-renders
   * at the media's real shape.
   */
  readonly onLoadSize: (width: number, height: number) => void;
};

/**
 * The aspect ratio for a list media frame, settled from the payload or the media
 * itself.
 *
 * The payload is the preferred source: when the server reported the item's pixel
 * dimensions the ratio is known before a byte is fetched, so the frame is right
 * on the first paint and never settles. Media the payload cannot describe — a
 * legacy row, or the videos whose picker reported no size — resolves from the
 * natural size the media reports once it has loaded, which is what `onLoadSize`
 * records.
 *
 * The state is deliberately the *loaded size* rather than the resolved ratio: a
 * later render whose payload has a shape still prefers the payload, and describing
 * the media by its own size keeps that decision in one place
 * (`resolveMediaAspectRatio`), where it can be tested without a renderer.
 *
 * It lives beside `mediaFrame.ts` rather than inside it so that module stays free
 * of every import, as its own documentation promises (KNOT-020g-fix7).
 */
export function useMediaAspectRatio(
  payloadWidth?: number | null,
  payloadHeight?: number | null,
): MediaAspectRatio {
  const [loaded, setLoaded] = useState<MediaSize | null>(null);

  const onLoadSize = useCallback((width: number, height: number): void => {
    setLoaded({ width, height });
  }, []);

  return {
    aspectRatio: resolveMediaAspectRatio(
      payloadWidth,
      payloadHeight,
      loaded?.width,
      loaded?.height,
    ),
    onLoadSize,
  };
}
