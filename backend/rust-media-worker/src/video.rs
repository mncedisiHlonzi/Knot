//! Video derivatives: one H.264 + AAC MP4 whose index is at the front, and the
//! still a card draws before the first frame arrives.
//!
//! `-movflags +faststart` is the whole point of the stream: it moves the `moov`
//! box to the start of the file, so a player can begin on the first bytes it
//! receives instead of downloading the file to find out where the frames are.
//! Everything else is the ladder the Go pipeline used — 1080 wide at most, CRF
//! 28, `yuv420p` for hardware decoders — because the worker replaces the encoder,
//! not the format (KNOT-038). The rate factor was 23 until KNOT-048-fix: at 23 a
//! half-minute phone clip streamed as several megabytes, which a mobile
//! connection could not deliver before the viewer gave up.
//!
//! The poster exists because a player spends its first moments with nothing on
//! screen: the surface is black until a frame decodes. A still taken one second
//! in, encoded as WebP, gives the feed and the detail carousel something to draw
//! in that gap (KNOT-042).

use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{SystemTime, UNIX_EPOCH};

use anyhow::{bail, Context, Result};
use ffmpeg_sidecar::command::FfmpegCommand;

use crate::images::{encode_webp, scale_to_width};
use crate::keys::{
    POSTER_MAX_WIDTH, POSTER_OFFSET_SECONDS, POSTER_QUALITY, STREAM_AUDIO_BITRATE, STREAM_CRF,
    STREAM_PRESET, STREAM_SCALE_FILTER,
};

/// Reports whether an FFmpeg binary can be executed.
///
/// It is checked once at startup so an operator sees the missing prerequisite in
/// the log at boot rather than as every video upload failing later.
pub fn ffmpeg_is_available() -> bool {
    Command::new(ffmpeg_sidecar::paths::ffmpeg_path())
        .arg("-version")
        .output()
        .map(|output| output.status.success())
        .unwrap_or(false)
}

/// Transcodes `input` into the web-playable stream rendition.
///
/// The work goes through a temporary directory because FFmpeg needs real files for
/// both ends: it seeks within the input and writes a `moov` box it can only place
/// once it has finished. Streaming through pipes would lose the seek and the
/// faststart rewrite, which are the two properties this rendition exists for.
pub fn transcode_to_stream(input: &[u8]) -> Result<Vec<u8>> {
    let workspace = Workspace::new()?;
    let source = workspace.path.join("input");
    let output = workspace.path.join("output.mp4");

    fs::write(&source, input).context("write the upload to a temporary file")?;

    let arguments = vec![
        "-y".to_string(),
        "-loglevel".to_string(),
        "error".to_string(),
        "-i".to_string(),
        source.to_string_lossy().into_owned(),
        // Cap the width and let FFmpeg derive an even height.
        "-vf".to_string(),
        STREAM_SCALE_FILTER.to_string(),
        "-c:v".to_string(),
        "libx264".to_string(),
        "-crf".to_string(),
        STREAM_CRF.to_string(),
        "-preset".to_string(),
        STREAM_PRESET.to_string(),
        "-c:a".to_string(),
        "aac".to_string(),
        "-b:a".to_string(),
        STREAM_AUDIO_BITRATE.to_string(),
        // The pixel format every browser and hardware decoder accepts.
        "-pix_fmt".to_string(),
        "yuv420p".to_string(),
        // The index goes at the front of the file.
        "-movflags".to_string(),
        "+faststart".to_string(),
        output.to_string_lossy().into_owned(),
    ];

    let mut child = FfmpegCommand::new()
        .args(arguments)
        .spawn()
        .context("start ffmpeg (is it installed and on PATH?)")?;
    let status = child.wait().context("wait for ffmpeg")?;
    if !status.success() {
        bail!("ffmpeg failed with {status}");
    }

    let encoded = fs::read(&output).context("read the transcoded stream")?;
    if encoded.is_empty() {
        bail!("ffmpeg produced no output");
    }

    Ok(encoded)
}

