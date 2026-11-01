package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
	// The uploads allowlist accepts image/webp, and Go's image package has no
	// WebP decoder of its own. Registering this one is what lets a WebP upload
	// produce derivatives instead of quietly falling back to the original
	// (KNOT-037). It is decode-only: encoding to WebP is chai2010/webp's job
	// (KNOT-037-fix6).
	_ "golang.org/x/image/webp"
)

// The derivative ladder (KNOT-037-fix7).
//
// The pipeline stores DERIVATIVES ONLY: the upload itself is never written to the
// bucket, which is what keeps storage cost proportional to what a client actually
// downloads. An image becomes a 2048px AVIF for the story view and a 400px WebP
// for the feed card; a video becomes a 1080p H.264 MP4 whose index is at the
// front, so it starts playing before it has finished downloading. No cover image
// is generated for a video: it autoplays in the feed, so a still would be a second
// copy of the same content (KNOT-037-fix7).
const (
	// DisplayMaxWidth is the widest an image's display rendition may be. A source
	// narrower than this is re-encoded at its own size, never upscaled.
	DisplayMaxWidth = 2048
	// DisplayQuality is the AVIF quality of the display rendition.
	DisplayQuality = 80
	// ThumbnailMaxWidth is the widest a feed thumbnail may be: a feed card is at
	// most 400 logical points wide.
	ThumbnailMaxWidth = 400
	// ThumbnailQuality is the WebP quality of the feed thumbnail.
	ThumbnailQuality = 80
	// AVIFSpeed is libavif's encoder effort: 0 is slowest and smallest, 10 is
	// fastest and largest. Eight is the balance the thumbnails shipped with
	// (KNOT-037-fix6); a 2048px display takes a few seconds at it, which is why the
	// work happens in the background (KNOT-037-fix7).
	AVIFSpeed = 8
	// StreamMaxWidth is libavif's counterpart for video: the stream's WIDTH is
	// capped at 1080px (the brief's `scale='min(1080,iw)':-2`), so portrait video
	// keeps its own width and landscape is capped at 1080p.
	StreamMaxWidth = 1080
	// StreamCRF is x264's constant rate factor: lower is better and larger. 23 is
	// x264's own default, the point at which a transcode is visually transparent
	// for phone footage.
	StreamCRF = 23
	// StreamPreset trades encode time for bytes: `medium` is x264's default balance.
	StreamPreset = "medium"
	// StreamAudioBitrate is the AAC bitrate of the stream rendition.
	StreamAudioBitrate = "128k"
	// streamScaleFilter caps the width at StreamMaxWidth and lets FFmpeg derive an
	// even height (`-2`), which is what yuv420p requires.
	streamScaleFilter = "scale='min(1080,iw)':-2"
)

// The extensions and the suffixes are the object-key naming scheme. They live
// here so the key a row stores and the key a reader asks for cannot drift.
const (
	jpegExtension = ".jpg"
	avifExtension = ".avif"
	webpExtension = ".webp"
	mp4Extension  = ".mp4"
	movExtension  = ".mov"

	displaySuffix   = "_display"
	thumbnailSuffix = "_thumb"
	streamSuffix    = "_stream"
	// mediumSuffix and posterSuffix only ever appear in rows written before
	// KNOT-037-fix7; those files are still served, but nothing new is written with
	// them.
	mediumSuffix = "_medium"
	posterSuffix = "_poster"
)

// The content types a rendition can have. Serving one never echoes the row's
// stored MIME type: that describes the UPLOAD, and the object is a derivative of
// it (KNOT-037-fix7).
const (
	jpegContentType      = "image/jpeg"
	avifContentType      = "image/avif"
	webpContentType      = "image/webp"
	videoContentType     = "video/mp4"
	quickTimeContentType = "video/quicktime"
)

// ffmpegBinary is the executable used for video transcoding. It is a name, not a
// path: FFmpeg is a system prerequisite and its location is whatever PATH says
// (KNOT-037, docs/DEVELOPMENT.md).
const ffmpegBinary = "ffmpeg"

