package imgfmt

// 写 TIFF（LZW 压缩）。
//
// 为什么自己写：Go 标准库没有 TIFF 编码器；golang.org/x/image/tiff 能写 TIFF，但压缩方式
// 只支持"不压缩"和 Deflate，**没有 LZW**。而档案数字化那边要的就是 TIFF + LZW。
// 扫描仪本身也指望不上：Q400（Avision 机芯）报的 ICAP_COMPRESSION 只有 无压缩 / JPEG / G4，
// 同样没有 LZW。所以这一步只能在服务里做。
//
// 实现范围刚好够用：单页、条带式（strip）、LZW、无 predictor，三种像素格式——
// 1 位黑白、8 位灰度、24 位 RGB。扫描仪能出的就这几种。
//
// TIFF 的 LZW 和 GIF / compress/lzw 那个不是一回事：位序是高位在前，而且有个
// "early change"——码长要在字典还差一个就满的时候提前加宽。差这一位，别人的解码器
// 读出来就是花屏。

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

// TIFF 标签号，只列用到的。
const (
	tagImageWidth      = 256
	tagImageLength     = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagPhotometric     = 262
	tagStripOffsets    = 273
	tagSamplesPerPixel = 277
	tagRowsPerStrip    = 278
	tagStripByteCounts = 279
	tagXResolution     = 282
	tagYResolution     = 283
	tagPlanarConfig    = 284
	tagResolutionUnit  = 296
	tagSoftware        = 305
)

// 字段类型。
const (
	typeASCII    = 2
	typeShort    = 3
	typeLong     = 4
	typeRational = 5
)

const (
	compressionLZW = 5
	photoBilevel   = 1 // BlackIsZero：0 是黑，1 是白
	photoGray      = 1
	photoRGB       = 2
)

// stripTargetBytes 是每个条带的目标大小。TIFF 规范建议 8KB 左右，
// 太大在低内存机器上读图会吃力，太小则标签表变长。64KB 是个折中。
const stripTargetBytes = 64 << 10

// EncodeTIFFBytes 返回完整的 TIFF 内容。
// 文件头里要写 IFD 的偏移，而偏移要等数据写完才知道，所以在内存里拼好再一次性交出去。
// 扫描件单页几十 MB 顶天，内存扛得住。
func EncodeTIFFBytes(img image.Image, dpiX, dpiY int) ([]byte, error) {
	src, err := newTiffSource(img)
	if err != nil {
		return nil, err
	}
	if dpiX <= 0 {
		dpiX = 300
	}
	if dpiY <= 0 {
		dpiY = 300
	}

	rowBytes := src.rowBytes()
	rowsPerStrip := stripTargetBytes / max(rowBytes, 1)
	if rowsPerStrip < 1 {
		rowsPerStrip = 1
	}
	if rowsPerStrip > src.height {
		rowsPerStrip = src.height
	}
	stripCount := (src.height + rowsPerStrip - 1) / rowsPerStrip

	buf := &byteWriter{}
	head := make([]byte, 8)
	copy(head, "II")
	binary.LittleEndian.PutUint16(head[2:], 42)
	buf.Write(head) // 偏移先留空，下面回填

	offsets := make([]uint32, stripCount)
	counts := make([]uint32, stripCount)
	row := make([]byte, rowBytes)
	for s := 0; s < stripCount; s++ {
		offsets[s] = uint32(len(buf.b))
		enc := newLZWWriter(buf)
		for y := s * rowsPerStrip; y < (s+1)*rowsPerStrip && y < src.height; y++ {
			src.fillRow(row, y)
			if _, err := enc.Write(row); err != nil {
				return nil, err
			}
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
		counts[s] = uint32(len(buf.b)) - offsets[s]
	}

	ifdOffset := uint32(len(buf.b))
	if err := writeIFD(buf, src, dpiX, dpiY, rowsPerStrip, offsets, counts); err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(buf.b[4:8], ifdOffset)
	return buf.b, nil
}

// ---- 像素来源 ----

// tiffSource 把各种 image.Image 归一成"按 TIFF 要求逐行铺字节"的样子。
type tiffSource struct {
	img           image.Image
	width, height int
	bitsPerSample []uint16
	samples       int
	photometric   uint16
	bilevel       bool
	gray          bool
}

func newTiffSource(img image.Image) (*tiffSource, error) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("图片尺寸非法 %dx%d", b.Dx(), b.Dy())
	}
	src := &tiffSource{img: img, width: b.Dx(), height: b.Dy()}

	switch m := img.(type) {
	case *image.Paletted:
		if isBilevelPalette(m.Palette) {
			src.bilevel = true
			src.bitsPerSample = []uint16{1}
			src.samples = 1
			src.photometric = photoBilevel
			return src, nil
		}
	case *image.Gray:
		src.gray = true
		src.bitsPerSample = []uint16{8}
		src.samples = 1
		src.photometric = photoGray
		return src, nil
	}

	src.bitsPerSample = []uint16{8, 8, 8}
	src.samples = 3
	src.photometric = photoRGB
	return src, nil
}

