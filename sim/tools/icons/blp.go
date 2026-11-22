package icons

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

// DecodeBLP decodes the first mipmap of a BLP2 texture as Pillow's BlpImagePlugin does (the icons were
// decoded by Pillow before; the PNGs in the cache stay what they are): a palette of BGRA with the palette's
// alpha, or DXT1 (with and without its 1-bit alpha), DXT3 or DXT5. The pixels go through the same flat
// buffer Pillow fills, so even its odd cases (a width that is no multiple of 4, DXT3/5 without the alpha flag)
// come out as Pillow's. BLP1 and the raw BGRA encoding are not supported, as in Pillow for BLP2's raw BGRA.
func DecodeBLP(data []byte) (*image.NRGBA, error) {
	if len(data) < 20 {
		return nil, errors.New("truncated BLP header")
	}
	switch string(data[:4]) {
	case "BLP2":
	case "BLP1":
		return nil, errors.New("BLP1 is not supported")
	default:
		return nil, fmt.Errorf("bad BLP magic %q", data[:4])
	}
	compression := int32(binary.LittleEndian.Uint32(data[4:]))
	encoding, alpha, alphaEncoding := int8(data[8]), int8(data[9]) != 0, int8(data[10])
	w, h := int(binary.LittleEndian.Uint32(data[12:])), int(binary.LittleEndian.Uint32(data[16:]))
	if w <= 0 || h <= 0 || w*h > 1<<24 {
		return nil, fmt.Errorf("bad BLP size %dx%d", w, h)
	}
	const head = 20 + 16*4*2 + 256*4 // header, mipmap offsets and lengths, palette
	if len(data) < head {
		return nil, errors.New("truncated BLP file")
	}
	offset := int(binary.LittleEndian.Uint32(data[20:]))
	length := int(binary.LittleEndian.Uint32(data[20+16*4:]))
	palette := data[20+16*4*2 : head]
	read := func(n int) ([]byte, error) { // ImageFile._safe_read from the current position
		if n < 0 || offset > len(data) || len(data)-offset < n {
			return nil, errors.New("truncated BLP file")
		}
		b := data[offset : offset+n]
		offset += n
		return b, nil
	}
	if compression != 1 {
		return nil, fmt.Errorf("unknown BLP compression %d", compression)
	}
	var flat []byte // what Pillow hands to set_as_raw
	switch encoding {
	case 1: // palette indices, one byte per pixel; Pillow takes the alpha from the palette
		idx, err := read(length)
		if err != nil {
			return nil, err
		}
		for _, i := range idx {
			b, g, r, a := palette[4*int(i)], palette[4*int(i)+1], palette[4*int(i)+2], palette[4*int(i)+3]
			flat = append(flat, r, g, b)
			if alpha {
				flat = append(flat, a)
			}
		}
	case 2:
		var block int
		var decode func(row []byte) [4][]byte
		switch alphaEncoding {
		case 0:
			block, decode = 8, func(row []byte) [4][]byte { return dxt1(row, alpha) }
		case 1:
			block, decode = 16, dxt3
		case 7:
			block, decode = 16, dxt5
		default:
			return nil, fmt.Errorf("unsupported alpha encoding %d", alphaEncoding)
		}
		lineSize := (w + 3) / 4 * block
		for y := 0; y < (h+3)/4; y++ {
			row, err := read(lineSize)
			if err != nil {
				return nil, err
			}
			for _, line := range decode(row) {
				flat = append(flat, line...)
			}
		}
	default:
		return nil, fmt.Errorf("unknown BLP encoding %d", encoding)
	}
	bpp := 3
	if alpha {
		bpp = 4
	}
	if len(flat) < w*h*bpp {
		return nil, errors.New("not enough image data")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for p := 0; p < w*h; p++ {
		px := flat[p*bpp:]
		a := byte(255)
		if alpha {
			a = px[3]
		}
		copy(img.Pix[p*4:], []byte{px[0], px[1], px[2], a})
	}
	return img, nil
}

func unpack565(c uint16) (int, int, int) {
	return int(c>>11&0x1f) << 3, int(c>>5&0x3f) << 2, int(c&0x1f) << 3
}

// colors are the four colors of a block: the two stored, two thirds between them (DXT3, DXT5, and DXT1 when
// color0 > color1), or for DXT1 otherwise their middle and black.
func colors(c0, c1 uint16, dxt1 bool) (out [4][3]int, transparent bool) {
	r0, g0, b0 := unpack565(c0)
	r1, g1, b1 := unpack565(c1)
	out[0], out[1] = [3]int{r0, g0, b0}, [3]int{r1, g1, b1}
	if !dxt1 || c0 > c1 {
		out[2] = [3]int{(2*r0 + r1) / 3, (2*g0 + g1) / 3, (2*b0 + b1) / 3}
		out[3] = [3]int{(2*r1 + r0) / 3, (2*g1 + g0) / 3, (2*b1 + b0) / 3}
		return out, false
	}
	out[2] = [3]int{(r0 + r1) / 2, (g0 + g1) / 2, (b0 + b1) / 2}
	return out, true
}

func dxt1(data []byte, alpha bool) (ret [4][]byte) {
	for i := 0; i+8 <= len(data)/8*8; i += 8 {
		c0, c1 := binary.LittleEndian.Uint16(data[i:]), binary.LittleEndian.Uint16(data[i+2:])
		bits := binary.LittleEndian.Uint32(data[i+4:])
		cs, black := colors(c0, c1, true)
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				control := bits & 3
				bits >>= 2
				c, a := cs[control], byte(0xff)
				if control == 3 && black {
					c, a = [3]int{}, 0
				}
				ret[j] = append(ret[j], byte(c[0]), byte(c[1]), byte(c[2]))
				if alpha {
					ret[j] = append(ret[j], a)
				}
			}
		}
	}
	return ret
}

