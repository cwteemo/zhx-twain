package main

// 图片格式转换。
//
// DLL 只会吐 BMP——zhx_Scan 里靠 "*.bmp" 这个通配符比对扫描前后的目录来认产物，
// 换格式就认不出来了。而前端要的是 jpg / tiff（档案数字化在用的两种），
// 单张 A4 300dpi 彩色 BMP 十几 MB，原样回传也不合适。所以在 Go 这边转。
//
// 解码、编码、TIFF 的 LZW 压缩都在 imgfmt 包里（纯 Go，可以单测）。这里只管文件进出。

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"scansvc/imgfmt"
)

// convertScan 把扫描产物转成 ext 指定的格式，返回新文件路径。
// ext 为空、为 bmp、或不认识的格式时原样返回源文件——扩展名和实际内容必须一致，
// 前端是从 URL 结尾取扩展名再报给后端的，糊弄不得。
func convertScan(srcPath, ext string) (string, error) {
	if !imgfmt.Supported(ext) {
		return srcPath, nil
	}
	ext = imgfmt.NormalizeExt(ext)

	im, err := imgfmt.DecodeBMPFile(srcPath)
	if err != nil {
		// 解不动就退回原图，别让格式转换挡住扫描本身。
		return srcPath, fmt.Errorf("解码 BMP 失败，回退为原始 BMP: %w", err)
	}

	body, err := imgfmt.Encode(im, ext)
	if err != nil {
		return srcPath, fmt.Errorf("编码 %s 失败，回退为原始 BMP: %w", ext, err)
	}

	dstPath := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + "." + ext
	if err := os.WriteFile(dstPath, body, 0o644); err != nil {
		return srcPath, fmt.Errorf("写入 %s 失败，回退为原始 BMP: %w", dstPath, err)
	}

	// TIFF 浏览器显示不了，顺手存一张同名 JPEG 当预览：/file/<名>?thumbnail=1 就是
	// 优先取同名 .jpeg（见 files.go）。前端要预览加这个参数，要原图就用原地址。
	if ext == "tiff" {
		writeTIFFPreview(im, dstPath)
	}

	// 原 BMP 留着没意义，转换成功就删掉——一张十几 MB，连续扫几百页很快就把盘吃满。
	os.Remove(srcPath)
	return dstPath, nil
}

// writeTIFFPreview 给一张 TIFF 存同名的 JPEG 预览。失败只记日志：
// 预览没了不影响归档，扫描更不该因此中断。
func writeTIFFPreview(im imgfmt.Image, tiffPath string) {
	body, err := imgfmt.Encode(im, "jpg")
	if err != nil {
		log.Printf("生成 %s 的 JPEG 预览失败: %v", filepath.Base(tiffPath), err)
		return
	}
	preview := strings.TrimSuffix(tiffPath, filepath.Ext(tiffPath)) + ".jpeg"
	if err := os.WriteFile(preview, body, 0o644); err != nil {
		log.Printf("写入 JPEG 预览 %s 失败: %v", preview, err)
	}
}
