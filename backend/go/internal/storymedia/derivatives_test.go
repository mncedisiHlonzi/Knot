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

func TestImageDerivativesScaleDownAndKeepAspectRatio(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 1000, 500))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}
	if len(renditions) != 2 {
		t.Fatalf("renditions = %d, want a thumbnail and a medium", len(renditions))
	}

	thumbnail, medium := renditions[0], renditions[1]

	if thumbnail.Variant != VariantThumb || medium.Variant != VariantMedium {
		t.Fatalf("variants = %q, %q, want thumb then medium", thumbnail.Variant, medium.Variant)
	}

	// 1000x500 scaled to a 400 max width is 400x200.
	thumbImage := decodeJPEG(t, thumbnail.Body)
	if got := thumbImage.Bounds(); got.Dx() != ThumbnailMaxWidth || got.Dy() != 200 {
		t.Errorf("thumbnail = %dx%d, want %dx200", got.Dx(), got.Dy(), ThumbnailMaxWidth)
	}
	if thumbnail.Width != ThumbnailMaxWidth || thumbnail.Height != 200 {
		t.Errorf("reported thumbnail size = %dx%d, want %dx200", thumbnail.Width, thumbnail.Height, ThumbnailMaxWidth)
	}

	// 1000 is narrower than the medium's 1200, so it is re-encoded at its own size
	// rather than upscaled.
	mediumImage := decodeJPEG(t, medium.Body)
	if got := mediumImage.Bounds(); got.Dx() != 1000 || got.Dy() != 500 {
		t.Errorf("medium = %dx%d, want the source's 1000x500", got.Dx(), got.Dy())
	}

	if thumbnail.ContentType != derivativeContentType || medium.ContentType != derivativeContentType {
		t.Errorf("content types = %q, %q, want %q", thumbnail.ContentType, medium.ContentType, derivativeContentType)
	}
	if thumbnail.Suffix != thumbnailSuffix || medium.Suffix != mediumSuffix {
		t.Errorf("suffixes = %q, %q, want %q, %q", thumbnail.Suffix, medium.Suffix, thumbnailSuffix, mediumSuffix)
	}
}

func TestImageDerivativesNeverUpscale(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 300, 200))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	for _, rendition := range renditions {
		if got := decodeJPEG(t, rendition.Body).Bounds(); got.Dx() != 300 || got.Dy() != 200 {
			t.Errorf("%s = %dx%d, want the source's 300x200", rendition.Variant, got.Dx(), got.Dy())
		}
	}
}

func TestImageDerivativesScaleAPortraitByWidth(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 500, 1500))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	// A tall image is still constrained by width, so 500x1500 becomes 400x1200.
	if got := decodeJPEG(t, renditions[0].Body).Bounds(); got.Dx() != ThumbnailMaxWidth || got.Dy() != 1200 {
		t.Errorf("thumbnail = %dx%d, want %dx1200", got.Dx(), got.Dy(), ThumbnailMaxWidth)
	}
}

func TestImageDerivativesFlattenTransparencyOntoWhite(t *testing.T) {
	renditions, err := ImageDerivatives(transparentPNG(t, 200))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	r, g, b, _ := decodeJPEG(t, renditions[0].Body).At(100, 100).RGBA()
	// JPEG is lossy, so near-white rather than exactly white.
	for name, channel := range map[string]uint32{"red": r, "green": g, "blue": b} {
		if channel>>8 < 250 {
			t.Errorf("%s channel = %d, want near-white (255) where the source was transparent", name, channel>>8)
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
