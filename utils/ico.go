package utils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// DecodeImage decodes an image with the standard library, falling back to
// decoding ICO files. Radio station favicons are frequently .ico resources
// (even when the name suggests otherwise), which Go's standard decoders ignore.
func DecodeImage(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return img, nil
	}
	return decodeICO(data)
}

const icoPNGSignature = "\x89PNG\r\n\x1a\n"

func decodeICO(data []byte) (image.Image, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("ico: data too short")
	}

	typ := binary.LittleEndian.Uint16(data[2:4])
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if typ != 1 || count <= 0 {
		return nil, fmt.Errorf("ico: not an icon resource")
	}

	const entrySize = 16
	if len(data) < 6+entrySize*count {
		return nil, fmt.Errorf("ico: truncated directory")
	}

	var (
		bestW, bestH, bestBpp, bestOffset, bestSize int
		have                                        bool
	)
	for i := 0; i < count; i++ {
		off := 6 + i*entrySize
		w := int(data[off])
		h := int(data[off+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		bpp := int(binary.LittleEndian.Uint16(data[off+6 : off+8]))
		size := int(binary.LittleEndian.Uint32(data[off+8 : off+12]))
		offset := int(binary.LittleEndian.Uint32(data[off+12 : off+16]))

		if !have || w*h > bestW*bestH || (w*h == bestW*bestH && bpp > bestBpp) {
			bestW, bestH, bestBpp, bestOffset, bestSize = w, h, bpp, offset, size
			have = true
		}
	}

	if !have || bestSize <= 0 || bestOffset < 0 || bestOffset+bestSize > len(data) {
		return nil, fmt.Errorf("ico: bad image entry")
	}

	imgData := data[bestOffset : bestOffset+bestSize]

	// Vista+ icons embed the PNG payload directly.
	if bytes.HasPrefix(imgData, []byte(icoPNGSignature)) {
		img, err := png.Decode(bytes.NewReader(imgData))
		if err != nil {
			return nil, fmt.Errorf("ico: png payload: %w", err)
		}
		return img, nil
	}

	return decodeICODIB(imgData, bestH)
}

func decodeICODIB(data []byte, dirHeight int) (image.Image, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("ico: dib too short")
	}

	headerSize := int(binary.LittleEndian.Uint32(data[0:4]))
	if headerSize < 40 {
		headerSize = 40
	}
	if len(data) < headerSize {
		return nil, fmt.Errorf("ico: truncated dib header")
	}

	width := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])

	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("ico: bad dimensions %dx%d", width, height)
	}
	if compression != 0 {
		return nil, fmt.Errorf("ico: unsupported compression %d", compression)
	}

	// ICO stores the AND mask, doubling the height.
	pixHeight := height
	if height == 2*dirHeight {
		pixHeight = dirHeight
	}

	clrUsed := binary.LittleEndian.Uint32(data[32:36])
	paletteSize := int(clrUsed)
	if paletteSize == 0 && bitCount <= 8 {
		paletteSize = 1 << uint(bitCount)
	}

	if bitCount <= 8 && len(data) < headerSize+paletteSize*4 {
		return nil, fmt.Errorf("ico: truncated palette")
	}

	palette := make([][4]byte, paletteSize)
	for i := 0; i < paletteSize; i++ {
		off := headerSize + i*4
		palette[i] = [4]byte{data[off+2], data[off+1], data[off], 0xff}
	}

	rowStride := (width*bitCount + 31) / 32 * 4
	pixOffset := headerSize + paletteSize*4
	if len(data) < pixOffset+rowStride*pixHeight {
		return nil, fmt.Errorf("ico: truncated pixel data")
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, pixHeight))
	for y := 0; y < pixHeight; y++ {
		srcRow := data[pixOffset+rowStride*(pixHeight-1-y):]
		for x := 0; x < width; x++ {
			var r, g, b, a uint8
			switch bitCount {
			case 32:
				i := x * 4
				b, g, r, a = srcRow[i], srcRow[i+1], srcRow[i+2], srcRow[i+3]
			case 24:
				i := x * 3
				b, g, r, a = srcRow[i], srcRow[i+1], srcRow[i+2], 255
			case 8:
				p := palette[srcRow[x]]
				r, g, b, a = p[0], p[1], p[2], p[3]
			case 4:
				idx := srcRow[x/2]
				if x%2 == 0 {
					idx >>= 4
				} else {
					idx &= 0x0f
				}
				p := palette[idx]
				r, g, b, a = p[0], p[1], p[2], p[3]
			case 1:
				idx := (srcRow[x/8] >> uint(7-x%8)) & 1
				p := palette[idx]
				r, g, b, a = p[0], p[1], p[2], p[3]
			default:
				return nil, fmt.Errorf("ico: unsupported bit depth %d", bitCount)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return img, nil
}
