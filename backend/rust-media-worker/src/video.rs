//! Video derivatives: one H.264 + AAC MP4 whose index is at the front.
//!
//! `-movflags +faststart` is the whole point: it moves the `moov` box to the start
//! of the file, so a player can begin on the first bytes it receives instead of
//! downloading the file to find out where the frames are. Everything else is the
//! ladder the Go pipeline used — 1080 wide at most, CRF 23, `yuv420p` for hardware
//! decoders — because the worker replaces the encoder, not the format
//! (KNOT-038).

use std::fs;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use anyhow::{bail, Context, Result};
use ffmpeg_sidecar::command::FfmpegCommand;

use crate::keys::{
    STREAM_AUDIO_BITRATE, STREAM_CRF, STREAM_PRESET, STREAM_SCALE_FILTER,
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
}
