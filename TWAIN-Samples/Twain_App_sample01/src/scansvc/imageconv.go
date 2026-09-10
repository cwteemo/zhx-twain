package main

// 图片格式转换。
//
// DLL 只会吐 BMP——zhx_Scan 里靠 "*.bmp" 这个通配符比对扫描前后的目录来认产物，
// 换格式就认不出来了。而前端要的是 png/jpg（默认 png），单张 A4 300dpi 彩色 BMP
// 十几 MB，原样回传也不合适。所以在 Go 这边转。
//
// 为什么自己解 BMP 而不用 golang.org/x/image/bmp：那个包的新版本要求 Go 1.23+，
// 引进来会把 go.mod 的版本要求抬上去，编译机器的 Go 版本就得跟着升。
// FreeImage 存出来的是标准未压缩 BMP，解析它并不复杂。

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// jpegQuality 是转 JPEG 时的质量。85 是肉眼几乎无损、体积又降得下来的常用档位。
const jpegQuality = 85

// convertScan 把扫描产物转成 ext 指定的格式，返回新文件路径。
// ext 为空、为 bmp、或不认识的格式时原样返回源文件——扩展名和实际内容必须一致，
// 前端是从 URL 结尾取扩展名再报给后端的，糊弄不得。
func convertScan(srcPath, ext string) (string, error) {
	ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	switch ext {
	case "png", "jpg", "jpeg":
	default:
		return srcPath, nil
	}

	img, err := decodeBMPFile(srcPath)
	if err != nil {
		// 解不动就退回原图，别让格式转换挡住扫描本身。
		return srcPath, fmt.Errorf("解码 BMP 失败，回退为原始 BMP: %w", err)
	}

	dstPath := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + "." + ext
	f, err := os.Create(dstPath)
	if err != nil {
		return srcPath, fmt.Errorf("创建 %s 失败，回退为原始 BMP: %w", dstPath, err)
	}
	defer f.Close()

	if ext == "png" {
		err = png.Encode(f, img)
	} else {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		os.Remove(dstPath)
		return srcPath, fmt.Errorf("编码 %s 失败，回退为原始 BMP: %w", ext, err)
	}

	// 原 BMP 留着没意义，转换成功就删掉——一张十几 MB，连续扫几百页很快就把盘吃满。
	os.Remove(srcPath)
	return dstPath, nil
}

// decodeBMPFile 解一张未压缩 BMP。
// 覆盖 FreeImage 会写出的几种：1 位黑白、8 位灰度/调色板、24 位真彩、32 位带 alpha。
func decodeBMPFile(path string) (image.Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 54 {
		return nil, fmt.Errorf("文件太小，不是 BMP（%d 字节）", len(raw))
	}
	if raw[0] != 'B' || raw[1] != 'M' {
		return nil, fmt.Errorf("缺少 BM 文件头")
	}

	dataOffset := binary.LittleEndian.Uint32(raw[10:14])
	headerSize := binary.LittleEndian.Uint32(raw[14:18])
	if headerSize < 40 {
		return nil, fmt.Errorf("不支持的 DIB 头（%d 字节），只认 BITMAPINFOHEADER 及以上", headerSize)
	}

	width := int(int32(binary.LittleEndian.Uint32(raw[18:22])))
	rawHeight := int(int32(binary.LittleEndian.Uint32(raw[22:26])))
	bitCount := int(binary.LittleEndian.Uint16(raw[28:30]))
	compression := binary.LittleEndian.Uint32(raw[30:34])

	if compression != 0 {
		// BI_RGB 之外的（RLE、位域）这里不处理，交给调用方回退成原始 BMP。
		return nil, fmt.Errorf("不支持的压缩方式 %d", compression)
	}
	if width <= 0 || rawHeight == 0 {
		return nil, fmt.Errorf("尺寸非法 %dx%d", width, rawHeight)
	}

	// 高度为负表示自顶向下存储，正数是自底向上（BMP 的常态）。
	height := rawHeight
	topDown := false
	if height < 0 {
		height = -height
		topDown = true
	}

	// 调色板：位深 <= 8 时紧跟在 DIB 头后面，每项 4 字节 BGRA。
	var palette []color.RGBA
	if bitCount <= 8 {
		count := int(binary.LittleEndian.Uint32(raw[46:50])) // biClrUsed
		if count == 0 {
			count = 1 << uint(bitCount)
		}
		palStart := 14 + int(headerSize)
		if palStart+count*4 > len(raw) {
			return nil, fmt.Errorf("调色板越界")
		}
		palette = make([]color.RGBA, count)
		for i := 0; i < count; i++ {
			p := palStart + i*4
			palette[i] = color.RGBA{R: raw[p+2], G: raw[p+1], B: raw[p], A: 0xff}
		}
	}

	// 每行按 4 字节对齐。
	rowSize := ((width*bitCount + 31) / 32) * 4
	need := int(dataOffset) + rowSize*height
	if need > len(raw) {
		return nil, fmt.Errorf("像素数据不完整：需要 %d 字节，实际 %d", need, len(raw))
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		// 自底向上时，文件里的第 0 行是图像的最后一行。
		srcRow := y
		if !topDown {
			srcRow = height - 1 - y
		}
		row := raw[int(dataOffset)+srcRow*rowSize:]

		for x := 0; x < width; x++ {
			var c color.RGBA
			switch bitCount {
			case 1:
				bit := (row[x/8] >> uint(7-x%8)) & 1
				c = paletteAt(palette, int(bit))
			case 4:
				b := row[x/2]
				idx := int(b >> 4)
				if x%2 == 1 {
					idx = int(b & 0x0f)
				}
				c = paletteAt(palette, idx)
			case 8:
				c = paletteAt(palette, int(row[x]))
			case 24:
				p := x * 3
				c = color.RGBA{R: row[p+2], G: row[p+1], B: row[p], A: 0xff}
			case 32:
				p := x * 4
				// 32 位 BMP 的第 4 字节多数是填充而非有效 alpha，一律当不透明处理，
				// 否则扫描件会整张变透明。
				c = color.RGBA{R: row[p+2], G: row[p+1], B: row[p], A: 0xff}
			default:
				return nil, fmt.Errorf("不支持的位深 %d", bitCount)
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img, nil
}

func paletteAt(palette []color.RGBA, idx int) color.RGBA {
	if idx >= 0 && idx < len(palette) {
		return palette[idx]
	}
	return color.RGBA{A: 0xff}
}

// encodeToBuffer 把图片编码进内存，仅供测试与调试使用。
func encodeToBuffer(img image.Image, ext string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if ext == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	}
	return buf.Bytes(), err
}