// ErrFFmpegUnavailable is returned when the system has no FFmpeg. It is distinct
// from a failed transcode so the log line can say which problem occurred: a
// missing prerequisite is an operator's job, a malformed video is not.
var ErrFFmpegUnavailable = errors.New("storymedia: ffmpeg is not installed")

// Rendition is one generated derivative: its variant, the bytes to store, and the
// object-key suffix that names it.
//
// It carries the bytes rather than a path because every image rendition is small
// enough to hold in memory, and the caller uploads it immediately.
type Rendition struct {
	// Variant is which derivative this is: VariantThumb or VariantMedium.
	Variant Variant
	// Format is the encoding, which is what tells one thumbnail file from the
	// other: VariantThumb exists once as FormatAVIF and once as FormatWebP.
	Format Format
	// Suffix is appended to the object's base name, e.g. "_thumb".
	Suffix string
	// Extension is the object key's extension, e.g. ".avif".
	Extension string
	// ContentType is the stored content type: image/avif, image/webp, or
	// image/jpeg, matching Format.
	ContentType string
	// Body is the encoded rendition.
	Body []byte
	// Width and Height are the rendition's pixel dimensions.
	Width  int
	Height int
}

// ImageDerivatives decodes an uploaded image and returns its display rendition
// (AVIF, at most DisplayMaxWidth) and its feed thumbnail (WebP, at most
// ThumbnailMaxWidth), in that order.
//
// Decoding applies EXIF orientation, because a phone photo is usually stored
// sideways with a rotation flag, and a rendition that ignores the flag is a
// sideways picture. A source narrower than a rendition's max width is re-encoded
// at its own size rather than upscaled: the point of the pipeline is fewer bytes,
// not more.
//
// Transparency is composited onto white before encoding. Neither encoder is asked
// to keep alpha, and letting one drop it turns a transparent PNG's background
// black.
//
// The renditions that DID encode are returned together with the error from any that
// did not, so one codec failing does not hide the other's result.
func ImageDerivatives(data []byte) ([]Rendition, error) {
	source, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	renditions := make([]Rendition, 0, 2)
	var failures []error

	display, err := encodeDisplay(flattenOntoWhite(scaleToMaxWidth(source, DisplayMaxWidth)))
	if err != nil {
		failures = append(failures, err)
	} else {
		renditions = append(renditions, display)
	}

	thumbnail, err := encodeThumbnail(flattenOntoWhite(scaleToMaxWidth(source, ThumbnailMaxWidth)))
	if err != nil {
		failures = append(failures, err)
	} else {
		renditions = append(renditions, thumbnail)
	}

	return renditions, errors.Join(failures...)
}

// flattenOntoWhite composites an image onto a white background.
//
// It is always done: the image is small by now, and doing it unconditionally means
// an opaque photo and a transparent PNG follow exactly the same path.
func flattenOntoWhite(source image.Image) image.Image {
	return imaging.Overlay(
		imaging.New(source.Bounds().Dx(), source.Bounds().Dy(), color.White),
		source,
		image.Point{},
		1.0,
	)
}

// encodeDisplay encodes the full story-view rendition as AVIF.
func encodeDisplay(source image.Image) (Rendition, error) {
	var buffer bytes.Buffer
	err := avif.Encode(&buffer, source, avif.Options{
		Quality: DisplayQuality,
		// The picture is opaque by now, so the alpha quality only has to be a
		// valid number; passing the picture quality keeps it from silently
		// falling back to the library default.
		QualityAlpha: DisplayQuality,
		Speed:        AVIFSpeed,
		// 4:2:0 is the chroma layout a photograph wants; the zero value of the
		// option would select 4:4:4 and cost bytes for detail nobody sees.
		ChromaSubsampling: image.YCbCrSubsampleRatio420,
	})
	if err != nil {
		return Rendition{}, fmt.Errorf("encode %s %s: %w", VariantDisplay, FormatAVIF, err)
	}

	return newRendition(VariantDisplay, FormatAVIF, buffer.Bytes(), source), nil
}

