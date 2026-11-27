package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

var photoCornerColors = [4]color.RGBA{{220, 30, 30, 255}, {30, 200, 30, 255}, {30, 30, 220, 255}, {220, 200, 30, 255}}

func quadrantPhoto() *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, 800, 480))
	for i, rectangle := range []image.Rectangle{image.Rect(0, 0, 400, 240), image.Rect(400, 0, 800, 240), image.Rect(0, 240, 400, 480), image.Rect(400, 240, 800, 480)} {
		draw.Draw(result, rectangle, image.NewUniform(photoCornerColors[i]), image.Point{}, draw.Src)
	}
	return result
}

// orientationTIFF includes a real GPS IFD in addition to orientation so the
// corrected output must preserve displayed pixels while discarding GPS metadata.
func orientationTIFF(orientation int, order binary.ByteOrder) []byte {
	result := make([]byte, 92)
	copy(result, []byte("II"))
	if order == binary.BigEndian {
		copy(result, []byte("MM"))
	}
	order.PutUint16(result[2:4], 42)
	order.PutUint32(result[4:8], 8)
	order.PutUint16(result[8:10], 2)
	order.PutUint16(result[10:12], 0x0112)
	order.PutUint16(result[12:14], 3)
	order.PutUint32(result[14:18], 1)
	order.PutUint16(result[18:20], uint16(orientation))
	order.PutUint16(result[22:24], 0x8825)
	order.PutUint16(result[24:26], 4)
	order.PutUint32(result[26:30], 1)
	order.PutUint32(result[30:34], 38)
	order.PutUint16(result[38:40], 2)
	order.PutUint16(result[40:42], 1)
	order.PutUint16(result[42:44], 2)
	order.PutUint32(result[44:48], 2)
	result[48] = 'N'
	order.PutUint16(result[52:54], 2)
	order.PutUint16(result[54:56], 5)
	order.PutUint32(result[56:60], 3)
	order.PutUint32(result[60:64], 68)
	for i, value := range []uint32{59, 20, 0} {
		order.PutUint32(result[68+i*8:72+i*8], value)
		order.PutUint32(result[72+i*8:76+i*8], 1)
	}
	return result
}