/// Extracts one still frame from `input` and encodes it as a WebP poster.
///
/// The frame is taken `POSTER_OFFSET_SECONDS` into the clip, because a clip
/// usually opens on black or a fade and the poster is the only picture a card
/// shows until playback starts. A clip shorter than the offset — or one whose
/// seek lands past its last frame — falls back to its very first frame, so a
/// still is produced for every decodable video (KNOT-042).
///
/// The frame is scaled to at most `POSTER_MAX_WIDTH`, never upscaled, and encoded
/// with the image pipeline's own helper so the two paths cannot drift into
/// different codecs or different scaling rules.
pub fn extract_poster(input: &[u8]) -> Result<Vec<u8>> {
    let workspace = Workspace::new()?;
    let source = workspace.path.join("input");
    let frame = workspace.path.join("poster.png");

    fs::write(&source, input).context("write the upload to a temporary file")?;

    // The offset first (the frame worth showing), the first frame second (the
    // frame that always exists).
    let mut extracted = false;
    for offset in [POSTER_OFFSET_SECONDS, 0.0] {
        if extract_frame_file(&source, &frame, offset).is_ok() {
            extracted = true;
            break;
        }
    }

    if !extracted {
        bail!("ffmpeg produced no poster frame");
    }

    let bytes = fs::read(&frame).context("read the poster frame")?;
    let decoded = image::load_from_memory(&bytes).context("decode the poster frame")?;
    let poster_frame = scale_to_width(&decoded, POSTER_MAX_WIDTH);

    encode_webp(&poster_frame, POSTER_QUALITY).context("encode the poster as webp")
}

/// Runs FFmpeg to write one frame at `offset` seconds next to the upload, and
/// fails when no frame came out.
///
/// `-nostdin` and a null stdin are both set on purpose: without them FFmpeg
/// attaches to the worker's own terminal, and a background service can then block
/// forever on a frame extraction that is otherwise finished. `FfmpegCommand` above
/// handles this for the transcode; a plain `Command` does not (KNOT-042).
fn extract_frame_file(source: &Path, frame: &Path, offset: f64) -> Result<()> {
    let arguments = vec![
        "-y".to_string(),
        "-nostdin".to_string(),
        "-loglevel".to_string(),
        "error".to_string(),
        "-ss".to_string(),
        offset.to_string(),
        "-i".to_string(),
        source.to_string_lossy().into_owned(),
        "-frames:v".to_string(),
        "1".to_string(),
        "-f".to_string(),
        "image2".to_string(),
        frame.to_string_lossy().into_owned(),
    ];

    let status = Command::new(ffmpeg_sidecar::paths::ffmpeg_path())
        .args(arguments)
        .stdin(Stdio::null())
        .status()
        .context("start ffmpeg to extract a poster frame")?;

    if !status.success() || !file_has_bytes(frame) {
        let _ = fs::remove_file(frame);
        bail!("ffmpeg wrote no poster frame at {offset}s");
    }

    Ok(())
}

/// Reports whether a path holds a non-empty file.
fn file_has_bytes(path: &Path) -> bool {
    fs::metadata(path)
        .map(|metadata| metadata.is_file() && metadata.len() > 0)
        .unwrap_or(false)
}

/// A temporary directory that removes itself, even when a step fails.
struct Workspace {
    path: PathBuf,
}

impl Workspace {
    fn new() -> Result<Self> {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .context("read the clock")?
            .as_nanos();
        let path = std::env::temp_dir().join(format!(
            "knot-media-{}-{nonce}",
            std::process::id()
        ));

        fs::create_dir_all(&path).context("create a temporary directory")?;

        Ok(Self { path })
    }
}

