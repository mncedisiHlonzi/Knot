package storymedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/chai2010/webp"
	"github.com/gen2brain/avif"
)

// testPNG encodes a solid image of the given size, so a test can assert what a
// rendition's dimensions should be from a known source.
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encoding the test PNG: %v", err)
	}

	return buffer.Bytes()
}

// transparentPNG encodes a fully transparent image, for the flattening test.
func transparentPNG(t *testing.T, size int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, size, size))

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encoding the transparent test PNG: %v", err)
	}

	return buffer.Bytes()
}

// decodeJPEG decodes a rendition so a test can read its pixels and dimensions.
func decodeJPEG(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the rendition as JPEG: %v", err)
	}

	return img
}

// decodeAVIF decodes an AVIF rendition.
func decodeAVIF(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, err := avif.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the rendition as AVIF: %v", err)
	}

	return img
}

// decodeWebP decodes a WebP rendition.
func decodeWebP(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the rendition as WebP: %v", err)
	}

	return img
}

// renditionFor finds one rendition by variant and format.
func renditionFor(t *testing.T, renditions []Rendition, variant Variant, format Format) Rendition {
	t.Helper()

	for _, rendition := range renditions {
		if rendition.Variant == variant && rendition.Format == format {
			return rendition
		}
	}

	t.Fatalf("no %s rendition in %s format (have %d renditions)", variant, format, len(renditions))
	return Rendition{}
}

func TestImageDerivativesScaleDownAndKeepAspectRatio(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 1000, 500))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}
	if len(renditions) != 3 {
		t.Fatalf("renditions = %d, want an AVIF thumbnail, a WebP thumbnail, and a medium", len(renditions))
	}

	// The order is the ladder: AVIF first, then the WebP fallback, then the medium.
	avifThumbnail := renditionFor(t, renditions, VariantThumb, FormatAVIF)
	webpThumbnail := renditionFor(t, renditions, VariantThumb, FormatWebP)
	medium := renditionFor(t, renditions, VariantMedium, FormatJPEG)

	// 1000x500 scaled to an 800 max width is 800x400.
	thumbnailCases := []struct {
		name      string
		rendition Rendition
		decode    func(*testing.T, []byte) image.Image
	}{
		{name: "avif", rendition: avifThumbnail, decode: decodeAVIF},
		{name: "webp", rendition: webpThumbnail, decode: decodeWebP},
	}
	for _, testCase := range thumbnailCases {
		bounds := testCase.decode(t, testCase.rendition.Body).Bounds()
		if bounds.Dx() != ThumbnailMaxWidth || bounds.Dy() != 400 {
			t.Errorf("%s thumbnail = %dx%d, want %dx400", testCase.name, bounds.Dx(), bounds.Dy(), ThumbnailMaxWidth)
		}
		if testCase.rendition.Width != ThumbnailMaxWidth || testCase.rendition.Height != 400 {
			t.Errorf("reported %s thumbnail size = %dx%d, want %dx400",
				testCase.name, testCase.rendition.Width, testCase.rendition.Height, ThumbnailMaxWidth)
		}
	}

	// 1000 is narrower than the medium's 1200, so it is re-encoded at its own size
	// rather than upscaled.
	if got := decodeJPEG(t, medium.Body).Bounds(); got.Dx() != 1000 || got.Dy() != 500 {
		t.Errorf("medium = %dx%d, want the source's 1000x500", got.Dx(), got.Dy())
	}

	// The content type and the key extension follow the format, so a file is never
	// stored as one codec and served as another.
	wanted := map[Format]struct {
		contentType string
		extension   string
	}{
		FormatAVIF: {avifContentType, avifExtension},
		FormatWebP: {webpContentType, webpExtension},
		FormatJPEG: {jpegContentType, jpegExtension},
	}
	for format, want := range wanted {
		got := map[Format]Rendition{FormatAVIF: avifThumbnail, FormatWebP: webpThumbnail, FormatJPEG: medium}[format]
		if got.ContentType != want.contentType {
			t.Errorf("%s content type = %q, want %q", format, got.ContentType, want.contentType)
		}
		if got.Extension != want.extension {
			t.Errorf("%s extension = %q, want %q", format, got.Extension, want.extension)
		}
	}

	if avifThumbnail.Suffix != thumbnailSuffix || webpThumbnail.Suffix != thumbnailSuffix {
		t.Errorf("thumbnail suffixes = %q, %q, want both %q",
			avifThumbnail.Suffix, webpThumbnail.Suffix, thumbnailSuffix)
	}
	if medium.Suffix != mediumSuffix {
		t.Errorf("medium suffix = %q, want %q", medium.Suffix, mediumSuffix)
	}

	// AVIF at this effort is the point of the task: it must not be the larger file.
	if len(avifThumbnail.Body) >= len(webpThumbnail.Body) {
		t.Errorf("avif thumbnail = %d bytes, want fewer than the webp thumbnail's %d",
			len(avifThumbnail.Body), len(webpThumbnail.Body))
	}
}

func TestImageDerivativesNeverUpscale(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 300, 200))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	for _, rendition := range renditions {
		var bounds image.Rectangle
		if rendition.Format == FormatAVIF {
			bounds = decodeAVIF(t, rendition.Body).Bounds()
		} else if rendition.Format == FormatWebP {
			bounds = decodeWebP(t, rendition.Body).Bounds()
		} else {
			bounds = decodeJPEG(t, rendition.Body).Bounds()
		}
		if bounds.Dx() != 300 || bounds.Dy() != 200 {
			t.Errorf("%s/%s = %dx%d, want the source's 300x200",
				rendition.Variant, rendition.Format, bounds.Dx(), bounds.Dy())
		}
	}
}

