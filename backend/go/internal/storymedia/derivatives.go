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

	"github.com/disintegration/imaging"
	// The uploads allowlist accepts image/webp, and Go's image package has no
	// WebP decoder of its own. Registering this one is what lets a WebP upload
	// produce derivatives instead of quietly falling back to the original
	// (KNOT-037). It is decode-only; nothing re-encodes to WebP.
	_ "golang.org/x/image/webp"
)

// The derivative ladder (KNOT-037, KNOT-ADR-091).
//
// The numbers are the product decision, not a tuning knob: a feed card is at most
// 400 logical points wide on the widest phone, so a 400px thumbnail is already
// retina-marginal, and the story detail is a full-width column at roughly 1200px
// on a tablet. Everything is JPEG because these are photographs being shown at
// screen size, where PNG's lossless bytes buy nothing.
const (
	// ThumbnailMaxWidth is the widest a feed thumbnail may be. A narrower source
	// is never upscaled.
	ThumbnailMaxWidth = 400
	// ThumbnailQuality is the JPEG quality of a feed thumbnail.
	ThumbnailQuality = 80
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

// derivativeExtension and the suffixes are the object-key naming scheme. They
// live here so the key a row stores and the key a reader asks for cannot drift.
const (
	derivativeExtension = ".jpg"
	thumbnailSuffix     = "_thumb"
	mediumSuffix        = "_medium"
	posterSuffix        = "_poster"
)

// derivativeContentType is the content type of every derivative. They are all
// JPEG, whatever the original was, which is why serving one never echoes the
// row's stored MIME type.
const derivativeContentType = "image/jpeg"

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
	// Suffix is appended to the object's base name, e.g. "_thumb".
	Suffix string
	// ContentType is the stored content type (always derivativeContentType).
	ContentType string
	// Body is the encoded rendition.
	Body []byte
	// Width and Height are the rendition's pixel dimensions.
	Width  int
	Height int
}

// ImageDerivatives decodes an uploaded image and returns its thumbnail and medium
// renditions, in that order.
//
// Decoding applies EXIF orientation, because a phone photo is usually stored
// sideways with a rotation flag, and a thumbnail that ignores the flag is a
// thumbnail of a sideways picture. A source narrower than a rendition's max width
// is re-encoded at its own size rather than upscaled: the point of the pipeline is
// fewer bytes, not more.
//
// Transparency is composited onto white before encoding. JPEG has no alpha, and
// letting the encoder drop it turns a transparent PNG's background black.
func ImageDerivatives(data []byte) ([]Rendition, error) {
	source, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	thumbnail, err := encodeRendition(source, VariantThumb, thumbnailSuffix, ThumbnailMaxWidth, ThumbnailQuality)
	if err != nil {
		return nil, err
	}

	medium, err := encodeRendition(source, VariantMedium, mediumSuffix, MediumMaxWidth, MediumQuality)
	if err != nil {
		return nil, err
	}

	return []Rendition{thumbnail, medium}, nil
}

// encodeRendition scales one source image to a maximum width and encodes it as
// JPEG.
func encodeRendition(source image.Image, variant Variant, suffix string, maxWidth, quality int) (Rendition, error) {
	scaled := scaleToMaxWidth(source, maxWidth)
	// The composite is always done: it is a small image by now, and doing it
	// unconditionally means an opaque photo and a transparent PNG follow exactly
	// the same path.
	flattened := imaging.Overlay(
		imaging.New(scaled.Bounds().Dx(), scaled.Bounds().Dy(), color.White),
		scaled,
		image.Point{},
		1.0,
	)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, flattened, &jpeg.Options{Quality: quality}); err != nil {
		return Rendition{}, fmt.Errorf("encode %s: %w", variant, err)
	}

	return Rendition{
		Variant:     variant,
		Suffix:      suffix,
		ContentType: derivativeContentType,
		Body:        buffer.Bytes(),
		Width:       flattened.Bounds().Dx(),
		Height:      flattened.Bounds().Dy(),
	}, nil
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

	output := filepath.Join(dir, "poster"+derivativeExtension)
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
