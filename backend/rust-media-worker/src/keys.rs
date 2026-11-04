//! The cross-process contract: Redis keys, object-key naming, and the derivative
//! ladder.
//!
//! These constants are the worker's half of an agreement with the Go API
//! (`internal/storymedia/jobs.go` and `internal/storymedia/keys.go`). They are
//! spelled out here rather than shared through a schema because the two processes
//! are written in different languages; the tests in `job.rs` pin the derived keys,
//! so a change on either side is visible rather than silent (KNOT-038).

/// The waiting line. The API appends here; a worker blocks here.
pub const JOBS: &str = "knot:media:jobs";

/// Jobs a worker has claimed but not finished. A job left here is recoverable,
/// which is the whole point of claiming with `BLMOVE` rather than popping.
pub const PROCESSING: &str = "knot:media:processing";

/// Finished jobs, kept for auditing and for a drain check.
pub const COMPLETED: &str = "knot:media:completed";

/// Jobs that exhausted their attempts.
pub const FAILED: &str = "knot:media:failed";

/// Hash of `media id -> unix seconds` for the jobs currently claimed. It exists
/// because a Redis list cannot carry a timestamp: the payload is immutable once
/// queued, so "when was this claimed" has to live beside it. The reclaimer reads
/// it to tell a job that is still running from one whose worker died.
pub const CLAIMS: &str = "knot:media:claimed_at";

/// Prefix of the per-item attempt counter. One key per item rather than one hash
/// keeps `INCR` atomic without a second round trip; it is deleted on success so
/// the keyspace does not grow with every upload.
pub const ATTEMPTS_PREFIX: &str = "knot:media:attempts:";

/// Names the uploaded file, which is the only object the request path writes.
pub const ORIGINAL_SUFFIX: &str = "_original";

/// Names an image's full rendition.
///
/// It exists in TWO codecs beside each other: `<uuid>_display.avif` (the primary)
/// and `<uuid>_display.webp` (the same 1200px picture, for devices that cannot
/// decode AVIF — Android below 12 and iOS below 16; KNOT-038-fix3).
pub const DISPLAY_SUFFIX: &str = "_display";

/// Names the feed thumbnail.
pub const THUMBNAIL_SUFFIX: &str = "_thumb";

/// Names a video's full rendition.
pub const STREAM_SUFFIX: &str = "_stream";

/// The bucket prefix every story's media lives under. Every key is namespaced by
/// the story id, so a key can always be traced back to exactly one story.
pub const STORY_MEDIA_NAMESPACE: &str = "story-media/";

pub const AVIF_EXTENSION: &str = ".avif";
pub const WEBP_EXTENSION: &str = ".webp";
pub const MP4_EXTENSION: &str = ".mp4";

pub const AVIF_CONTENT_TYPE: &str = "image/avif";
pub const WEBP_CONTENT_TYPE: &str = "image/webp";
pub const VIDEO_CONTENT_TYPE: &str = "video/mp4";

/// The widest an image's display rendition may be. A narrower source is re-encoded
/// at its own size, never upscaled: the pipeline exists to send fewer bytes.
pub const DISPLAY_MAX_WIDTH: u32 = 1200;

/// The AVIF quality of the display rendition.
pub const DISPLAY_QUALITY: u8 = 75;

/// The widest a feed thumbnail may be. A feed card is at most 400 logical points.
pub const THUMBNAIL_MAX_WIDTH: u32 = 400;

/// The WebP quality of the feed thumbnail.
pub const THUMBNAIL_QUALITY: f32 = 80.0;

/// libavif's encoder effort: 0 is slowest and smallest, 10 is fastest and largest.
/// Eight is the balance the Go pipeline shipped, and it keeps a 1200px encode in
/// the low seconds on one core.
pub const AVIF_SPEED: u8 = 8;

/// x264's constant rate factor: 23 is its default and the point at which a
/// transcode is visually transparent for phone footage.
pub const STREAM_CRF: u8 = 23;

/// x264's speed/compression balance.
pub const STREAM_PRESET: &str = "medium";

/// The AAC bitrate of the stream rendition.
pub const STREAM_AUDIO_BITRATE: &str = "128k";

/// Caps the width at 1080 and lets FFmpeg derive an even height (`-2`), which is
/// what `yuv420p` requires. Portrait video keeps its own width.
pub const STREAM_SCALE_FILTER: &str = "scale='min(1080,iw)':-2";
