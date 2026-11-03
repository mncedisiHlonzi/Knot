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