func (s *tiffSource) rowBytes() int {
	if s.bilevel {
		return (s.width + 7) / 8
	}
	return s.width * s.samples
}

// fillRow 把第 y 行铺进 dst。dst 长度必须是 rowBytes()。
func (s *tiffSource) fillRow(dst []byte, y int) {
	b := s.img.Bounds()
	py := b.Min.Y + y

	switch {
	case s.bilevel:
		for i := range dst {
			dst[i] = 0
		}
		m := s.img.(*image.Paletted)
		white := whiteIndex(m.Palette)
		for x := 0; x < s.width; x++ {
			if m.ColorIndexAt(b.Min.X+x, py) == white {
				dst[x/8] |= 1 << uint(7-x%8) // 1 = 白（photometric 用 BlackIsZero）
			}
		}
	case s.gray:
		m := s.img.(*image.Gray)
		for x := 0; x < s.width; x++ {
			dst[x] = m.GrayAt(b.Min.X+x, py).Y
		}
	default:
		for x := 0; x < s.width; x++ {
			r, g, bb, _ := s.img.At(b.Min.X+x, py).RGBA()
			p := x * 3
			dst[p] = uint8(r >> 8)
			dst[p+1] = uint8(g >> 8)
			dst[p+2] = uint8(bb >> 8)
		}
	}
}

// isBilevelPalette 判断是不是"黑 + 白"两色调色板。
func isBilevelPalette(p color.Palette) bool {
	if len(p) != 2 {
		return false
	}
	var black, white bool
	for _, c := range p {
		r, g, b, _ := c.RGBA()
		switch {
		case r == 0 && g == 0 && b == 0:
			black = true
		case r>>8 == 0xff && g>>8 == 0xff && b>>8 == 0xff:
			white = true
		}
	}
	return black && white
}

// whiteIndex 返回调色板里白色那一项的下标。
func whiteIndex(p color.Palette) uint8 {
	for i, c := range p {
		r, g, b, _ := c.RGBA()
		if r>>8 == 0xff && g>>8 == 0xff && b>>8 == 0xff {
			return uint8(i)
		}
	}
	return 1
}

// ---- IFD ----

type ifdEntry struct {
	tag    uint16
	typ    uint16
	count  uint32
	inline []byte // 不超过 4 字节时直接放这里
	data   []byte // 超过 4 字节时放文件尾部，这里存原始内容
}

