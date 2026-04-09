package qoi

import (
	"bufio"
	"encoding/binary"
	"image"
	"image/draw"
	"io"
)

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

type encoder struct {
	w     *bufio.Writer
	img   *image.NRGBA
	prev  rgba
	index [64]rgba
	buf   [5]byte
	run   int
}

func (e *encoder) writeHeader() error {
	channels := uint8(3)
	if hasAlpha(e.img.Pix) {
		channels = 4
	}

	var header [14]byte
	copy(header[0:4], "qoif")
	binary.BigEndian.PutUint32(header[4:8], uint32(e.img.Bounds().Dx()))
	binary.BigEndian.PutUint32(header[8:12], uint32(e.img.Bounds().Dy()))
	header[12] = channels
	header[13] = 0 // colorspace: sRGB
	_, err := e.w.Write(header[:])
	return err
}

func (e *encoder) selectChunk(cur rgba) []byte {
	h := (cur[0]*3 + cur[1]*5 + cur[2]*7 + cur[3]*11) % 64
	defer func() { e.index[h] = cur }()

	if e.index[h] == cur { // INDEX
		e.buf[0] = h
		return e.buf[:1]
	}

	if cur[3] != e.prev[3] { // RGBA
		e.buf[0] = 0xff
		copy(e.buf[1:5], cur[:])
		return e.buf[:5]
	}

	dr := cur[0] - e.prev[0]
	dg := cur[1] - e.prev[1]
	db := cur[2] - e.prev[2]

	if dr+2 < 4 && dg+2 < 4 && db+2 < 4 { // DIFF
		e.buf[0] = 0x40 | (dr+2)<<4 | (dg+2)<<2 | (db + 2)
		return e.buf[:1]
	}

	drDg, dbDg := dr-dg, db-dg
	if dg+32 < 64 && drDg+8 < 16 && dbDg+8 < 16 { // LUMA
		e.buf[0] = 0x80 | (dg + 32)
		e.buf[1] = (drDg+8)<<4 | (dbDg + 8)
		return e.buf[:2]
	}

	// RGB
	e.buf[0] = 0xfe
	copy(e.buf[1:4], cur[0:3])
	return e.buf[:4]
}

func (e *encoder) flushRun() {
	if e.run > 0 {
		e.w.WriteByte(0xc0 | byte(e.run-1))
		e.run = 0
	}
}

func (e *encoder) encode() error {
	e.prev = rgba{0, 0, 0, 255}
	pixels := e.img.Pix

	for len(pixels) > 0 {
		var cur rgba
		copy(cur[:], pixels[:4])
		pixels = pixels[4:]

		if cur == e.prev {
			e.run++
			if e.run == 62 || len(pixels) == 0 {
				e.flushRun()
			}
			continue
		}

		e.flushRun()
		e.w.Write(e.selectChunk(cur))
		e.prev = cur
	}

	// End marker
	_, err := e.w.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	return err
}

func Encode(w io.Writer, img image.Image) error {
	e := &encoder{
		w:   bufio.NewWriter(w),
		img: toNRGBA(img),
	}
	if err := e.writeHeader(); err != nil {
		return err
	}
	if err := e.encode(); err != nil {
		return err
	}
	return e.w.Flush()
}

func hasAlpha(pix []uint8) bool {
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 255 {
			return true
		}
	}
	return false
}
