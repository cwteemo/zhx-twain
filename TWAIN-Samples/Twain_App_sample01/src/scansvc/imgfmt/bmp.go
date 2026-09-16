package imgfmt

// 解 DLL 吐出来的 BMP。
//
// 为什么自己解而不用 golang.org/x/image/bmp：那个包的新版本要求 Go 1.23+，
// 引进来会把 go.mod 的版本要求抬上去，编译机器的 Go 版本就得跟着升。
// FreeImage 存出来的是标准未压缩 BMP，解析它并不复杂。
//
// 和早期版本的区别：**按原始位深还原图片类型**，不再一律转成 RGBA。
// 黑白扫描出来的 1 位图转成 RGBA 再存 TIFF，会从几十 KB 变成几十 MB。

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
)

// Image 是解出来的图，连同扫描分辨率。
// DPI 来自 BMP 头里的 biXPelsPerMeter，转出去的 TIFF / JPEG 要把它写回去——
// 档案数字化验收要看图片里记录的 DPI，丢了等于白扫。
type Image struct {
	Image image.Image
	DPIX  int
	DPIY  int
}

// DecodeBMPFile 解一张未压缩 BMP。
// 覆盖 FreeImage 会写出的几种：1 位黑白、4/8 位调色板或灰度、24 位真彩、32 位带填充字节。
func DecodeBMPFile(path string) (Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Image{}, err
	}
	return DecodeBMP(raw)
}

