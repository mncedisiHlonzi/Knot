package storymedia

// The object-key naming scheme.
//
// The extensions and suffixes live in one place so the key a row stores and the
// key the Rust media worker writes cannot drift (KNOT-037-fix7). Since KNOT-038
// the upload itself IS stored, under `_original`, because the worker runs in a
// different process and downloads it from the bucket; the worker then writes the
// derivatives beside it and removes the original.
const (
	jpegExtension = ".jpg"
	avifExtension = ".avif"
	webpExtension = ".webp"
	mp4Extension  = ".mp4"
	movExtension  = ".mov"

	// originalSuffix names the uploaded file. It is the ONLY object written by the
	// request path; every other file belongs to the worker (KNOT-038).
	originalSuffix  = "_original"
	displaySuffix   = "_display"
	thumbnailSuffix = "_thumb"
	streamSuffix    = "_stream"
	// posterSuffix names a video's extracted still, written by the media worker
	// beside the stream since KNOT-042. A row written before KNOT-037-fix7 may also
	// hold a legacy JPEG poster under the same suffix, which is still served.
	posterSuffix = "_poster"
	// lqipSuffix names the ~20px placeholder the worker writes for every item,
	// image or video, from the same base name as its thumbnail or poster. It is
	// never stored in a column: the API derives it from the key it already has
	// (KNOT-043).
	lqipSuffix = "_lqip"
	// mediumSuffix only ever appears in rows written before KNOT-037-fix7; those
	// files are still served, but nothing new is written with it.
	mediumSuffix = "_medium"
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
