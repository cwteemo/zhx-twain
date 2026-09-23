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
//
// 转成什么**只看 ext**（前端请求里的 extension），源文件是什么**只看内容**，不看它的后缀——
// 后缀是谁都能改的，内容才是事实。DLL 现在固定吐 BMP（`zhx_Scan` 是用 *.bmp 找新文件的），
// 但驱动设置一变就可能直接出 JPEG，那时候还按 BMP 解就会失败。
//
// ext 不认识（空、bmp 之类）时原样返回源文件：扩展名必须和实际内容一致，
// 前端是从 URL 结尾取扩展名再报给后端的，糊弄不得。
func convertScan(srcPath, ext string) (string, error) {
	if !imgfmt.Supported(ext) {
		return srcPath, nil
	}
	ext = imgfmt.NormalizeExt(ext)

	im, srcFormat, err := imgfmt.DecodeFile(srcPath)
	if err != nil {
		if srcFormat == ext {
			// 源文件已经是要的格式了（比如驱动直接出了 JPEG），解不解得开都无所谓，
			// 只要后缀对上就行。
			return renameToExt(srcPath, ext)
		}
		// 解不动又不是目标格式：退回原图，别让格式转换挡住扫描本身。
		return srcPath, fmt.Errorf("解码扫描产物失败（识别为 %s），保留原文件: %w", orUnknown(srcFormat), err)
	}
	if srcFormat == ext {
		// 已经是目标格式，重新编码只会掉画质（JPEG 尤其），改个后缀就够。
		return renameToExt(srcPath, ext)
	}

	// 源图 DPI 缺失或异常时 imgfmt.Encode 会兜底（见 imgfmt.ResolveDPI），这里只负责留痕：
	// 兜底值不一定是真实扫描分辨率，档案验收对不上时要能从日志查到是哪一页。
	// DLL 已经用 DAT_IMAGEINFO 的分辨率回填 BMP 头，走到这里说明驱动连 IMAGEINFO 都没给对。
	if dx, dy, fallback := imgfmt.ResolveDPI(im.DPIX, im.DPIY); fallback {
		log.Printf("警告: %s 的 DPI 缺失或异常（%d x %d），转 %s 时按 %d x %d 写，详见 twain.log 里这一页的 IMAGEINFO",
			filepath.Base(srcPath), im.DPIX, im.DPIY, ext, dx, dy)
	}

	body, err := imgfmt.Encode(im, ext)
	if err != nil {
		return srcPath, fmt.Errorf("编码 %s 失败，保留原文件（%s）: %w", ext, orUnknown(srcFormat), err)
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

	// 原图留着没意义，转换成功就删掉——一张十几 MB，连续扫几百页很快就把盘吃满。
	os.Remove(srcPath)
	return dstPath, nil
}

// renameToExt 只改后缀不动内容，用于"源文件已经是目标格式"的情况。
// 目标已存在（同名不同后缀的冲突）时保留源文件，交给上层的去重逻辑处理。
func renameToExt(srcPath, ext string) (string, error) {
	dstPath := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + "." + ext
	if dstPath == srcPath {
		return srcPath, nil
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		return srcPath, fmt.Errorf("改名为 %s 失败: %w", dstPath, err)
	}
	return dstPath, nil
}

func orUnknown(format string) string {
	if format == "" {
		return "未知格式"
	}
	return format
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
