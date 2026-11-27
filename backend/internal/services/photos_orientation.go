package services

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

const maxPhotoEXIFBytes = 256 * 1024
const maxPhotoIFDEntries = 512

// photoOrientation reads only EXIF's primary-image orientation, never GPS,
// thumbnail directories or arbitrary TIFF pointers. Container and TIFF offsets
// are checked before slicing; parsing allocates no metadata-sized buffers.
func photoOrientation(data []byte, format string) (int, error) {
	orientation, found := 1, false
	readEXIF := func(payload []byte) error {
		if found {
			return ErrPhotoInvalid
		}
		found = true
		var err error
		orientation, err = photoTIFFOrientation(payload)
		return err
	}
	switch format {
	case "jpeg":
		if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
			return 0, ErrPhotoInvalid
		}
		for pos := 2; pos < len(data); {
			if data[pos] != 0xff {
				return 0, ErrPhotoInvalid
			}
			for pos < len(data) && data[pos] == 0xff {
				pos++
			}
			if pos == len(data) {
				return 0, ErrPhotoInvalid
			}
			marker := data[pos]
			pos++
			if marker == 0xda || marker == 0xd9 {
				break
			} // metadata precedes scan data
			if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
				continue
			}
			if pos+2 > len(data) {
				return 0, ErrPhotoInvalid
			}
			length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			if length < 2 || length > len(data)-pos {
				return 0, ErrPhotoInvalid
			}
			payload := data[pos+2 : pos+length]
			if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				if err := readEXIF(payload); err != nil {
					return 0, err
				}
			}
			pos += length
		}
	case "png":
		if len(data) < 8 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
			return 0, ErrPhotoInvalid
		}
		for pos := 8; pos < len(data); {
			if len(data)-pos < 12 {
				return 0, ErrPhotoInvalid
			}
			length := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
			end := uint64(pos) + 12 + length
			if end > uint64(len(data)) {
				return 0, ErrPhotoInvalid
			}
			kind := string(data[pos+4 : pos+8])
			if kind == "eXIf" {
				if err := readEXIF(data[pos+8 : int(end)-4]); err != nil {
					return 0, err
				}
			}
			pos = int(end)
			if kind == "IEND" {
				break
			}
		}
	case "webp":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return 0, ErrPhotoInvalid
		}
		end := uint64(binary.LittleEndian.Uint32(data[4:8])) + 8
		if end < 12 || end > uint64(len(data)) {
			return 0, ErrPhotoInvalid
		}
		for pos := uint64(12); pos < end; {
			if end-pos < 8 {
				return 0, ErrPhotoInvalid
			}
			length := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			next := pos + 8 + length + length%2 // RIFF chunks include an even-byte pad
			if next > end {
				return 0, ErrPhotoInvalid
			}
			if string(data[pos:pos+4]) == "EXIF" {
				if err := readEXIF(data[pos+8 : pos+8+length]); err != nil {
					return 0, err
				}
			}
			pos = next
		}
	default:
		return 0, ErrPhotoInvalid
	}
	return orientation, nil
}

func photoTIFFOrientation(data []byte) (int, error) {
	// JPEG requires this prefix. Some WebP writers include it as well; PNG's
	// eXIf chunk ordinarily starts with the TIFF header directly.
	if bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
		data = data[6:]
	}
	if len(data) < 8 || len(data) > maxPhotoEXIFBytes {
		return 0, ErrPhotoInvalid
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, ErrPhotoInvalid
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, ErrPhotoInvalid
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset+2 > uint64(len(data)) {
		return 0, ErrPhotoInvalid
	}
	count := uint64(order.Uint16(data[offset : offset+2]))
	if count > maxPhotoIFDEntries || offset+2+count*12+4 > uint64(len(data)) {
		return 0, ErrPhotoInvalid
	}
	orientation, found := 1, false
	for entry := offset + 2; entry < offset+2+count*12; entry += 12 {
		if order.Uint16(data[entry:entry+2]) != 0x0112 {
			continue
		}
		if found || order.Uint16(data[entry+2:entry+4]) != 3 || order.Uint32(data[entry+4:entry+8]) != 1 {
			return 0, ErrPhotoInvalid
		}
		found = true
		orientation = int(order.Uint16(data[entry+8 : entry+10]))
		if orientation < 1 || orientation > 8 {
			return 0, ErrPhotoInvalid
		}
	}
	return orientation, nil
}

// orientedPhoto remaps coordinates during scaling, avoiding a second decoded
// image allocation for photos up to the existing 25-megapixel limit.
type orientedPhoto struct {
	source      image.Image
	orientation int
}

func (p orientedPhoto) ColorModel() color.Model { return p.source.ColorModel() }
func (p orientedPhoto) Bounds() image.Rectangle {
	bounds := p.source.Bounds()
	if p.orientation >= 5 {
		return image.Rect(0, 0, bounds.Dy(), bounds.Dx())
	}
	return image.Rect(0, 0, bounds.Dx(), bounds.Dy())
}
func (p orientedPhoto) At(x, y int) color.Color {
	if !image.Pt(x, y).In(p.Bounds()) {
		return color.RGBA{}
	}
	bounds := p.source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	switch p.orientation {
	case 2:
		x = width - 1 - x
	case 3:
		x, y = width-1-x, height-1-y
	case 4:
		y = height - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, height-1-x
	case 7:
		x, y = width-1-y, height-1-x
	case 8:
		x, y = width-1-y, x
	}
	return p.source.At(bounds.Min.X+x, bounds.Min.Y+y)
}
