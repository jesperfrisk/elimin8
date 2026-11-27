package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func smallPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	im.Set(0, 0, color.NRGBA{R: 255, A: 128})
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPreparePhotoStripsEXIFAndSniffsBytes(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 5)), nil); err != nil {
		t.Fatal(err)
	}
	// A valid little-endian EXIF TIFF: IFD0 has a GPSInfo pointer, whose GPS
	// IFD contains a latitude reference and three rational latitude values.
	tiff := make([]byte, 80)
	copy(tiff, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x8825)
	binary.LittleEndian.PutUint16(tiff[12:14], 4)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint32(tiff[18:22], 26)
	binary.LittleEndian.PutUint16(tiff[26:28], 2)
	binary.LittleEndian.PutUint16(tiff[28:30], 1)
	binary.LittleEndian.PutUint16(tiff[30:32], 2)
	binary.LittleEndian.PutUint32(tiff[32:36], 2)
	tiff[36] = 'N'
	binary.LittleEndian.PutUint16(tiff[40:42], 2)
	binary.LittleEndian.PutUint16(tiff[42:44], 5)
	binary.LittleEndian.PutUint32(tiff[44:48], 3)
	binary.LittleEndian.PutUint32(tiff[48:52], 56)
	for i, value := range []uint32{59, 20, 0} {
		binary.LittleEndian.PutUint32(tiff[56+i*8:60+i*8], value)
		binary.LittleEndian.PutUint32(tiff[60+i*8:64+i*8], 1)
	}
	metadata := append([]byte("Exif\x00\x00"), tiff...)
	source := append([]byte(nil), encoded.Bytes()[:2]...)
	source = append(source, 0xff, 0xe1, byte((len(metadata)+2)>>8), byte(len(metadata)+2))
	source = append(source, metadata...)
	source = append(source, encoded.Bytes()[2:]...)
	result, err := preparePhoto(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{[]byte("Exif"), []byte("GPS"), []byte("private location")} {
		if bytes.Contains(result, secret) {
			t.Fatal("source metadata preserved")
		}
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(result))
	if err != nil || cfg.Width != 8 || cfg.Height != 5 {
		t.Fatalf("bad JPEG: %+v %v", cfg, err)
	}
	// A PNG is accepted by its bytes without relying on the upload's declared MIME/extension.
	if _, err = preparePhoto(context.Background(), smallPNG(t)); err != nil {
		t.Fatal(err)
	}
	// Valid lossy 1x1 WebP fixture, decoded by the official x/image decoder.
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = preparePhoto(context.Background(), webp); err != nil {
		t.Fatalf("valid WebP: %v", err)
	}
}

func TestPreparePhotoRejectsMalformedAndOversizedInputs(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrPhotoInvalid},
		{"HTML", []byte("<html>not an image</html>"), ErrPhotoUnsupported},
		{"oversized", make([]byte, MaxPhotoBytes+1), ErrPhotoTooLarge},
		{"truncated JPEG", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0}, ErrPhotoInvalid},
	}
	bomb := smallPNG(t)
	binary.BigEndian.PutUint32(bomb[16:20], 10000)
	binary.BigEndian.PutUint32(bomb[20:24], 10000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	tests = append(tests, struct {
		name string
		data []byte
		want error
	}{"dimension bomb", bomb, ErrPhotoTooLarge})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := preparePhoto(ctx, tc.data); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := preparePhoto(cancelled, smallPNG(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled decode: %v", err)
	}
}
