//! Fixtures shared by the crate's tests.
//!
//! They are generated rather than committed: a generated PNG and a one-second
//! synthetic clip keep the repository free of binary fixtures, and the video only
//! exists where FFmpeg does (the tests that need it skip politely otherwise).

use std::fs;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use image::{DynamicImage, ImageFormat, Rgba, RgbaImage};

/// The story id every test job names.
pub const STORY_ID: &str = "55555555-5555-4555-8555-555555555555";

/// The media row id every test job names.
pub const MEDIA_ID: &str = "00000000-0000-4000-8000-000000000001";

/// Encodes a PNG with a gradient and one transparent pixel.
pub fn png(width: u32, height: u32) -> Vec<u8> {
    let mut picture = RgbaImage::new(width, height);
    for (x, y, pixel) in picture.enumerate_pixels_mut() {
        *pixel = Rgba([
            (x % 255) as u8,
            (y % 255) as u8,
            ((x + y) % 255) as u8,
            if x == 0 && y == 0 { 0 } else { 255 },
        ]);
    }

    let mut encoded = std::io::Cursor::new(Vec::new());
    DynamicImage::ImageRgba8(picture)
        .write_to(&mut encoded, ImageFormat::Png)
        .expect("the PNG fixture encodes");

    encoded.into_inner()
}

/// Encodes one second of synthetic H.264 video.
pub fn mp4() -> Vec<u8> {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("the clock reads")
        .as_nanos();
    let path: PathBuf = std::env::temp_dir().join(format!(
        "knot-fixture-{}-{nonce}.mp4",
        std::process::id()
    ));

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
    assert!(status.success(), "the MP4 fixture encodes");

    let bytes = fs::read(&path).expect("read the MP4 fixture");
    let _ = fs::remove_file(&path);

    bytes
}
