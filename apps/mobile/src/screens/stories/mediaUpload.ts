/**
 * The words a card shows while the composer's media is being uploaded to
 * storage (KNOT-040).
 *
 * Publishing is optimistic: the story is created first and the bytes follow, so
 * the composer is gone before the uploads run. What the reader sees instead is the
 * new card at the top of the feed, and this module is what decides what that card
 * says — "Uploading media 1 of 2...", a failure, or nothing at all once the server
 * has the bytes and its own "processing" state takes over.
 *
 * It is a pure function with no React and no I/O, so the wording is pinned by a
 * unit test rather than by reading the screen.
 */

/** One upload run's position. */
export type MediaUploadProgress = {
  /** How many items have finished uploading (successfully or not). */
  readonly completed: number;
  /** How many items the publish started with. */
  readonly total: number;
  /** True when at least one item could not be uploaded. */
  readonly failed: boolean;
};

/**
 * The label a card shows for an upload in flight, or undefined when there is
 * nothing useful to say.
 *
 * Nothing is said when the run is finished: from that point the server is
 * encoding, and the card's own "Processing media..." placeholder is the honest
 * description. A failure is reported once and stays reported, because the reader
 * needs to know the pictures are not coming.
 */
export function mediaUploadLabel(progress: MediaUploadProgress | undefined): string | undefined {
  if (progress === undefined || progress.total <= 0) {
    return undefined;
  }
  if (progress.failed) {
    return 'Media upload failed';
  }
  if (progress.completed >= progress.total) {
    return undefined;
  }

  // 1-based: "1 of 2" while the first file is going up.
  return `Uploading media ${progress.completed + 1} of ${progress.total}...`;
}
