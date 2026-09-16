package imgfmt

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
)

// ---- 造 BMP：模拟 FreeImage 存出来的那几种 ----

// buildBMP 拼一张未压缩 BMP。rows 是已经按行铺好的像素数据（不含 4 字节对齐的填充）。
func buildBMP(t *testing.T, width, height, bitCount int, palette []color.RGBA, rows [][]byte, dpi int) []byte {
	t.Helper()
	rowSize := ((width*bitCount + 31) / 32) * 4
	palBytes := len(palette) * 4
	dataOffset := 14 + 40 + palBytes

	buf := make([]byte, dataOffset+rowSize*height)
	copy(buf, "BM")
	binary.LittleEndian.PutUint32(buf[2:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[10:], uint32(dataOffset))
	binary.LittleEndian.PutUint32(buf[14:], 40)
	binary.LittleEndian.PutUint32(buf[18:], uint32(width))
	binary.LittleEndian.PutUint32(buf[22:], uint32(height)) // 正数 = 自底向上
	binary.LittleEndian.PutUint16(buf[26:], 1)
	binary.LittleEndian.PutUint16(buf[28:], uint16(bitCount))
	ppm := uint32(float64(dpi)/0.0254 + 0.5)
	binary.LittleEndian.PutUint32(buf[38:], ppm)
	binary.LittleEndian.PutUint32(buf[42:], ppm)
	binary.LittleEndian.PutUint32(buf[46:], uint32(len(palette)))

	for i, c := range palette {
		p := 14 + 40 + i*4
		buf[p], buf[p+1], buf[p+2] = c.B, c.G, c.R
	}
	for y := 0; y < height; y++ {
		// 自底向上：图像第 0 行写在文件最后一行
		copy(buf[dataOffset+(height-1-y)*rowSize:], rows[y])
	}
	return buf
}

func TestDecodeBMPBilevel(t *testing.T) {
	// 4x2 黑白：第 0 行 白黑白黑，第 1 行 黑白黑白
	pal := []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}}
	rows := [][]byte{{0xA0}, {0x50}} // 1010....  0101....
	im, err := DecodeBMP(buildBMP(t, 4, 2, 1, pal, rows, 300))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := im.Image.(*image.Paletted)
	if !ok {
		t.Fatalf("1 位图应当解成 Paletted，得到 %T", im.Image)
	}
	if im.DPIX != 300 || im.DPIY != 300 {
		t.Errorf("DPI 应当是 300，得到 %d/%d", im.DPIX, im.DPIY)
	}
	want := [][]uint8{{1, 0, 1, 0}, {0, 1, 0, 1}}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			if got := p.ColorIndexAt(x, y); got != want[y][x] {
				t.Errorf("(%d,%d) 得到 %d，期望 %d", x, y, got, want[y][x])
			}
		}
	}
}

func TestDecodeBMPGrayAndColor(t *testing.T) {
	// 8 位灰度（灰阶调色板）
	pal := make([]color.RGBA, 256)
	for i := range pal {
		pal[i] = color.RGBA{uint8(i), uint8(i), uint8(i), 255}
	}
	rows := [][]byte{{0, 64, 128, 255}, {255, 128, 64, 0}}
	im, err := DecodeBMP(buildBMP(t, 4, 2, 8, pal, rows, 200))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := im.Image.(*image.Gray)
	if !ok {
		t.Fatalf("灰阶调色板应当解成 Gray，得到 %T", im.Image)
	}
	if g.GrayAt(0, 0).Y != 0 || g.GrayAt(3, 0).Y != 255 || g.GrayAt(0, 1).Y != 255 {
		t.Errorf("灰度值不对: %v %v %v", g.GrayAt(0, 0), g.GrayAt(3, 0), g.GrayAt(0, 1))
	}
	if im.DPIX != 200 {
		t.Errorf("DPI 应当是 200，得到 %d", im.DPIX)
	}

	// 24 位真彩：BMP 里是 BGR
	rows24 := [][]byte{{0, 0, 255, 0, 255, 0}, {255, 0, 0, 255, 255, 255}}
	im2, err := DecodeBMP(buildBMP(t, 2, 2, 24, nil, rows24, 600))
	if err != nil {
		t.Fatal(err)
	}
	rgba, ok := im2.Image.(*image.RGBA)
	if !ok {
		t.Fatalf("24 位应当解成 RGBA，得到 %T", im2.Image)
	}
	if rgba.RGBAAt(0, 0) != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("(0,0) 应当是红色，得到 %v", rgba.RGBAAt(0, 0))
	}
	if rgba.RGBAAt(1, 1) != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("(1,1) 应当是白色，得到 %v", rgba.RGBAAt(1, 1))
	}
}

// ---- TIFF ----