// DecodeBMP 解内存里的一张 BMP。
func DecodeBMP(raw []byte) (Image, error) {
	if len(raw) < 54 {
		return Image{}, fmt.Errorf("文件太小，不是 BMP（%d 字节）", len(raw))
	}
	if raw[0] != 'B' || raw[1] != 'M' {
		return Image{}, fmt.Errorf("缺少 BM 文件头")
	}

	dataOffset := binary.LittleEndian.Uint32(raw[10:14])
	headerSize := binary.LittleEndian.Uint32(raw[14:18])
	if headerSize < 40 {
		return Image{}, fmt.Errorf("不支持的 DIB 头（%d 字节），只认 BITMAPINFOHEADER 及以上", headerSize)
	}

	width := int(int32(binary.LittleEndian.Uint32(raw[18:22])))
	rawHeight := int(int32(binary.LittleEndian.Uint32(raw[22:26])))
	bitCount := int(binary.LittleEndian.Uint16(raw[28:30]))
	compression := binary.LittleEndian.Uint32(raw[30:34])
	dpiX := pixelsPerMeterToDPI(int32(binary.LittleEndian.Uint32(raw[38:42])))
	dpiY := pixelsPerMeterToDPI(int32(binary.LittleEndian.Uint32(raw[42:46])))

	if compression != 0 {
		// BI_RGB 之外的（RLE、位域）这里不处理，交给调用方回退成原始 BMP。
		return Image{}, fmt.Errorf("不支持的压缩方式 %d", compression)
	}
	if width <= 0 || rawHeight == 0 {
		return Image{}, fmt.Errorf("尺寸非法 %dx%d", width, rawHeight)
	}

	// 高度为负表示自顶向下存储，正数是自底向上（BMP 的常态）。
	height := rawHeight
	topDown := false
	if height < 0 {
		height = -height
		topDown = true
	}

	// 调色板：位深 <= 8 时紧跟在 DIB 头后面，每项 4 字节 BGRA。
	var palette color.Palette
	if bitCount <= 8 {
		count := int(binary.LittleEndian.Uint32(raw[46:50])) // biClrUsed
		if count == 0 {
			count = 1 << uint(bitCount)
		}
		palStart := 14 + int(headerSize)
		if palStart+count*4 > len(raw) {
			return Image{}, fmt.Errorf("调色板越界")
		}
		palette = make(color.Palette, count)
		for i := 0; i < count; i++ {
			p := palStart + i*4
			palette[i] = color.RGBA{R: raw[p+2], G: raw[p+1], B: raw[p], A: 0xff}
		}
	}

	// 每行按 4 字节对齐。
	rowSize := ((width*bitCount + 31) / 32) * 4
	need := int(dataOffset) + rowSize*height
	if need > len(raw) {
		return Image{}, fmt.Errorf("像素数据不完整：需要 %d 字节，实际 %d", need, len(raw))
	}

	out := Image{DPIX: dpiX, DPIY: dpiY}
	rect := image.Rect(0, 0, width, height)

	// 行号换算：自底向上时文件里的第 0 行是图像的最后一行。
	srcRowOf := func(y int) []byte {
		srcRow := y
		if !topDown {
			srcRow = height - 1 - y
		}
		return raw[int(dataOffset)+srcRow*rowSize:]
	}

	switch {
	case bitCount <= 8 && isGrayPalette(palette):
		// 黑白和灰度扫描走这里：存成 Gray（1 位的另外存 Paletted，见下），
		// 后面转 TIFF / JPEG 时就能保持单通道，体积差一个数量级。
		if bitCount == 1 {
			img := image.NewPaletted(rect, color.Palette{palette[0], palette[len(palette)-1]})
			for y := 0; y < height; y++ {
				row := srcRowOf(y)
				for x := 0; x < width; x++ {
					img.SetColorIndex(x, y, (row[x/8]>>uint(7-x%8))&1)
				}
			}
			out.Image = img
			return out, nil
		}
		img := image.NewGray(rect)
		for y := 0; y < height; y++ {
			row := srcRowOf(y)
			for x := 0; x < width; x++ {
				idx := indexAt(row, x, bitCount)
				c := paletteAt(palette, idx)
				img.SetGray(x, y, color.Gray{Y: c.R})
			}
		}
		out.Image = img
		return out, nil

	case bitCount <= 8:
		img := image.NewPaletted(rect, palette)
		for y := 0; y < height; y++ {
			row := srcRowOf(y)
			for x := 0; x < width; x++ {
				img.SetColorIndex(x, y, uint8(indexAt(row, x, bitCount)))
			}
		}
		out.Image = img
		return out, nil

	case bitCount == 24 || bitCount == 32:
		step := bitCount / 8
		img := image.NewRGBA(rect)
		for y := 0; y < height; y++ {
			row := srcRowOf(y)
			for x := 0; x < width; x++ {
				p := x * step
				// 32 位 BMP 的第 4 字节多数是填充而非有效 alpha，一律当不透明处理，
				// 否则扫描件会整张变透明。
				img.SetRGBA(x, y, color.RGBA{R: row[p+2], G: row[p+1], B: row[p], A: 0xff})
			}
		}
		out.Image = img
		return out, nil

	default:
		return Image{}, fmt.Errorf("不支持的位深 %d", bitCount)
	}
}

// indexAt 取第 x 个像素的调色板下标（位深 1 / 4 / 8）。
func indexAt(row []byte, x, bitCount int) int {
	switch bitCount {
	case 1:
		return int((row[x/8] >> uint(7-x%8)) & 1)
	case 4:
		b := row[x/2]
		if x%2 == 1 {
			return int(b & 0x0f)
		}
		return int(b >> 4)
	default:
		return int(row[x])
	}
}

func paletteAt(palette color.Palette, idx int) color.RGBA {
	if idx >= 0 && idx < len(palette) {
		if c, ok := palette[idx].(color.RGBA); ok {
			return c
		}
	}
	return color.RGBA{A: 0xff}
}

// isGrayPalette 判断调色板是不是纯灰阶（R=G=B）。扫描仪的黑白 / 灰度图都是这样。
func isGrayPalette(palette color.Palette) bool {
	if len(palette) == 0 {
		return false
	}
	for _, c := range palette {
		rc, ok := c.(color.RGBA)
		if !ok || rc.R != rc.G || rc.G != rc.B {
			return false
		}
	}
	return true
}

// pixelsPerMeterToDPI 把 BMP 头里的"每米像素数"换算成 DPI。0 表示没写。
func pixelsPerMeterToDPI(ppm int32) int {
	if ppm <= 0 {
		return 0
	}
	return int(math.Round(float64(ppm) * 0.0254))
}