impl Drop for Workspace {
    fn drop(&mut self) {
        // A leftover directory is housekeeping, not a job failure.
        let _ = fs::remove_dir_all(&self.path);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The rate factor `transcode_to_stream` hands to `-crf`.
    ///
    /// It is the one number that decides the rendition's size, so it is pinned:
    /// 23 produced multi-megabyte streams that a mobile connection could not
    /// deliver on time, and drifting back would silently restore that
    /// (KNOT-048-fix).
    #[test]
    fn streams_at_rate_factor_28() {
        assert_eq!(STREAM_CRF, 28);
    }

    /// Builds one second of synthetic video, so the test needs no fixture file.
    fn fixture() -> Vec<u8> {
        let workspace = Workspace::new().expect("a temporary directory");
        let path = workspace.path.join("fixture.mp4");

        let status = Command::new(ffmpeg_sidecar::paths::ffmpeg_path())
            .args([
                "-y",
                "-loglevel",
                "error",
                "-f",
                "lavfi",
                "-i",
                "testsrc=size=320x240:rate=10",
                "-t",
                "1",
                "-pix_fmt",
                "yuv420p",
                "-c:v",
                "libx264",
            ])
            .arg(&path)
            .status()
            .expect("run ffmpeg for the fixture");
        assert!(status.success(), "the fixture encodes");

        fs::read(&path).expect("read the fixture")
    }

    /// Finds the first occurrence of `needle` in `haystack`.
    fn find(haystack: &[u8], needle: &[u8]) -> Option<usize> {
        haystack
            .windows(needle.len())
            .position(|window| window == needle)
    }

    #[test]
    fn transcodes_a_clip_and_moves_the_index_to_the_front() {
        if !ffmpeg_is_available() {
            eprintln!("skipping: ffmpeg is not on PATH");
            return;
        }

        let encoded = transcode_to_stream(&fixture()).expect("the fixture transcodes");

        assert_eq!(&encoded[4..8], b"ftyp", "the output is an MP4");
        let moov = find(&encoded, b"moov").expect("the output has an index");
        let mdat = find(&encoded, b"mdat").expect("the output has media data");
        assert!(
            moov < mdat,
            "+faststart must place moov ({moov}) before mdat ({mdat})"
        );
    }

    #[test]
    fn rejects_bytes_that_are_not_a_video() {
        if !ffmpeg_is_available() {
            eprintln!("skipping: ffmpeg is not on PATH");
            return;
        }

        assert!(transcode_to_stream(b"not a video").is_err());
    }

    #[test]
    fn extracts_a_webp_poster_from_a_clip() {
        if !ffmpeg_is_available() {
            eprintln!("skipping: ffmpeg is not on PATH");
            return;
        }

        let poster = extract_poster(&fixture()).expect("the fixture yields a poster");

        // RIFF....WEBP
        assert_eq!(&poster[0..4], b"RIFF");
        assert_eq!(&poster[8..12], b"WEBP");

        let decoded = image::load_from_memory(&poster).expect("the poster decodes");
        // The fixture is 320x240, so the ladder re-encodes at its own width: a
        // poster is never upscaled (KNOT-042).
        assert_eq!(decoded.width(), 320);
        assert_eq!(decoded.height(), 240);
        assert!(decoded.width() <= POSTER_MAX_WIDTH);
    }

    #[test]
    fn caps_a_wide_frame_at_the_poster_width() {
        // The scaling rule is arithmetic, so it is checked without FFmpeg: a
        // 1920px frame is capped at POSTER_MAX_WIDTH and keeps its shape.
        let source = image::DynamicImage::ImageRgb8(image::RgbImage::new(1920, 1080));
        let scaled = scale_to_width(&source, POSTER_MAX_WIDTH);

        assert_eq!(scaled.width(), POSTER_MAX_WIDTH);
        assert_eq!(scaled.height(), 450);
    }

    #[test]
    fn a_poster_needs_a_decodable_video() {
        if !ffmpeg_is_available() {
            eprintln!("skipping: ffmpeg is not on PATH");
            return;
        }

        assert!(extract_poster(b"not a video").is_err());
    }
}
