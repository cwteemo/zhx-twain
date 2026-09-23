// Package imgfmt 负责扫描产物的格式转换：把 DLL 吐出来的 BMP 转成前端要的格式。
//
// 目前支持 **jpg / tiff（LZW 压缩）/ png** 三种。tiff 和 jpg 是档案数字化实际在用的两种，
// png 是本服务早期的默认值，留着兼容。
//
// 分成独立的包有个实际好处：它是纯 Go 的，不碰 cgo，所以在 Linux 上也能
// `go test ./imgfmt` 跑全套——TIFF 的字节布局和 LZW 码流这种东西，光靠肉眼看代码是看不出错的。
package imgfmt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
)

// JPEGQuality 是转 JPEG 时的质量。85 是肉眼几乎无损、体积又降得下来的常用档位。
const JPEGQuality = 85

// NormalizeExt 把用户给的扩展名规整一下：去掉点、转小写、tif 统一成 tiff、jpeg 统一成 jpg。
// 返回空串表示不认识这个格式。
func NormalizeExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), ".")) {
	case "jpg", "jpeg":
		return "jpg"
	case "tif", "tiff":
		return "tiff"
	case "png":
		return "png"
	}
	return ""
}

// Supported 判断这个格式认不认识。
func Supported(ext string) bool { return NormalizeExt(ext) != "" }

// Sniff 按**文件内容**判断是什么格式，返回 NormalizeExt 那套名字（jpg / png / tiff / bmp）。
// 认不出来返回空串。
//
// 为什么不看扩展名：扩展名是调用方给的，内容才是事实。扫描仪驱动换个设置就可能
// 直接吐 JPEG，这时候还当成 BMP 去解就会失败。
func Sniff(raw []byte) string {
	switch {
	case len(raw) >= 2 && raw[0] == 'B' && raw[1] == 'M':
		return "bmp"
	case len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return "jpg"
	case len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(raw) >= 4 && (string(raw[:4]) == "II*\x00" || string(raw[:4]) == "MM\x00*"):
		return "tiff"
	}
	return ""
}

