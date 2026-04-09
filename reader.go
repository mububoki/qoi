package qoi

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

// rgba holds a pixel in R, G, B, A order, like image.NRGBA.Pix.
type rgba [4]uint8

type decoder struct {
	r          *bufio.Reader
	width      int
	height     int
	channels   uint8
	colorspace uint8
	prev       rgba
	index      [64]rgba
	img        *image.NRGBA
	b          byte
}

func (d *decoder) hashPixel() uint8 {
	return (d.prev[0]*3 + d.prev[1]*5 + d.prev[2]*7 + d.prev[3]*11) % 64
}

func (d *decoder) readHeader() error {
	var header [14]byte
	if _, err := io.ReadFull(d.r, header[:]); err != nil {
		return fmt.Errorf("qoi: reading header: %w", err)
	}

	if string(header[0:4]) != "qoif" {
		return fmt.Errorf("qoi: invalid magic bytes")
	}

	d.width = int(binary.BigEndian.Uint32(header[4:8]))
	d.height = int(binary.BigEndian.Uint32(header[8:12]))
	d.channels = header[12]
	d.colorspace = header[13]

	if d.channels != 3 && d.channels != 4 {
		return fmt.Errorf("qoi: invalid channels: %d", d.channels)
	}
	if d.colorspace > 1 {
		return fmt.Errorf("qoi: invalid colorspace: %d", d.colorspace)
	}

	return nil
}

func (d *decoder) decode() (image.Image, error) {
	d.prev = rgba{0, 0, 0, 255}
	d.img = image.NewNRGBA(image.Rect(0, 0, d.width, d.height))
	pixels := d.img.Pix

	for len(pixels) > 0 {
		tag, err := d.r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("qoi: unexpected end of data: %w", err)
		}

		switch {
		case tag == 0xfe: // RGB
			if _, err = io.ReadFull(d.r, d.prev[0:3]); err != nil {
				return nil, fmt.Errorf("qoi: unexpected end of data: %w", err)
			}

		case tag == 0xff: // RGBA
			if _, err = io.ReadFull(d.r, d.prev[:]); err != nil {
				return nil, fmt.Errorf("qoi: unexpected end of data: %w", err)
			}

		case tag>>6 == 0b00: // INDEX
			d.prev = d.index[tag&0b00_111111]

		case tag>>6 == 0b01: // DIFF
			d.prev[0] += (tag>>4)&0x03 - 2
			d.prev[1] += (tag>>2)&0x03 - 2
			d.prev[2] += tag&0x03 - 2

		case tag>>6 == 0b10: // LUMA
			dg := (tag & 0b00_111111) - 32
			d.b, err = d.r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("qoi: unexpected end of data: %w", err)
			}
			d.prev[0] += dg + (d.b>>4)&0x0f - 8
			d.prev[1] += dg
			d.prev[2] += dg + d.b&0x0f - 8

		case tag>>6 == 0b11: // RUN
			run := int(tag&0b00_111111) + 1
			d.index[d.hashPixel()] = d.prev
			copy(pixels[:4], d.prev[:])
			for i := 4; i < run*4; i += 4 {
				copy(pixels[i:i+4], pixels[:4])
			}
			pixels = pixels[run*4:]
			continue

		default:
			return nil, fmt.Errorf("qoi: invalid tag: 0x%02x", tag)
		}

		d.index[d.hashPixel()] = d.prev
		copy(pixels[:4], d.prev[:])
		pixels = pixels[4:]
	}

	var end [8]byte
	if _, err := io.ReadFull(d.r, end[:]); err != nil {
		return nil, fmt.Errorf("qoi: reading end marker: %w", err)
	}
	if end != [8]byte{0, 0, 0, 0, 0, 0, 0, 1} {
		return nil, fmt.Errorf("qoi: invalid end marker")
	}
	return d.img, nil
}

func Decode(r io.Reader) (image.Image, error) {
	d := &decoder{r: bufio.NewReader(r)}
	if err := d.readHeader(); err != nil {
		return nil, err
	}
	return d.decode()
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	d := &decoder{r: bufio.NewReader(r)}
	if err := d.readHeader(); err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: color.NRGBAModel,
		Width:      d.width,
		Height:     d.height,
	}, nil
}

func init() {
	image.RegisterFormat("qoi", "qoif", Decode, DecodeConfig)
}
