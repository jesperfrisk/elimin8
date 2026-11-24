package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxPhotoBytes = 5 * 1024 * 1024
const maxPhotoPixels = 25_000_000

var (
	ErrPhotoTooLarge    = errors.New("photo exceeds the 5 MiB or 25 megapixel limit")
	ErrPhotoUnsupported = errors.New("choose a JPEG, PNG or WebP photo")
	ErrPhotoInvalid     = errors.New("photo could not be decoded")
	photoDecodeSlots    = make(chan struct{}, 2)
)

// preparePhoto decodes into pixels and creates a fresh JPEG. No source metadata
// (including EXIF GPS information) can pass through to the stored image.
func preparePhoto(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) > MaxPhotoBytes {
		return nil, ErrPhotoTooLarge
	}
	if len(data) == 0 {
		return nil, ErrPhotoInvalid
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, ErrPhotoUnsupported
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrPhotoInvalid
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, ErrPhotoUnsupported
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > maxPhotoPixels {
		return nil, ErrPhotoTooLarge
	}
	select {
	case photoDecodeSlots <- struct{}{}:
		defer func() { <-photoDecodeSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrPhotoInvalid
	}
	width, height := config.Width, config.Height
	if width > 400 || height > 400 {
		if width >= height {
			height, width = max(1, height*400/width), 400
		} else {
			width, height = max(1, width*400/height), 400
		}
	}
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	// Flatten transparent PNG/WebP pixels against white instead of black.
	draw.Draw(result, result.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(result, result.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, result, &jpeg.Options{Quality: 85}); err != nil {
		return nil, ErrPhotoInvalid
	}
	return encoded.Bytes(), nil
}
