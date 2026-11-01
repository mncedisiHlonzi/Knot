package storymedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
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

func TestImageDerivativesProduceDisplayAndThumbnail(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 1000, 500))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}
	if len(renditions) != 2 {
		t.Fatalf("renditions = %d, want a display rendition and a thumbnail", len(renditions))
	}

	display := renditionFor(t, renditions, VariantDisplay, FormatAVIF)
	thumbnail := renditionFor(t, renditions, VariantThumb, FormatWebP)

	// 1000x500 is narrower than the 2048 cap, so the display keeps its own size.
	if bounds := decodeAVIF(t, display.Body).Bounds(); bounds.Dx() != 1000 || bounds.Dy() != 500 {
		t.Errorf("display = %dx%d, want the source's 1000x500", bounds.Dx(), bounds.Dy())
	}
	if display.Width != 1000 || display.Height != 500 {
		t.Errorf("reported display size = %dx%d, want 1000x500", display.Width, display.Height)
	}

	// The thumbnail is capped at 400 wide: 1000x500 becomes 400x200.
	if bounds := decodeWebP(t, thumbnail.Body).Bounds(); bounds.Dx() != ThumbnailMaxWidth || bounds.Dy() != 200 {
		t.Errorf("thumbnail = %dx%d, want %dx200", bounds.Dx(), bounds.Dy(), ThumbnailMaxWidth)
	}

	// The suffix, extension and content type follow the variant, so a file is never
	// stored as one codec and served as another.
	if display.Suffix != displaySuffix || display.Extension != avifExtension || display.ContentType != avifContentType {
		t.Errorf("display = (%q, %q, %q), want (%q, %q, %q)",
			display.Suffix, display.Extension, display.ContentType, displaySuffix, avifExtension, avifContentType)
	}
	if thumbnail.Suffix != thumbnailSuffix || thumbnail.Extension != webpExtension || thumbnail.ContentType != webpContentType {
		t.Errorf("thumbnail = (%q, %q, %q), want (%q, %q, %q)",
			thumbnail.Suffix, thumbnail.Extension, thumbnail.ContentType, thumbnailSuffix, webpExtension, webpContentType)
	}
}

func TestImageDerivativesCapTheDisplayAt2048(t *testing.T) {
	renditions, err := ImageDerivatives(testPNG(t, 2200, 1100))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	// 2200x1100 scaled to a 2048 max width is 2048x1024.
	display := renditionFor(t, renditions, VariantDisplay, FormatAVIF)
	if got := decodeAVIF(t, display.Body).Bounds(); got.Dx() != DisplayMaxWidth || got.Dy() != 1024 {
		t.Errorf("display = %dx%d, want %dx1024", got.Dx(), got.Dy(), DisplayMaxWidth)
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
		} else {
			bounds = decodeWebP(t, rendition.Body).Bounds()
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

	// A tall image is still constrained by width, so the 400px thumbnail of
	// 900x1350 is 400x600.
	thumbnail := renditionFor(t, renditions, VariantThumb, FormatWebP)
	if got := decodeWebP(t, thumbnail.Body).Bounds(); got.Dx() != ThumbnailMaxWidth || got.Dy() != 600 {
		t.Errorf("thumbnail = %dx%d, want %dx600", got.Dx(), got.Dy(), ThumbnailMaxWidth)
	}
}

func TestImageDerivativesFlattenTransparencyOntoWhite(t *testing.T) {
	renditions, err := ImageDerivatives(transparentPNG(t, 200))
	if err != nil {
		t.Fatalf("ImageDerivatives() error = %v, want nil", err)
	}

	// Neither encoder is asked to keep alpha, so an unflattened picture would go
	// black — check both files the pipeline stores.
	cases := []struct {
		name   string
		body   []byte
		decode func(*testing.T, []byte) image.Image
	}{
		{name: "display", body: renditionFor(t, renditions, VariantDisplay, FormatAVIF).Body, decode: decodeAVIF},
		{name: "thumbnail", body: renditionFor(t, renditions, VariantThumb, FormatWebP).Body, decode: decodeWebP},
	}

	for _, testCase := range cases {
		r, g, b, _ := testCase.decode(t, testCase.body).At(100, 100).RGBA()
		// Both codecs are lossy, so near-white rather than exactly white.
		for name, channel := range map[string]uint32{"red": r, "green": g, "blue": b} {
			if channel>>8 < 245 {
				t.Errorf("%s: %s channel = %d, want near-white (255) where the source was transparent",
					testCase.name, name, channel>>8)
			}
		}
	}
}

func TestImageDerivativesRejectsUndecodableBytes(t *testing.T) {
	if _, err := ImageDerivatives([]byte("not an image")); err == nil {
		t.Fatal("ImageDerivatives() error = nil, want a decode failure")
	}
}

// decodePNG decodes a PNG, which is what the transcode test asks FFmpeg to write
// when it pulls a frame out of the stream.
func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the PNG: %v", err)
	}

	return img
}

