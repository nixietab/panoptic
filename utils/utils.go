package utils

import (
	"bytes"
	"encoding/base64"
	"html"
	"image"
	"image/jpeg"
	"regexp"
	"strings"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

func StripHTMLTags(s string) string {
	return htmlTagRegex.ReplaceAllString(s, "")
}

func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

// IsEmbeddedImage reports whether the message carries an embedded image
// delivered as a base64 data URI.
func IsEmbeddedImage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "data:image/") && strings.Contains(lower, "base64")
}

// LogSafeMessage returns a short placeholder when the message embeds a large
// base64 image that would otherwise flood the logs.
func LogSafeMessage(msg string) string {
	if IsEmbeddedImage(msg) {
		return "image detected, not logged"
	}
	return msg
}

// ResizeImage scales img to width x height using nearest-neighbour sampling.
func ResizeImage(img image.Image, width, height uint) image.Image {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW == 0 || srcH == 0 || width == 0 || height == 0 {
		return img
	}

	dst := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	xRatio := float64(srcW) / float64(width)
	yRatio := float64(srcH) / float64(height)

	for y := uint(0); y < height; y++ {
		srcY := bounds.Min.Y + int(float64(y)*yRatio)
		for x := uint(0); x < width; x++ {
			srcX := bounds.Min.X + int(float64(x)*xRatio)
			dst.Set(int(x), int(y), img.At(srcX, srcY))
		}
	}

	return dst
}

// EncodeImageDataURI encodes img as a JPEG data URI, picking the highest
// quality that still fits within maxChars (the Mumble message size budget).
// It returns "" when no quality fits.
func EncodeImageDataURI(img image.Image, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 4850
	}

	prefix := `<img src="data:image/jpeg;base64,`
	suffix := `" />`

	for _, quality := range []int{60, 40, 25} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			continue
		}

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		encoded := prefix + b64 + suffix

		if len(encoded) <= maxChars {
			return encoded
		}
	}

	return ""
}