// encodeThumbnail encodes the feed thumbnail as WebP.
func encodeThumbnail(source image.Image) (Rendition, error) {
	var buffer bytes.Buffer
	err := webp.Encode(&buffer, source, &webp.Options{Quality: ThumbnailQuality})
	if err != nil {
		return Rendition{}, fmt.Errorf("encode %s %s: %w", VariantThumb, FormatWebP, err)
	}

	return newRendition(VariantThumb, FormatWebP, buffer.Bytes(), source), nil
}

// newRendition fills in the naming and content type that belong to a variant and
// format, so the key a row stores and the type it is served as cannot disagree.
func newRendition(variant Variant, format Format, body []byte, source image.Image) Rendition {
	return Rendition{
		Variant:     variant,
		Format:      format,
		Suffix:      suffixForVariant(variant),
		Extension:   extensionForFormat(format),
		ContentType: contentTypeForFormat(format),
		Body:        body,
		Width:       source.Bounds().Dx(),
		Height:      source.Bounds().Dy(),
	}
}

// suffixForVariant returns the object-key suffix a variant is named with.
func suffixForVariant(variant Variant) string {
	switch variant {
	case VariantDisplay:
		return displaySuffix
	case VariantStream:
		return streamSuffix
	default:
		return thumbnailSuffix
	}
}

// extensionForFormat returns the object-key extension a format is stored with.
func extensionForFormat(format Format) string {
	switch format {
	case FormatAVIF:
		return avifExtension
	case FormatWebP:
		return webpExtension
	default:
		return jpegExtension
	}
}

// contentTypeForFormat returns the content type a format is served as.
func contentTypeForFormat(format Format) string {
	switch format {
	case FormatAVIF:
		return avifContentType
	case FormatWebP:
		return webpContentType
	default:
		return jpegContentType
	}
}

// scaleToMaxWidth shrinks an image to maxWidth, preserving its aspect ratio, and
// returns the source untouched when it is already narrow enough.
func scaleToMaxWidth(source image.Image, maxWidth int) image.Image {
	width := source.Bounds().Dx()
	if width <= maxWidth {
		return source
	}
	return imaging.Resize(source, maxWidth, 0, imaging.Lanczos)
}

// TranscodeToStream writes the web-playable stream rendition of a video file.
//
// The output is H.264 + AAC in an MP4 whose index (`moov`) is written FIRST
// (`-movflags +faststart`). That single flag is what makes the feed feel instant:
// a player can start on the first bytes it receives instead of downloading the
// whole file to find out where the frames are (KNOT-037-fix7).
//
// FFmpeg is a system prerequisite, not a Go dependency. When it is absent the error
// is ErrFFmpegUnavailable, so a missing prerequisite reads differently in the log
// from a video FFmpeg refused.
func TranscodeToStream(ctx context.Context, inputPath, outputPath string) error {
	binary, err := exec.LookPath(ffmpegBinary)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFFmpegUnavailable, err)
	}

	arguments := []string{
		"-y",
		"-i", inputPath,
		// Cap the width at 1080 and let FFmpeg derive an even height.
		"-vf", streamScaleFilter,
		"-c:v", "libx264",
		"-crf", strconv.Itoa(StreamCRF),
		"-preset", StreamPreset,
		"-c:a", "aac",
		"-b:a", StreamAudioBitrate,
		// yuv420p is the pixel format every browser and hardware decoder accepts.
		"-pix_fmt", "yuv420p",
		// The index goes at the front of the file.
		"-movflags", "+faststart",
		outputPath,
	}

	command := exec.CommandContext(ctx, binary, arguments...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("transcode stream: ffmpeg: %w: %s", err, lastLine(stderr.String()))
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("transcode stream: read output: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("transcode stream: ffmpeg produced no output")
	}

	return nil
}

// lastLine returns the last non-empty line of FFmpeg's stderr, which is where the
// actual reason for a failure sits; the rest is banner and progress noise.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}

	return ""
}