// tiffTags 把编出来的 TIFF 的标签解出来，用于检查结构。只认小端。
func tiffTags(t *testing.T, b []byte) map[uint16][]uint32 {
	t.Helper()
	if len(b) < 8 || string(b[:2]) != "II" || binary.LittleEndian.Uint16(b[2:4]) != 42 {
		t.Fatalf("TIFF 文件头不对: % x", b[:min(8, len(b))])
	}
	off := binary.LittleEndian.Uint32(b[4:8])
	if int(off)+2 > len(b) {
		t.Fatalf("IFD 偏移越界: %d", off)
	}
	n := int(binary.LittleEndian.Uint16(b[off : off+2]))
	out := map[uint16][]uint32{}
	prev := uint16(0)
	for i := 0; i < n; i++ {
		p := int(off) + 2 + i*12
		tag := binary.LittleEndian.Uint16(b[p:])
		if tag < prev {
			t.Errorf("IFD 条目没有按标签号升序: %d 出现在 %d 之后", tag, prev)
		}
		prev = tag
		typ := binary.LittleEndian.Uint16(b[p+2:])
		cnt := binary.LittleEndian.Uint32(b[p+4:])
		size := map[uint16]int{2: 1, 3: 2, 4: 4, 5: 8}[typ]
		vals := make([]uint32, 0, cnt)
		read := func(q int) uint32 {
			switch typ {
			case 3:
				return uint32(binary.LittleEndian.Uint16(b[q:]))
			case 2:
				return uint32(b[q])
			default:
				return binary.LittleEndian.Uint32(b[q:])
			}
		}
		if int(cnt)*size <= 4 {
			for j := 0; j < int(cnt); j++ {
				vals = append(vals, read(p+8+j*size))
			}
		} else {
			d := int(binary.LittleEndian.Uint32(b[p+8:]))
			if typ == 5 { // RATIONAL：分子分母各算一个
				for j := 0; j < int(cnt); j++ {
					vals = append(vals, binary.LittleEndian.Uint32(b[d+j*8:]), binary.LittleEndian.Uint32(b[d+j*8+4:]))
				}
			} else {
				for j := 0; j < int(cnt); j++ {
					vals = append(vals, read(d+j*size))
				}
			}
		}
		out[tag] = vals
	}
	return out
}

func TestEncodeTIFFStructure(t *testing.T) {
	cases := []struct {
		name        string
		img         image.Image
		photometric uint32
		bits        []uint32
		samples     uint32
	}{
		{"黑白", image.NewPaletted(image.Rect(0, 0, 40, 30), color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}), 1, []uint32{1}, 1},
		{"灰度", image.NewGray(image.Rect(0, 0, 40, 30)), 1, []uint32{8}, 1},
		{"彩色", image.NewRGBA(image.Rect(0, 0, 40, 30)), 2, []uint32{8, 8, 8}, 3},
	}
	for _, c := range cases {
		b, err := EncodeTIFFBytes(c.img, 300, 400)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		tags := tiffTags(t, b)

		check := func(tag uint16, want []uint32, what string) {
			got := tags[tag]
			if len(got) != len(want) {
				t.Errorf("%s: %s 得到 %v，期望 %v", c.name, what, got, want)
				return
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s: %s 得到 %v，期望 %v", c.name, what, got, want)
					return
				}
			}
		}
		check(256, []uint32{40}, "宽")
		check(257, []uint32{30}, "高")
		check(258, c.bits, "位深")
		check(259, []uint32{5}, "压缩方式（5 = LZW）")
		check(262, []uint32{c.photometric}, "光度解释")
		check(277, []uint32{c.samples}, "每像素采样数")
		check(284, []uint32{1}, "平面配置")
		check(296, []uint32{2}, "分辨率单位（2 = 英寸）")
		check(282, []uint32{300, 1}, "X 分辨率")
		check(283, []uint32{400, 1}, "Y 分辨率")

		if len(tags[273]) == 0 || len(tags[273]) != len(tags[279]) {
			t.Errorf("%s: 条带偏移和条带长度对不上: %v / %v", c.name, tags[273], tags[279])
		}
		// 条带数据不能越界
		for i := range tags[273] {
			if int(tags[273][i]+tags[279][i]) > len(b) {
				t.Errorf("%s: 第 %d 个条带越界", c.name, i)
			}
		}
	}
}

// 没给 DPI 时按 300 写，别写成 1——写 1 会让看图软件以为是 1dpi 的巨幅图。
func TestEncodeTIFFDefaultDPI(t *testing.T) {
	b, err := EncodeTIFFBytes(image.NewGray(image.Rect(0, 0, 8, 8)), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tags := tiffTags(t, b)
	if len(tags[282]) != 2 || tags[282][0] != 300 {
		t.Errorf("默认 X 分辨率应当是 300，得到 %v", tags[282])
	}
}

// LZW 得真的压缩：大片同色的图压完要比原始像素小得多。
func TestTIFFCompresses(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 600, 600)) // 全黑，最容易压
	b, err := EncodeTIFFBytes(img, 300, 300)
	if err != nil {
		t.Fatal(err)
	}
	raw := 600 * 600
	if len(b) > raw/20 {
		t.Errorf("LZW 压缩后 %d 字节，原始像素 %d 字节，压得太少，可能没真压", len(b), raw)
	}
}

