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

/// Names a video's poster frame: the still a card draws before (or instead of)
/// the first decoded frame, so a feed never opens on black (KNOT-042).
pub const POSTER_SUFFIX: &str = "_poster";

/// Names the low-quality image placeholder: a ~20px still that arrives in a
/// kilobyte, for the moment before even the poster or the thumbnail exists
/// (KNOT-043).
pub const LQIP_SUFFIX: &str = "_lqip";

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

/// Where in the clip the poster frame is taken from, in seconds.
///
/// One second in rather than the very first frame: a clip routinely opens on a
/// fade or a black leader, and a poster is the card's only picture, so the frame
/// has to be something worth showing. A clip shorter than a second falls back to
/// its first frame (KNOT-042).
pub const POSTER_OFFSET_SECONDS: f64 = 1.0;

/// The widest a poster may be. A story-detail frame is at most a phone's width,
/// and a feed card is narrower still, so 800px is sharp on a 3x screen and small
/// enough to arrive in one round trip.
pub const POSTER_MAX_WIDTH: u32 = 800;

/// The WebP quality of the poster. The same 80 the thumbnails use: a poster is
/// shown large, so it cannot go lower, and it is shown instantly, so it cannot go
/// much higher (KNOT-042).
pub const POSTER_QUALITY: f32 = 80.0;

/// The widest a low-quality placeholder may be.
///
/// Twenty pixels is deliberately far below anything that reads as a photograph:
/// the picture is only ever shown stretched and blurred, and at this width the
/// whole frame costs about a kilobyte, which is the point — it has to arrive
/// before the eye notices the gap it fills (KNOT-043).
pub const LQIP_MAX_WIDTH: u32 = 20;

/// The WebP quality of the placeholder. Forty is enough for a blurred swatch of
/// colour and keeps the file near a kilobyte; a higher quality at 20px is
/// indistinguishable and simply costs bytes (KNOT-043).
pub const LQIP_QUALITY: f32 = 40.0;
