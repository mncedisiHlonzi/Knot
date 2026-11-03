//! Image derivatives: a 1200px AVIF display rendition and a 400px WebP
//! thumbnail.
//!
//! Decoding is done once and both renditions come from the same decoded picture,
//! so a photo is never parsed twice. The ladder never upscales: a source narrower
//! than a step is re-encoded at its own width, because the point of the pipeline is
//! fewer bytes, not more. Transparency is composited onto white first, since
//! neither target format is being asked to keep an alpha channel and letting one
//! drop it turns a transparent PNG's background black (KNOT-038).

use anyhow::{Context, Result};
use image::codecs::avif::AvifEncoder;
use image::imageops::FilterType;
use image::{DynamicImage, ExtendedColorType, ImageEncoder, Rgb, RgbImage};

use crate::keys::{
    AVIF_SPEED, DISPLAY_MAX_WIDTH, DISPLAY_QUALITY, THUMBNAIL_MAX_WIDTH, THUMBNAIL_QUALITY,
};

/// The two files one image produces.
#[derive(Debug)]
pub struct Derivatives {
    /// The AVIF the story view loads.
    pub display: Vec<u8>,
    /// The WebP the feed card loads.
    pub thumbnail: Vec<u8>,
    /// The display rendition's width, which is the source's unless it was capped.
    pub width: u32,
    /// The display rendition's height.
    pub height: u32,
}

/// Decodes `bytes` and encodes both renditions.
pub fn image_derivatives(bytes: &[u8]) -> Result<Derivatives> {
    let source = image::load_from_memory(bytes).context("decode uploaded image")?;
    let source = flatten_onto_white(&source);

    let display = scale_to_width(&source, DISPLAY_MAX_WIDTH);
    let thumbnail = scale_to_width(&source, THUMBNAIL_MAX_WIDTH);

    let width = display.width();
    let height = display.height();

    Ok(Derivatives {
        display: encode_avif(&display)?,
        thumbnail: encode_webp(&thumbnail)?,
        width,
        height,
    })
}

/// Composites a picture onto white.
///
/// It is always done, so an opaque photo and a transparent PNG follow exactly the
/// same path and the encoders never have to decide what "no background" means.
fn flatten_onto_white(source: &DynamicImage) -> DynamicImage {
    let rgba = source.to_rgba8();
    let (width, height) = rgba.dimensions();
    let mut flattened = RgbImage::new(width, height);

    for (x, y, pixel) in rgba.enumerate_pixels() {
        let alpha = f32::from(pixel[3]) / 255.0;
        let over = |channel: u8| -> u8 {
            (f32::from(channel) * alpha + 255.0 * (1.0 - alpha))
                .round()
                .clamp(0.0, 255.0) as u8
        };
        flattened.put_pixel(x, y, Rgb([over(pixel[0]), over(pixel[1]), over(pixel[2])]));
    }

    DynamicImage::ImageRgb8(flattened)
}

/// Shrinks a picture to `max_width`, preserving its aspect ratio, and returns it
/// untouched when it is already narrow enough.
fn scale_to_width(source: &DynamicImage, max_width: u32) -> DynamicImage {
    let width = source.width();
    if width <= max_width {
        return source.clone();
    }

    let height = (u64::from(source.height()) * u64::from(max_width) / u64::from(width)).max(1) as u32;
    source.resize_exact(max_width, height, FilterType::Lanczos3)
}

/// Encodes the display rendition as AVIF.
fn encode_avif(source: &DynamicImage) -> Result<Vec<u8>> {
    let rgb = source.to_rgb8();
    let mut encoded = Vec::new();

    AvifEncoder::new_with_speed_quality(&mut encoded, AVIF_SPEED, DISPLAY_QUALITY)
        .write_image(
            rgb.as_raw(),
            rgb.width(),
            rgb.height(),
            ExtendedColorType::Rgb8,
        )
        .context("encode display as avif")?;

    Ok(encoded)
}

/// Encodes the feed thumbnail as lossy WebP.
fn encode_webp(source: &DynamicImage) -> Result<Vec<u8>> {
    let rgba = source.to_rgba8();
    let encoder = webp::Encoder::from_rgba(rgba.as_raw(), rgba.width(), rgba.height());

    Ok(encoder.encode(THUMBNAIL_QUALITY).to_vec())
}

