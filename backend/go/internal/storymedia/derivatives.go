package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
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

// The derivative ladder (KNOT-037, KNOT-ADR-091; dual-format thumbnails
// KNOT-037-fix6).
//
// The numbers are the product decision, not a tuning knob: a feed card is at most
// 400 logical points wide on the widest phone, so an 800px thumbnail is comfortably
// retina, and the story detail is a full-width column at roughly 1200px on a
// tablet. The feed thumbnail is written TWICE — AVIF first for its bytes, WebP as
// the fallback for a device with no AVIF decoder — while the medium rendition and
// the video poster stay JPEG, where the extra bytes buy nothing.
const (
	// ThumbnailMaxWidth is the widest a feed thumbnail may be. A narrower source
	// is never upscaled.
	ThumbnailMaxWidth = 800
	// ThumbnailQuality is the quality a feed thumbnail is encoded at, for BOTH
	// AVIF and WebP. It is the same number in both so the two files differ by
	// codec, not by a second tuning decision.
	ThumbnailQuality = 80
	// AVIFSpeed is libavif's encoder effort: 0 is slowest and smallest, 10 is
	// fastest and largest. Eight keeps an 800px thumbnail well ahead of the JPEG
	// it replaces at a cost of a second or two of CPU per upload; at ten the same
	// image can come out LARGER than that JPEG.
	AVIFSpeed = 8
	// MediumMaxWidth is the widest the story-detail rendition may be.
	MediumMaxWidth = 1200
	// MediumQuality is the JPEG quality of the story-detail rendition.
	MediumQuality = 85
	// PosterMaxWidth is the widest a video poster frame may be.
	PosterMaxWidth = 400
	// PosterQuality is the quality the poster frame is written with. FFmpeg is
	// asked for `-q:v 3`, which is roughly this, and the value is named so the
	// figure is not a bare literal in a command line.
	PosterQuality = 85
	// posterOffsetRatio is how far into a video the poster frame is taken. A
	// frame at zero is often a black fade or a title card, so the still is taken
	// a fifth of the way in.
	posterOffsetRatio = 0.2
	// unknownDurationOffsetMS is where the frame is taken when the client did not
	// report the video's duration. Zero is the only offset that is guaranteed to
	// exist in any video.
	unknownDurationOffsetMS = 0
)

// The extensions and the suffixes are the object-key naming scheme. They live
// here so the key a row stores and the key a reader asks for cannot drift.
const (
	jpegExtension = ".jpg"
	avifExtension = ".avif"
	webpExtension = ".webp"

	thumbnailSuffix = "_thumb"
	mediumSuffix    = "_medium"
	posterSuffix    = "_poster"
)

// The content types a rendition can have. Serving one never echoes the row's
// stored MIME type: a derivative's type is whatever its encoder produced.
const (
	jpegContentType = "image/jpeg"
	avifContentType = "image/avif"
	webpContentType = "image/webp"
)

// ffmpegBinary is the executable used for poster frames. It is a name, not a
// path: FFmpeg is a system prerequisite and its location is whatever PATH says
// (KNOT-037, docs/DEVELOPMENT.md).
const ffmpegBinary = "ffmpeg"

// ErrPosterUnavailable is returned when the system has no FFmpeg. It is distinct
// from a failed extraction so the log line can say which problem occurred: a
// missing prerequisite is an operator's job, a malformed video is not.
var ErrPosterUnavailable = errors.New("storymedia: ffmpeg is not installed")

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