func dxt3(data []byte) (ret [4][]byte) {
	for i := 0; i+16 <= len(data)/16*16; i += 16 {
		block := data[i : i+16]
		cs, _ := colors(binary.LittleEndian.Uint16(block[8:]), binary.LittleEndian.Uint16(block[10:]), false)
		code := binary.LittleEndian.Uint32(block[12:])
		for j := 0; j < 4; j++ {
			high := false
			for k := 0; k < 4; k++ {
				a := block[(4*j+k)/2]
				if high {
					a >>= 4
				} else {
					a &= 0xf
				}
				high = !high
				c := cs[code>>(2*(4*j+k))&3]
				ret[j] = append(ret[j], byte(c[0]), byte(c[1]), byte(c[2]), a*17)
			}
		}
	}
	return ret
}

func dxt5(data []byte) (ret [4][]byte) {
	for i := 0; i+16 <= len(data)/16*16; i += 16 {
		block := data[i : i+16]
		a0, a1 := int(block[0]), int(block[1])
		bits := block[2:8]
		code1 := uint64(bits[2]) | uint64(bits[3])<<8 | uint64(bits[4])<<16 | uint64(bits[5])<<24
		code2 := uint64(bits[0]) | uint64(bits[1])<<8
		cs, _ := colors(binary.LittleEndian.Uint16(block[8:]), binary.LittleEndian.Uint16(block[10:]), false)
		code := binary.LittleEndian.Uint32(block[12:])
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				index := 3 * (4*j + k)
				var ac int
				switch {
				case index <= 12:
					ac = int(code2 >> index & 7)
				case index == 15:
					ac = int(code2>>15 | code1<<1&6)
				default:
					ac = int(code1 >> (index - 16) & 7)
				}
				var a int
				switch {
				case ac == 0:
					a = a0
				case ac == 1:
					a = a1
				case a0 > a1:
					a = ((8-ac)*a0 + (ac-1)*a1) / 7
				case ac == 6:
					a = 0
				case ac == 7:
					a = 255
				default:
					a = ((6-ac)*a0 + (ac-1)*a1) / 5
				}
				c := cs[code>>(2*(4*j+k))&3]
				ret[j] = append(ret[j], byte(c[0]), byte(c[1]), byte(c[2]), byte(a))
			}
		}
	}
	return ret
}