func TestImageDerivativesScaleAPortraitByWidth(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 900, 1350))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	// A tall image is still constrained by width, so 900x1350 becomes 800x1200.
	thumbnail := renditionFor(t, renditions, VariantThumb, FormatAVIF)
	if got := decodeAVIF(t, thumbnail.Body).Bounds(); got.Dx() != ThumbnailMaxWidth || got.Dy() != 1200 {
		t.Errorf("thumbnail = %dx%d, want %dx1200", got.Dx(), got.Dy(), ThumbnailMaxWidth)
	}
}

func TestImageDerivativesFlattenTransparencyOntoWhite(t *testing.T) {
	renditions, err := ImageDerivatives(transparentPNG(t, 200))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	// The AVIF thumbnail is checked because it is the file the feed loads: neither
	// codec is asked to keep alpha, so an unflattened picture would go black.
	r, g, b, _ := decodeAVIF(t, renditionFor(t, renditions, VariantThumb, FormatAVIF).Body).At(100, 100).RGBA()
	// Both codecs are lossy, so near-white rather than exactly white.
	for name, channel := range map[string]uint32{"red": r, "green": g, "blue": b} {
		if channel>>8 < 245 {
			t.Errorf("%s channel = %d, want near-white (255) where the source was transparent", name, channel>>8)
		}
	}

	// The WebP fallback must agree with it.
	r, g, b, _ = decodeWebP(t, renditionFor(t, renditions, VariantThumb, FormatWebP).Body).At(100, 100).RGBA()
	for name, channel := range map[string]uint32{"red": r, "green": g, "blue": b} {
		if channel>>8 < 245 {
			t.Errorf("%s channel = %d in the webp fallback, want near-white (255)", name, channel>>8)
		}
	}
}

func TestImageDerivativesRejectsUndecodableBytes(t *testing.T) {
	if _, err := ImageDerivatives([]byte("not an image")); err == nil {
		t.Fatal("ImageDerivatives() error = nil, want a decode failure")
	}
}

func TestPosterOffsetIsAFifthOfTheDuration(t *testing.T) {
	threeSeconds := 3000
	cases := []struct {
		name       string
		durationMS *int
		want       int
	}{
		{name: "known duration", durationMS: &threeSeconds, want: 600},
		{name: "unknown duration", durationMS: nil, want: 0},
		{name: "zero duration", durationMS: intPointer(0), want: 0},
		{name: "negative duration", durationMS: intPointer(-5), want: 0},
	}

	for _, testCase := range cases {
		if got := posterOffsetMS(testCase.durationMS); got != testCase.want {
			t.Errorf("%s: posterOffsetMS() = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func intPointer(value int) *int { return &value }

func TestParseVariant(t *testing.T) {
	cases := []struct {
		raw    string
		want   Variant
		wantOK bool
	}{
		{raw: "", want: "", wantOK: true},
		{raw: "thumb", want: VariantThumb, wantOK: true},
		{raw: " medium ", want: VariantMedium, wantOK: true},
		{raw: "poster", want: VariantPoster, wantOK: true},
		{raw: "huge", want: "", wantOK: false},
		{raw: "original", want: "", wantOK: false},
	}

	for _, testCase := range cases {
		got, ok := ParseVariant(testCase.raw)
		if got != testCase.want || ok != testCase.wantOK {
			t.Errorf("ParseVariant(%q) = %q, %v, want %q, %v", testCase.raw, got, ok, testCase.want, testCase.wantOK)
		}
	}
}

// TestExtractPosterFrameNeedsFFmpeg pins the missing-prerequisite path. It runs on
// machines without FFmpeg (which is the case in this checkout) and skips where
// FFmpeg is installed, because there the error can never occur.
func TestExtractPosterFrameNeedsFFmpeg(t *testing.T) {
	if _, err := exec.LookPath(ffmpegBinary); err == nil {
		t.Skip("FFmpeg is installed; the missing-prerequisite path cannot be exercised")
	}

	_, err := ExtractPosterFrame(context.Background(), []byte("not a video"), 0)
	if !errors.Is(err, ErrPosterUnavailable) {
		t.Fatalf("ExtractPosterFrame() error = %v, want ErrPosterUnavailable", err)
	}
}

// TestExtractPosterFrame renders a frame from a generated clip. It needs FFmpeg
// twice — once to build the clip and once to extract the frame — so it skips
// wherever FFmpeg is absent.
func TestExtractPosterFrame(t *testing.T) {
	binary, err := exec.LookPath(ffmpegBinary)
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}

	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp4")
	build := exec.Command(
		binary, "-y",
		"-f", "lavfi",
		"-i", "testsrc=size=640x360:rate=10:duration=1",
		"-pix_fmt", "yuv420p",
		clip,
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building a test clip: %v: %s", err, output)
	}

	video, err := os.ReadFile(clip)
	if err != nil {
		t.Fatalf("reading the test clip: %v", err)
	}

	frame, err := ExtractPosterFrame(context.Background(), video, 0)
	if err != nil {
		t.Fatalf("ExtractPosterFrame() error = %v, want nil", err)
	}

	bounds := decodeJPEG(t, frame).Bounds()
	if bounds.Dx() != PosterMaxWidth {
		t.Errorf("poster width = %d, want %d", bounds.Dx(), PosterMaxWidth)
	}
	// 640x360 at 400 wide is 225 high; `-2` rounds it to an even number.
	if bounds.Dy() < 200 || bounds.Dy() > 250 {
		t.Errorf("poster height = %d, want roughly 225", bounds.Dy())
	}
}