func TestParseVariant(t *testing.T) {
	cases := []struct {
		raw    string
		want   Variant
		wantOK bool
	}{
		{raw: "", want: "", wantOK: true},
		{raw: "thumb", want: VariantThumb, wantOK: true},
		{raw: "display", want: VariantDisplay, wantOK: true},
		{raw: "stream", want: VariantStream, wantOK: true},
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

// TestTranscodeToStreamNeedsFFmpeg pins the missing-prerequisite path. Emptying
// PATH makes the lookup fail whatever the machine has installed, so the test is
// deterministic instead of skipping itself away.
func TestTranscodeToStreamNeedsFFmpeg(t *testing.T) {
	t.Setenv("PATH", "")

	err := TranscodeToStream(context.Background(), "in.mp4", "out.mp4")
	if !errors.Is(err, ErrFFmpegUnavailable) {
		t.Fatalf("TranscodeToStream() error = %v, want ErrFFmpegUnavailable", err)
	}
}

// TestTranscodeToStreamWritesAFastStartStream builds a real clip with FFmpeg,
// transcodes it, and proves the two properties the feed depends on: the index
// (`moov`) comes BEFORE the media data (that is what `+faststart` buys, and what
// lets a player start on the first bytes it receives), and the width is capped at
// StreamMaxWidth (KNOT-037-fix7).
func TestTranscodeToStreamWritesAFastStartStream(t *testing.T) {
	binary, err := exec.LookPath(ffmpegBinary)
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	build := exec.Command(
		binary, "-y",
		"-f", "lavfi",
		// Taller than the cap in width, so the scale filter has work to do.
		"-i", "testsrc=size=1440x2560:rate=10:duration=1",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-shortest",
		input,
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building a test clip: %v: %s", err, output)
	}

	output := filepath.Join(dir, "stream.mp4")
	if err := TranscodeToStream(context.Background(), input, output); err != nil {
		t.Fatalf("TranscodeToStream() error = %v, want nil", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("the stream is empty")
	}
	if got := string(data[4:8]); got != "ftyp" {
		t.Errorf("stream starts with %q, want an MP4 ftyp box", got)
	}

	moov := bytes.Index(data, []byte("moov"))
	mdat := bytes.Index(data, []byte("mdat"))
	if moov < 0 || mdat < 0 {
		t.Fatalf("stream boxes: moov=%d mdat=%d, want both present", moov, mdat)
	}
	if moov > mdat {
		t.Errorf("moov at %d is after mdat at %d; the index must be at the front (+faststart)", moov, mdat)
	}

	// The width cap is checked by decoding one frame of the result.
	frame := filepath.Join(dir, "frame.png")
	extract := exec.Command(binary, "-y", "-i", output, "-frames:v", "1", frame)
	if output, err := extract.CombinedOutput(); err != nil {
		t.Fatalf("extracting a frame: %v: %s", err, output)
	}
	still, err := os.ReadFile(frame)
	if err != nil {
		t.Fatalf("reading the frame: %v", err)
	}
	bounds := decodePNG(t, still).Bounds()
	if bounds.Dx() != StreamMaxWidth {
		t.Errorf("stream width = %d, want %d", bounds.Dx(), StreamMaxWidth)
	}
}