// ImageDerivatives decodes an uploaded image and returns its feed thumbnail in
// both modern formats followed by its JPEG medium rendition: AVIF first, then
// WebP, then JPEG.
//
// Decoding applies EXIF orientation, because a phone photo is usually stored
// sideways with a rotation flag, and a thumbnail that ignores the flag is a
// thumbnail of a sideways picture. A source narrower than a rendition's max width
// is re-encoded at its own size rather than upscaled: the point of the pipeline is
// fewer bytes, not more.
//
// Transparency is composited onto white before encoding. None of these encoders is
// asked to keep alpha, and letting one drop it turns a transparent PNG's background
// black.
//
// The renditions that DID encode are returned together with the error from any that
// did not. One codec failing must not cost the other: an AVIF encoder that refuses
// a picture still leaves a WebP thumbnail to serve.
func ImageDerivatives(data []byte) ([]Rendition, error) {
	source, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	renditions := make([]Rendition, 0, 3)
	var failures []error

	// Both thumbnails come from ONE scaled copy, so AVIF and WebP are guaranteed to
	// be the same picture and only the codec differs.
	thumbnailSource := flattenOntoWhite(scaleToMaxWidth(source, ThumbnailMaxWidth))

	avifThumbnail, err := encodeAVIFThumbnail(thumbnailSource)
	if err != nil {
		failures = append(failures, err)
	} else {
		renditions = append(renditions, avifThumbnail)
	}

	webpThumbnail, err := encodeWebPThumbnail(thumbnailSource)
	if err != nil {
		failures = append(failures, err)
	} else {
		renditions = append(renditions, webpThumbnail)
	}

	medium, err := encodeJPEG(flattenOntoWhite(scaleToMaxWidth(source, MediumMaxWidth)), VariantMedium, mediumSuffix, MediumQuality)
	if err != nil {
		failures = append(failures, err)
	} else {
		renditions = append(renditions, medium)
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

// encodeAVIFThumbnail encodes a feed thumbnail as AVIF.
func encodeAVIFThumbnail(source image.Image) (Rendition, error) {
	var buffer bytes.Buffer
	err := avif.Encode(&buffer, source, avif.Options{
		Quality: ThumbnailQuality,
		// The picture is opaque by now, so the alpha quality only has to be a
		// valid number; passing the picture quality keeps it from silently
		// falling back to the library default.
		QualityAlpha: ThumbnailQuality,
		Speed:        AVIFSpeed,
		// 4:2:0 is the chroma layout a photograph wants; the zero value of the
		// option would select 4:4:4 and cost bytes for detail nobody sees.
		ChromaSubsampling: image.YCbCrSubsampleRatio420,
	})
	if err != nil {
		return Rendition{}, fmt.Errorf("encode %s %s: %w", VariantThumb, FormatAVIF, err)
	}

	return newRendition(VariantThumb, FormatAVIF, buffer.Bytes(), source), nil
}

// encodeWebPThumbnail encodes a feed thumbnail as WebP.
func encodeWebPThumbnail(source image.Image) (Rendition, error) {
	var buffer bytes.Buffer
	err := webp.Encode(&buffer, source, &webp.Options{Quality: ThumbnailQuality})
	if err != nil {
		return Rendition{}, fmt.Errorf("encode %s %s: %w", VariantThumb, FormatWebP, err)
	}

	return newRendition(VariantThumb, FormatWebP, buffer.Bytes(), source), nil
}

// encodeJPEG encodes one JPEG rendition at the given quality.
func encodeJPEG(source image.Image, variant Variant, suffix string, quality int) (Rendition, error) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, source, &jpeg.Options{Quality: quality}); err != nil {
		return Rendition{}, fmt.Errorf("encode %s: %w", variant, err)
	}

	return newRendition(variant, FormatJPEG, buffer.Bytes(), source), nil
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
	case VariantMedium:
		return mediumSuffix
	case VariantPoster:
		return posterSuffix
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

// posterOffsetMS decides where in a video the poster frame is taken.
//
// A known duration puts the frame posterOffsetRatio of the way in; an unknown one
// takes the first frame, which is the only offset that is always present.
func posterOffsetMS(durationMS *int) int {
	if durationMS == nil || *durationMS <= 0 {
		return unknownDurationOffsetMS
	}
	offset := int(float64(*durationMS) * posterOffsetRatio)
	if offset < 0 {
		return unknownDurationOffsetMS
	}
	return offset
}

// ExtractPosterFrame renders one frame of a video as a JPEG.
//
// FFmpeg is a system prerequisite, not a Go dependency (KNOT-037). When it is
// absent the returned error is ErrPosterUnavailable, so the caller can log a
// missing prerequisite differently from a video it could not read. The frame is
// scaled to PosterMaxWidth with the aspect ratio preserved (`scale=400:-2` keeps
// the height even, which is what the JPEG encoder wants).
//
// The video is written to a temporary file first: FFmpeg seeks far more reliably
// over a file than over a pipe, and the offset is only meaningful with seeking.
func ExtractPosterFrame(ctx context.Context, video []byte, offsetMS int) ([]byte, error) {
	if len(video) == 0 {
		return nil, fmt.Errorf("extract poster: the video is empty")
	}

	binary, err := exec.LookPath(ffmpegBinary)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPosterUnavailable, err)
	}

	dir, err := os.MkdirTemp("", "knot-poster-")
	if err != nil {
		return nil, fmt.Errorf("extract poster: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	input := filepath.Join(dir, "source")
	if err := os.WriteFile(input, video, 0o600); err != nil {
		return nil, fmt.Errorf("extract poster: write source: %w", err)
	}

	output := filepath.Join(dir, "poster"+jpegExtension)
	offset := strconv.FormatFloat(float64(offsetMS)/1000, 'f', 3, 64)

	arguments := []string{
		"-y",
		"-ss", offset,
		"-i", input,
		"-frames:v", "1",
		"-q:v", "3",
		"-vf", fmt.Sprintf("scale=%d:-2", PosterMaxWidth),
		output,
	}

	command := exec.CommandContext(ctx, binary, arguments...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("extract poster: ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	frame, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("extract poster: read frame: %w", err)
	}
	if len(frame) == 0 {
		return nil, fmt.Errorf("extract poster: ffmpeg produced no frame")
	}

	return frame, nil
}
