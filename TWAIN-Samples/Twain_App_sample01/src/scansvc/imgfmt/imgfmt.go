// Package imgfmt 负责扫描产物的格式转换：把 DLL 吐出来的 BMP 转成前端要的格式。
//
// 目前支持 **jpg / tiff（LZW 压缩）/ png** 三种。tiff 和 jpg 是档案数字化实际在用的两种，
// png 是本服务早期的默认值，留着兼容。
//
// 分成独立的包有个实际好处：它是纯 Go 的，不碰 cgo，所以在 Linux 上也能
// `go test ./imgfmt` 跑全套——TIFF 的字节布局和 LZW 码流这种东西，光靠肉眼看代码是看不出错的。
package imgfmt

import (
	"fmt"
	"image/jpeg"
	"image/png"
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

// Encode 按 ext 指定的格式编码。ext 先过 NormalizeExt。
// dpi 会写进文件：TIFF 写 XResolution / YResolution，JPEG 写 JFIF 里的密度。
// 档案验收要查图片里记录的 DPI，丢了等于白扫。
func Encode(im Image, ext string) ([]byte, error) {
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
		return buf.b, nil
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