func photoPNGChunk(kind string, payload []byte) []byte {
	result := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(result[:4], uint32(len(payload)))
	copy(result[4:8], kind)
	copy(result[8:], payload)
	binary.BigEndian.PutUint32(result[len(result)-4:], crc32.ChecksumIEEE(result[4:len(result)-4]))
	return result
}
func photoRIFFChunk(kind string, payload []byte) []byte {
	result := make([]byte, 8+len(payload)+len(payload)%2)
	copy(result[:4], kind)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	return result
}
func photoWithEXIF(t *testing.T, source []byte, format string, metadata []byte) []byte {
	t.Helper()
	switch format {
	case "jpeg":
		payload := append([]byte("Exif\x00\x00"), metadata...)
		segment := make([]byte, 4+len(payload))
		segment[0], segment[1] = 0xff, 0xe1
		binary.BigEndian.PutUint16(segment[2:4], uint16(len(payload)+2))
		copy(segment[4:], payload)
		result := append([]byte(nil), source[:2]...)
		result = append(result, segment...)
		return append(result, source[2:]...)
	case "png":
		result := append([]byte(nil), source[:33]...)
		result = append(result, photoPNGChunk("eXIf", metadata)...)
		return append(result, source[33:]...)
	case "webp":
		config, _, err := image.DecodeConfig(bytes.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		extended := make([]byte, 10)
		extended[0] = 1 << 3 // WebP EXIF flag
		width, height := config.Width-1, config.Height-1
		for i := 0; i < 3; i++ {
			extended[4+i] = byte(width >> (8 * i))
			extended[7+i] = byte(height >> (8 * i))
		}
		result := append([]byte(nil), source[:12]...)
		if string(source[12:16]) != "VP8X" {
			result = append(result, photoRIFFChunk("VP8X", extended)...)
		}
		result = append(result, source[12:]...)
		result = append(result, photoRIFFChunk("EXIF", metadata)...)
		binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
		return result
	}
	t.Fatal("unknown fixture format", format)
	return nil
}
func orientationFixtures(t *testing.T) map[string][]byte {
	t.Helper()
	source := quadrantPhoto()
	var jpegBytes, pngBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngBytes, source); err != nil {
		t.Fatal(err)
	}
	webpBytes, err := os.ReadFile("testdata/photo-corners.webp")
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"jpeg": jpegBytes.Bytes(), "png": pngBytes.Bytes(), "webp": webpBytes}
}
func assertPhotoColor(t *testing.T, actual color.Color, want color.RGBA) {
	t.Helper()
	got := color.RGBAModel.Convert(actual).(color.RGBA)
	// Two JPEG encodings can alter values slightly; quadrants are sampled far
	// from their boundaries, so a wrong rotation/mirror remains unambiguous.
	for _, values := range [][2]uint8{{got.R, want.R}, {got.G, want.G}, {got.B, want.B}} {
		delta := int(values[0]) - int(values[1])
		if delta < -25 || delta > 25 {
			t.Fatalf("pixel=%v want near %v", got, want)
		}
	}
}
func TestPreparePhotoAppliesAllEXIFOrientations(t *testing.T) {
	// Expected output TL,TR,BL,BR indexes into the original colored quadrants.
	corners := [][4]int{{0, 1, 2, 3}, {1, 0, 3, 2}, {3, 2, 1, 0}, {2, 3, 0, 1}, {0, 2, 1, 3}, {2, 0, 3, 1}, {3, 1, 2, 0}, {1, 3, 0, 2}}
	for format, source := range orientationFixtures(t) {
		for orientation, wantCorners := range corners {
			orientation++
			t.Run(fmt.Sprintf("%s/%d", format, orientation), func(t *testing.T) {
				var order binary.ByteOrder = binary.LittleEndian
				if orientation%2 == 0 {
					order = binary.BigEndian
				}
				metadata := orientationTIFF(orientation, order)
				result, err := preparePhoto(context.Background(), photoWithEXIF(t, source, format, metadata))
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := jpeg.Decode(bytes.NewReader(result))
				if err != nil {
					t.Fatal(err)
				}
				width, height := 400, 240
				if orientation >= 5 {
					width, height = 240, 400
				}
				if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
					t.Fatalf("dimensions=%v want %dx%d", decoded.Bounds(), width, height)
				}
				points := []image.Point{{20, 20}, {width - 21, 20}, {20, height - 21}, {width - 21, height - 21}}
				for corner, point := range points {
					assertPhotoColor(t, decoded.At(point.X, point.Y), photoCornerColors[wantCorners[corner]])
				}
				if bytes.Contains(result, []byte("Exif")) || bytes.Contains(result, metadata) {
					t.Fatal("orientation/GPS metadata survived fresh JPEG encoding")
				}
				if got, err := photoOrientation(result, "jpeg"); err != nil || got != 1 {
					t.Fatalf("output orientation=%d err=%v", got, err)
				}
			})
		}
	}
}
func TestOrientedPhotoRespectsNonzeroBounds(t *testing.T) {
	source := image.NewRGBA(image.Rect(5, 7, 8, 9))
	source.SetRGBA(5, 7, photoCornerColors[0])
	source.SetRGBA(7, 7, photoCornerColors[1])
	source.SetRGBA(5, 8, photoCornerColors[2])
	source.SetRGBA(7, 8, photoCornerColors[3])
	rotated := orientedPhoto{source: source, orientation: 6}
	if rotated.Bounds() != image.Rect(0, 0, 2, 3) {
		t.Fatal(rotated.Bounds())
	}
	assertPhotoColor(t, rotated.At(0, 0), photoCornerColors[2])
	assertPhotoColor(t, rotated.At(1, 0), photoCornerColors[0])
	if _, _, _, alpha := rotated.At(-1, 0).RGBA(); alpha != 0 {
		t.Fatal("out-of-bounds pixel is not transparent")
	}
}
func TestPreparePhotoOrientationPreservesWhiteTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	draw.Draw(source, image.Rect(40, 0, 80, 40), image.NewUniform(photoCornerColors[0]), image.Point{}, draw.Src)
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	result, err := preparePhoto(context.Background(), photoWithEXIF(t, input.Bytes(), "png", orientationTIFF(6, binary.LittleEndian)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != image.Rect(0, 0, 40, 80) {
		t.Fatal(decoded.Bounds())
	}
	assertPhotoColor(t, decoded.At(20, 10), color.RGBA{255, 255, 255, 255})
	assertPhotoColor(t, decoded.At(20, 70), photoCornerColors[0])
}
func TestPhotoOrientationRejectsMalformedMetadata(t *testing.T) {
	fixtures := orientationFixtures(t)
	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"truncated TIFF", func(data []byte) []byte { return data[:7] }},
		{"unknown byte order", func(data []byte) []byte { data[0] = 'X'; return data }},
		{"bad TIFF magic", func(data []byte) []byte { data[2] = 0; return data }},
		{"IFD offset overflow", func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4:8], 0xffffffff); return data }},
		{"IFD before header", func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4:8], 2); return data }},
		{"truncated directory", func(data []byte) []byte { return data[:20] }},
		{"too many entries", func(data []byte) []byte {
			data = append(data, make([]byte, 7000)...)
			binary.LittleEndian.PutUint16(data[8:10], maxPhotoIFDEntries+1)
			return data
		}},
		{"wrong tag type", func(data []byte) []byte { binary.LittleEndian.PutUint16(data[12:14], 4); return data }},
		{"wrong value count", func(data []byte) []byte { binary.LittleEndian.PutUint32(data[14:18], 0xffffffff); return data }},
		{"zero orientation", func(data []byte) []byte { binary.LittleEndian.PutUint16(data[18:20], 0); return data }},
		{"unknown orientation", func(data []byte) []byte { binary.LittleEndian.PutUint16(data[18:20], 9); return data }},
		{"duplicate orientation", func(data []byte) []byte { copy(data[22:34], data[10:22]); return data }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metadata := tc.mutate(orientationTIFF(6, binary.LittleEndian))
			for format, source := range fixtures {
				if _, err := preparePhoto(context.Background(), photoWithEXIF(t, source, format, metadata)); !errors.Is(err, ErrPhotoInvalid) {
					t.Fatalf("%s accepts malformed metadata: %v", format, err)
				}
			}
		})
	}
	oversized := append(orientationTIFF(6, binary.LittleEndian), make([]byte, maxPhotoEXIFBytes)...)
	for _, format := range []string{"png", "webp"} {
		if _, err := preparePhoto(context.Background(), photoWithEXIF(t, fixtures[format], format, oversized)); !errors.Is(err, ErrPhotoInvalid) {
			t.Fatalf("%s accepts oversized EXIF: %v", format, err)
		}
	}
}
func TestPhotoOrientationContainerBoundsAndOptionalMetadata(t *testing.T) {
	fixtures := orientationFixtures(t)
	for format, source := range fixtures {
		if got, err := photoOrientation(source, format); err != nil || got != 1 {
			t.Fatalf("missing %s EXIF: %d %v", format, got, err)
		}
		metadata := orientationTIFF(6, binary.LittleEndian)
		once := photoWithEXIF(t, source, format, metadata)
		twice := photoWithEXIF(t, once, format, metadata)
		if _, err := photoOrientation(twice, format); !errors.Is(err, ErrPhotoInvalid) {
			t.Fatalf("duplicate %s EXIF accepted: %v", format, err)
		}
	}
	webp := photoWithEXIF(t, fixtures["webp"], "webp", append([]byte("Exif\x00\x00"), orientationTIFF(8, binary.BigEndian)...))
	if got, err := photoOrientation(webp, "webp"); err != nil || got != 8 {
		t.Fatalf("WebP EXIF prefix: %d %v", got, err)
	}
	// An odd-sized EXIF chunk must account for RIFF's padding byte.
	webp = photoWithEXIF(t, fixtures["webp"], "webp", append(orientationTIFF(7, binary.LittleEndian), 0))
	if got, err := photoOrientation(webp, "webp"); err != nil || got != 7 {
		t.Fatalf("WebP odd chunk: %d %v", got, err)
	}
	brokenJPEG := append([]byte(nil), fixtures["jpeg"]...)
	binary.BigEndian.PutUint16(brokenJPEG[4:6], 0xffff)
	brokenPNG := append([]byte(nil), fixtures["png"]...)
	binary.BigEndian.PutUint32(brokenPNG[8:12], 0xffffffff)
	brokenWebP := append([]byte(nil), fixtures["webp"]...)
	binary.LittleEndian.PutUint32(brokenWebP[4:8], 0xffffffff)
	brokenWebPChunk := append([]byte(nil), fixtures["webp"]...)
	binary.LittleEndian.PutUint32(brokenWebPChunk[16:20], 0xffffffff)
	for _, tc := range []struct {
		format string
		data   []byte
	}{{"jpeg", brokenJPEG}, {"png", brokenPNG}, {"webp", brokenWebP}, {"webp", brokenWebPChunk}} {
		if _, err := photoOrientation(tc.data, tc.format); !errors.Is(err, ErrPhotoInvalid) {
			t.Fatalf("%s container overflow accepted: %v", tc.format, err)
		}
	}
}
func FuzzPhotoOrientationBounds(f *testing.F) {
	f.Add(orientationTIFF(6, binary.LittleEndian), uint8(0))
	f.Add(orientationTIFF(8, binary.BigEndian), uint8(1))
	f.Add([]byte("RIFF\x04\x00\x00\x00WEBP"), uint8(2))
	f.Add([]byte("\x89PNG\r\n\x1a\n"), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, format uint8) {
		if len(data) > MaxPhotoBytes {
			t.Skip()
		}
		_, _ = photoTIFFOrientation(data)
		_, _ = photoOrientation(data, []string{"jpeg", "png", "webp"}[format%3])
	})
}