// ---- JPEG ----

func TestEncodeJPEGWritesDPI(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	b, err := Encode(Image{Image: img, DPIX: 300, DPIY: 200}, "jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if b[13] != 1 {
		t.Errorf("JFIF 单位应当是 1（每英寸），得到 %d", b[13])
	}
	if x := int(b[14])<<8 | int(b[15]); x != 300 {
		t.Errorf("X 密度应当是 300，得到 %d", x)
	}
	if y := int(b[16])<<8 | int(b[17]); y != 200 {
		t.Errorf("Y 密度应当是 200，得到 %d", y)
	}
	// 改完还得是一张能解的 JPEG
	if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("改 DPI 之后 JPEG 解不开了: %v", err)
	}
}

func TestNormalizeExt(t *testing.T) {
	cases := map[string]string{
		"jpg": "jpg", "JPEG": "jpg", ".jpeg": "jpg", " tif ": "tiff", "TIFF": "tiff",
		"png": "png", "bmp": "", "": "", "pdf": "",
	}
	for in, want := range cases {
		if got := NormalizeExt(in); got != want {
			t.Errorf("NormalizeExt(%q) = %q，期望 %q", in, got, want)
		}
	}
	if Supported("bmp") || !Supported("tif") {
		t.Error("Supported 判断不对")
	}
}

func TestEncodeUnknownFormat(t *testing.T) {
	if _, err := Encode(Image{Image: image.NewGray(image.Rect(0, 0, 4, 4))}, "pdf"); err == nil {
		t.Error("不认识的格式应当报错")
	}
}

// ---- 按内容识别源格式 ----

func TestSniff(t *testing.T) {
	pal := []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}}
	bmp := buildBMP(t, 4, 2, 1, pal, [][]byte{{0xA0}, {0x50}}, 300)

	jpg, err := Encode(Image{Image: image.NewGray(image.Rect(0, 0, 8, 8)), DPIX: 300, DPIY: 300}, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	pngBytes, err := Encode(Image{Image: image.NewGray(image.Rect(0, 0, 8, 8))}, "png")
	if err != nil {
		t.Fatal(err)
	}
	tif, err := EncodeTIFFBytes(image.NewGray(image.Rect(0, 0, 8, 8)), 300, 300)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"BMP", bmp, "bmp"},
		{"JPEG", jpg, "jpg"},
		{"PNG", pngBytes, "png"},
		{"TIFF", tif, "tiff"},
		{"空", nil, ""},
		{"乱码", []byte("not an image at all"), ""},
	}
	for _, c := range cases {
		if got := Sniff(c.data); got != c.want {
			t.Errorf("%s: Sniff = %q，期望 %q", c.name, got, c.want)
		}
	}
}

// 解码只看内容，不看后缀：故意把 BMP 存成 .jpg 也要能解出来。
func TestDecodeFileIgnoresExtension(t *testing.T) {
	pal := []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}}
	bmp := buildBMP(t, 4, 2, 1, pal, [][]byte{{0xA0}, {0x50}}, 300)

	dir := t.TempDir()
	path := dir + "/misnamed.jpg" // 后缀是假的
	if err := os.WriteFile(path, bmp, 0o644); err != nil {
		t.Fatal(err)
	}

	im, format, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("按内容应当能解出 BMP: %v", err)
	}
	if format != "bmp" {
		t.Errorf("识别成 %q，期望 bmp", format)
	}
	if im.Image.Bounds().Dx() != 4 || im.DPIX != 300 {
		t.Errorf("解出来的图不对: %v dpi=%d", im.Image.Bounds(), im.DPIX)
	}
}

func TestDecodeFileJPEGAndPNG(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ ext, want string }{{"jpg", "jpg"}, {"png", "png"}} {
		body, err := Encode(Image{Image: image.NewGray(image.Rect(0, 0, 8, 8)), DPIX: 300, DPIY: 300}, c.ext)
		if err != nil {
			t.Fatal(err)
		}
		path := dir + "/x." + c.ext
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		_, format, err := DecodeFile(path)
		if err != nil || format != c.want {
			t.Errorf("%s: 得到 format=%q err=%v", c.ext, format, err)
		}
	}
}

// TIFF 能认出来但本包不解码——调用方据此判断"已经是目标格式"，不会误当成坏文件。
func TestDecodeFileTIFFRecognizedNotDecoded(t *testing.T) {
	tif, err := EncodeTIFFBytes(image.NewGray(image.Rect(0, 0, 8, 8)), 300, 300)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/x.tiff"
	if err := os.WriteFile(path, tif, 0o644); err != nil {
		t.Fatal(err)
	}
	_, format, err := DecodeFile(path)
	if format != "tiff" {
		t.Errorf("应当识别为 tiff，得到 %q", format)
	}
	if err == nil {
		t.Error("本包不解码 TIFF，应当返回错误")
	}
}