func writeIFD(w io.Writer, src *tiffSource, dpiX, dpiY, rowsPerStrip int, offsets, counts []uint32) error {
	entries := []ifdEntry{
		shortEntry(tagImageWidth, uint16(src.width)),
		shortEntry(tagImageLength, uint16(src.height)),
		bitsPerSampleEntry(src.bitsPerSample),
		shortEntry(tagCompression, compressionLZW),
		shortEntry(tagPhotometric, src.photometric),
		longArrayEntry(tagStripOffsets, offsets),
		shortEntry(tagSamplesPerPixel, uint16(src.samples)),
		longArrayEntry(tagRowsPerStrip, []uint32{uint32(rowsPerStrip)}),
		longArrayEntry(tagStripByteCounts, counts),
		rationalEntry(tagXResolution, uint32(dpiX), 1),
		rationalEntry(tagYResolution, uint32(dpiY), 1),
		shortEntry(tagPlanarConfig, 1),
		shortEntry(tagResolutionUnit, 2), // 2 = 英寸
		asciiEntry(tagSoftware, "scansvc"),
	}
	// 宽 / 高超过 65535 时 SHORT 放不下，换成 LONG。A0 幅面 600dpi 就能超。
	if src.width > 0xffff {
		entries[0] = longArrayEntry(tagImageWidth, []uint32{uint32(src.width)})
	}
	if src.height > 0xffff {
		entries[1] = longArrayEntry(tagImageLength, []uint32{uint32(src.height)})
	}

	// IFD 的条目必须按标签号升序，否则严格的解码器会拒收。
	sortEntries(entries)

	// 布局：条目数(2) + 条目(12*n) + 下一个 IFD 偏移(4) + 放不下的数据
	ifdStart := ifdOffsetOf(w)
	dataOffset := ifdStart + 2 + uint32(len(entries))*12 + 4

	buf := make([]byte, 0, 2+len(entries)*12+4)
	buf = appendUint16(buf, uint16(len(entries)))

	var extra []byte
	for _, e := range entries {
		buf = appendUint16(buf, e.tag)
		buf = appendUint16(buf, e.typ)
		buf = appendUint32(buf, e.count)
		if e.data == nil {
			inline := make([]byte, 4)
			copy(inline, e.inline)
			buf = append(buf, inline...)
			continue
		}
		buf = appendUint32(buf, dataOffset+uint32(len(extra)))
		extra = append(extra, e.data...)
		if len(extra)%2 == 1 { // 数据区按 2 字节对齐
			extra = append(extra, 0)
		}
	}
	buf = appendUint32(buf, 0) // 没有下一个 IFD

	if _, err := w.Write(buf); err != nil {
		return err
	}
	_, err := w.Write(extra)
	return err
}

// ifdOffsetOf 取当前写到哪了。
func ifdOffsetOf(w io.Writer) uint32 {
	if t, ok := w.(*byteWriter); ok {
		return uint32(len(t.b))
	}
	return 0
}

func shortEntry(tag uint16, v uint16) ifdEntry {
	inline := make([]byte, 2)
	binary.LittleEndian.PutUint16(inline, v)
	return ifdEntry{tag: tag, typ: typeShort, count: 1, inline: inline}
}

func bitsPerSampleEntry(bits []uint16) ifdEntry {
	if len(bits) == 1 {
		return shortEntry(tagBitsPerSample, bits[0])
	}
	data := make([]byte, 0, len(bits)*2)
	for _, b := range bits {
		data = appendUint16(data, b)
	}
	return ifdEntry{tag: tagBitsPerSample, typ: typeShort, count: uint32(len(bits)), data: data}
}

func longArrayEntry(tag uint16, vals []uint32) ifdEntry {
	if len(vals) == 1 {
		inline := make([]byte, 4)
		binary.LittleEndian.PutUint32(inline, vals[0])
		return ifdEntry{tag: tag, typ: typeLong, count: 1, inline: inline}
	}
	data := make([]byte, 0, len(vals)*4)
	for _, v := range vals {
		data = appendUint32(data, v)
	}
	return ifdEntry{tag: tag, typ: typeLong, count: uint32(len(vals)), data: data}
}

func rationalEntry(tag uint16, num, den uint32) ifdEntry {
	data := make([]byte, 0, 8)
	data = appendUint32(data, num)
	data = appendUint32(data, den)
	return ifdEntry{tag: tag, typ: typeRational, count: 1, data: data}
}

func asciiEntry(tag uint16, s string) ifdEntry {
	data := append([]byte(s), 0)
	if len(data) <= 4 {
		return ifdEntry{tag: tag, typ: typeASCII, count: uint32(len(data)), inline: data}
	}
	return ifdEntry{tag: tag, typ: typeASCII, count: uint32(len(data)), data: data}
}

func sortEntries(e []ifdEntry) {
	for i := 1; i < len(e); i++ {
		for j := i; j > 0 && e[j-1].tag > e[j].tag; j-- {
			e[j-1], e[j] = e[j], e[j-1]
		}
	}
}

func appendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// ---- 写入目标 ----

type byteWriter struct{ b []byte }

func (w *byteWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