// DecodeFile 读一张图，按内容挑解码器。
// 返回的第二个值是识别出来的源格式（见 Sniff）。
//
// TIFF 只认得出来、解不了——本包只写 TIFF 不读 TIFF（读 TIFF 要处理一堆压缩方式，
// 而扫描流程里不需要）。真遇到 TIFF 源文件，调用方按"已经是目标格式"处理即可。
func DecodeFile(path string) (Image, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Image{}, "", err
	}
	format := Sniff(raw)
	switch format {
	case "bmp":
		im, err := DecodeBMP(raw)
		return im, format, err
	case "jpg":
		img, err := jpeg.Decode(bytes.NewReader(raw))
		return Image{Image: img}, format, err
	case "png":
		img, err := png.Decode(bytes.NewReader(raw))
		return Image{Image: img}, format, err
	case "tiff":
		return Image{}, format, fmt.Errorf("本服务不解码 TIFF")
	}
	return Image{}, "", fmt.Errorf("认不出这是什么图片格式（前 4 字节 % x）", raw[:min(4, len(raw))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FallbackDPI 是 DPI 缺失或异常时的兜底值。档案数字化最常用的就是 300。
const FallbackDPI = 300

// 合理 DPI 的范围。扫描仪最低档一般 75、最高到 1200~4800；超出这个范围的
// 多半是驱动没填（0）、填了垃圾值，或者单位换算错了，一律当异常。
const (
	MinValidDPI = 50
	MaxValidDPI = 9600
)

// ValidDPI 判断一个 DPI 是否可信。
func ValidDPI(d int) bool { return d >= MinValidDPI && d <= MaxValidDPI }

// ResolveDPI 决定真正写进文件的 DPI，fallback 表示用了兜底（调用方据此记日志）。
//   - 两个都可信：原样用
//   - 只有一个可信：另一个跟它一样（扫描几乎都是 X/Y 同分辨率）
//   - 都不可信：都用 FallbackDPI
func ResolveDPI(x, y int) (dx, dy int, fallback bool) {
	vx, vy := ValidDPI(x), ValidDPI(y)
	switch {
	case vx && vy:
		return x, y, false
	case vx:
		return x, x, true
	case vy:
		return y, y, true
	}
	return FallbackDPI, FallbackDPI, true
}

// Encode 按 ext 指定的格式编码。ext 先过 NormalizeExt。
// dpi 会写进文件：TIFF 写 XResolution / YResolution，JPEG 写 JFIF 里的密度，PNG 写 pHYs。
// 档案验收要查图片里记录的 DPI，丢了等于白扫，所以缺失 / 异常时按 ResolveDPI 兜底。
func Encode(im Image, ext string) ([]byte, error) {
	im.DPIX, im.DPIY, _ = ResolveDPI(im.DPIX, im.DPIY)
	switch NormalizeExt(ext) {
	case "tiff":
		return EncodeTIFFBytes(im.Image, im.DPIX, im.DPIY)

	case "jpg":
		buf := &byteWriter{}
		if err := jpeg.Encode(buf, im.Image, &jpeg.Options{Quality: JPEGQuality}); err != nil {
			return nil, err
		}
		return withJPEGDensity(buf.b, im.DPIX, im.DPIY), nil

	case "png":
		buf := &byteWriter{}
		if err := png.Encode(buf, im.Image); err != nil {
			return nil, err
		}
		return withPNGDensity(buf.b, im.DPIX, im.DPIY), nil
	}
	return nil, fmt.Errorf("不支持的格式 %q（支持 jpg / tiff / png）", ext)
}

// withJPEGDensity 把 DPI 写进 JPEG。
//
// Go 的 jpeg 编码器**根本不写 JFIF 段**（SOI 之后直接是量化表），看图软件只能按默认值猜，
// 多半显示成 96dpi。档案验收要查图片里记录的 DPI，所以这里补一段标准的 APP0 JFIF：
//
//	FF E0 | 长度(2) | "JFIF\0" | 版本(2) | 单位(1) | X 密度(2) | Y 密度(2) | 缩略图宽高(2)
//
// 已经有 APP0 的（换个 Go 版本可能就有了）就地改那几个字节，不重复插。
func withJPEGDensity(b []byte, dpiX, dpiY int) []byte {
	if dpiX <= 0 || dpiY <= 0 || dpiX > 0xffff || dpiY > 0xffff {
		return b
	}
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return b // 不是 JPEG，不碰
	}

	// 已有 JFIF：改单位和密度
	if len(b) >= 18 && b[2] == 0xFF && b[3] == 0xE0 && string(b[6:11]) == "JFIF\x00" {
		b[13] = 1
		b[14], b[15] = byte(dpiX>>8), byte(dpiX)
		b[16], b[17] = byte(dpiY>>8), byte(dpiY)
		return b
	}

	seg := []byte{
		0xFF, 0xE0, 0x00, 0x10, // APP0，段长 16（不含 FF E0）
		'J', 'F', 'I', 'F', 0x00,
		0x01, 0x02, // JFIF 1.02
		0x01, // 单位：每英寸点数
		byte(dpiX >> 8), byte(dpiX),
		byte(dpiY >> 8), byte(dpiY),
		0x00, 0x00, // 不带缩略图
	}
	out := make([]byte, 0, len(b)+len(seg))
	out = append(out, b[0], b[1]) // SOI
	out = append(out, seg...)
	return append(out, b[2:]...)
}

// withPNGDensity 把 DPI 写进 PNG 的 pHYs 块。
//
// 和 JPEG 一样，Go 的 png 编码器不写 pHYs，看图软件只能当 96dpi / 72dpi 显示，
// 而 png 恰好是服务不传 extension 时的默认格式。pHYs 只认"每米像素数"，
// 必须出现在 IDAT 之前，这里紧跟在 IHDR 后面插一块：
//
//	长度(4)=9 | "pHYs" | X 每米像素(4) | Y 每米像素(4) | 单位(1)=1 米 | CRC(4)
//
// 已经有 pHYs 的（换个 Go 版本可能就有了）不重复插。
func withPNGDensity(b []byte, dpiX, dpiY int) []byte {
	if dpiX <= 0 || dpiY <= 0 {
		return b
	}
	// 8 字节签名 + IHDR 块（4 长度 + 4 类型 + 13 数据 + 4 CRC）
	const ihdrEnd = 8 + 4 + 4 + 13 + 4
	if len(b) < ihdrEnd || string(b[1:4]) != "PNG" || string(b[12:16]) != "IHDR" {
		return b // 不是 Go 写出来的那种标准 PNG，不碰
	}
	// 按块走到 IDAT 为止找 pHYs；不能在字节里直接搜，像素数据里碰巧有这 4 个字节就误判了
	for off := 8; off+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[off : off+4]))
		typ := string(b[off+4 : off+8])
		if typ == "pHYs" {
			return b
		}
		if typ == "IDAT" || n < 0 || off+12+n > len(b) {
			break
		}
		off += 12 + n
	}

	ppm := func(dpi int) uint32 { return uint32(float64(dpi)/0.0254 + 0.5) }
	chunk := make([]byte, 4+4+9+4)
	binary.BigEndian.PutUint32(chunk[0:4], 9)
	copy(chunk[4:8], "pHYs")
	binary.BigEndian.PutUint32(chunk[8:12], ppm(dpiX))
	binary.BigEndian.PutUint32(chunk[12:16], ppm(dpiY))
	chunk[16] = 1 // 单位：米
	binary.BigEndian.PutUint32(chunk[17:21], crc32.ChecksumIEEE(chunk[4:17]))

	out := make([]byte, 0, len(b)+len(chunk))
	out = append(out, b[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, b[ihdrEnd:]...)
}