#[cfg(test)]
mod tests {
    use super::*;
    use image::{ImageFormat, Rgba, RgbaImage};
    use std::io::Cursor;

    /// A PNG with a smooth gradient and one transparent pixel, so the flattening
    /// path is exercised by every test that needs a picture.
    fn png(width: u32, height: u32) -> Vec<u8> {
        let mut picture = RgbaImage::new(width, height);
        for (x, y, pixel) in picture.enumerate_pixels_mut() {
            *pixel = Rgba([
                (x % 255) as u8,
                (y % 255) as u8,
                ((x + y) % 255) as u8,
                if x == 0 && y == 0 { 0 } else { 255 },
            ]);
        }

        let mut encoded = Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(picture)
            .write_to(&mut encoded, ImageFormat::Png)
            .expect("encode the fixture");
        encoded.into_inner()
    }

    /// The ladder's geometry is tested on its own, without encoding.
    ///
    /// AVIF encoding is by far the most expensive thing in this crate (a debug
    /// build of ravif costs seconds per megapixel), so exactly one test pays for
    /// it and every other one exercises the arithmetic that decides the sizes.
    /// The split is deliberate: a sizing bug is a pure-function bug.
    fn scaled_dimensions(width: u32, height: u32, max_width: u32) -> (u32, u32) {
        let source = DynamicImage::ImageRgb8(RgbImage::new(width, height));
        let scaled = scale_to_width(&source, max_width);
        (scaled.width(), scaled.height())
    }

    #[test]
    fn produces_an_avif_display_and_a_webp_thumbnail() {
        let derivatives = image_derivatives(&png(320, 200)).expect("the fixture encodes");

        // The AVIF brand sits in the ftyp box, after the box length.
        assert_eq!(&derivatives.display[4..8], b"ftyp");
        assert_eq!(&derivatives.display[8..12], b"avif");

        assert_eq!(&derivatives.thumbnail[0..4], b"RIFF");
        assert_eq!(&derivatives.thumbnail[8..12], b"WEBP");

        // A source narrower than the ladder is re-encoded at its own size.
        assert_eq!(derivatives.width, 320);
        assert_eq!(derivatives.height, 200);
        assert!(
            !derivatives.display.is_empty() && !derivatives.thumbnail.is_empty(),
            "both renditions carry bytes"
        );
    }

    #[test]
    fn never_upscales_a_narrow_source() {
        assert_eq!(scaled_dimensions(320, 200, DISPLAY_MAX_WIDTH), (320, 200));
        assert_eq!(scaled_dimensions(320, 200, THUMBNAIL_MAX_WIDTH), (320, 200));
    }

    #[test]
    fn caps_a_wide_source_by_width() {
        assert_eq!(scaled_dimensions(1600, 900, DISPLAY_MAX_WIDTH), (1200, 675));
        assert_eq!(
            scaled_dimensions(1600, 900, THUMBNAIL_MAX_WIDTH),
            (400, 225)
        );
    }

    #[test]
    fn caps_a_portrait_source_by_width() {
        assert_eq!(
            scaled_dimensions(1800, 3600, DISPLAY_MAX_WIDTH),
            (1200, 2400)
        );
    }

    #[test]
    fn a_square_source_keeps_its_shape() {
        assert_eq!(scaled_dimensions(800, 800, DISPLAY_MAX_WIDTH), (800, 800));
    }

    #[test]
    fn flattens_transparency_onto_white() {
        // Fully transparent, fully opaque black, and half-transparent black: the
        // three answers the blend has to get right.
        let mut picture = RgbaImage::new(3, 1);
        picture.put_pixel(0, 0, Rgba([0, 0, 0, 0]));
        picture.put_pixel(1, 0, Rgba([0, 0, 0, 255]));
        picture.put_pixel(2, 0, Rgba([0, 0, 0, 128]));

        let flattened = flatten_onto_white(&DynamicImage::ImageRgba8(picture));
        let rgb = flattened.to_rgb8();

        assert_eq!(rgb.get_pixel(0, 0), &Rgb([255, 255, 255]));
        assert_eq!(rgb.get_pixel(1, 0), &Rgb([0, 0, 0]));
        assert_eq!(rgb.get_pixel(2, 0), &Rgb([127, 127, 127]));
    }

    #[test]
    fn rejects_bytes_that_are_not_an_image() {
        assert!(image_derivatives(b"definitely not a png").is_err());
    }
}
