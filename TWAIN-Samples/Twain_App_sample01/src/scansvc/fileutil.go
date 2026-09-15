package main

// 文件与目录操作工具层。
//
// 照搬自既有的 Go 转发服务（filemanager 仓库 "扫描工具" 分支的 utils/file.go、
// utils/Browser.go），行为刻意保持一致：
//   - 相对路径用 os.PathSeparator 拼接（Windows 下回给前端的是 `子目录\文件名`）；
//   - copyPath 既能复制单文件也能递归复制整个目录；
//   - 列目录支持"忽略"和"只取"两种正则。
//
// 和原版的差异只有两处，都是原版的坑，在各自函数上单独注明。

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// pthSep 对应原版的 utils.PthSep。
var pthSep = string(os.PathSeparator)

func fileExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	s, err := os.Stat(path)
	if err != nil {
		return false
	}
	return s.IsDir()
}

func mkDir(path string) error {
	if fileExist(path) {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

// joinPath 保持原版语义：任一侧为空时直接返回另一侧，不会多出一个分隔符。
func joinPath(p1, p2 string) string {
	if p1 == "" {
		return p2
	}
	if p2 == "" {
		return p1
	}
	return p1 + pthSep + p2
}

// trimTrailingSep 去掉目录路径末尾的分隔符。
// 原版是 path.Dir(dir) 干这件事的——只有在末尾确实带分隔符时才成立，
// 这里写成直接裁剪，语义一样但不会在别处误伤。
func trimTrailingSep(dir string) string {
	for len(dir) > 1 && (strings.HasSuffix(dir, "/") || strings.HasSuffix(dir, "\\")) {
		dir = dir[:len(dir)-1]
	}
	return dir
}

// getAllFiles 递归列出 root/dirPath 下的所有文件，返回相对 root 的路径。
//
// filenameIgnorePattern 命中则跳过；它为空时才看 filenameOnlyPattern，
// 后者是反过来的——没命中的才跳过。两个都为空就是全要。
func getAllFiles(root, dirPath, filenameIgnorePattern, filenameOnlyPattern string) ([]string, error) {
	files := []string{}
	entries, err := os.ReadDir(joinPath(root, dirPath))
	if err != nil {
		return files, err
	}

	for _, fi := range entries {
		if fi.IsDir() {
			sub, _ := getAllFiles(root, joinPath(dirPath, fi.Name()), filenameIgnorePattern, filenameOnlyPattern)
			files = append(files, sub...)
			continue
		}

		var ignore bool
		if filenameIgnorePattern != "" {
			ignore, _ = regexp.MatchString(filenameIgnorePattern, fi.Name())
		} else if filenameOnlyPattern != "" {
			ignore, _ = regexp.MatchString(filenameOnlyPattern, fi.Name())
			ignore = !ignore
		}

		if !ignore {
			files = append(files, joinPath(dirPath, fi.Name()))
		}
	}
	return files, nil
}

// getChildFolder 列出 root/dirPath 下的子目录和文件（只看一层，不递归）。
//
// 与原版的差异：原版对空的 filenameIgnorePattern 没做判空，而空正则匹配任何
// 字符串，结果是"不传忽略规则 → 目录和文件全被过滤掉、返回两个空数组"。
// 这里补上判空，不传规则时全要。
func getChildFolder(root, dirPath, filenameIgnorePattern string) (folders, files []string, err error) {
	folders = []string{}
	files = []string{}
	entries, err := os.ReadDir(joinPath(root, dirPath))
	if err != nil {
		return folders, files, err
	}

	for _, fi := range entries {
		var ignore bool
		if filenameIgnorePattern != "" {
			ignore, _ = regexp.MatchString(filenameIgnorePattern, fi.Name())
		}
		if ignore {
			continue
		}
		if fi.IsDir() {
			folders = append(folders, fi.Name())
		} else {
			files = append(files, fi.Name())
		}
	}
	return folders, files, nil
}

// copyPath 复制单个文件或整个目录（递归）。目标端的父目录会自动建出来。
func copyPath(from, to string) error {
	st, err := os.Stat(from)
	if err != nil {
		return err
	}

	if st.IsDir() {
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		if err := mkDir(to); err != nil {
			return err
		}
		for _, item := range entries {
			if err := copyPath(filepath.Join(from, item.Name()), filepath.Join(to, item.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	if err := mkDir(filepath.Dir(to)); err != nil {
		return err
	}

	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// openWithSystem 用系统默认程序打开路径（对应原版 utils.Open）。
//
// 与原版的差异：原版是 exec.Command("cmd /c start", uri)，把 "cmd /c start"
// 整个当成可执行文件名，在 Windows 上必然找不到。这里拆成正确的参数形式，
// start 后面那个空串是窗口标题占位——不给的话目录名带空格时会被当成标题。
func openWithSystem(uri string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", uri).Start()
	case "darwin":
		return exec.Command("open", uri).Start()
	case "linux":
		return exec.Command("xdg-open", uri).Start()
	default:
		return fmt.Errorf("不支持在 %s 上打开路径", runtime.GOOS)
	}
}

// safeJoin 把相对路径接到 root 下，并确认结果没有跑出 root。
// 原版对 name / filepath 这类前端传来的相对路径完全不做校验，一个 ../..
// 就能读写到目录外面去，这里统一挡掉。
func safeJoin(root, rel string) (string, bool) {
	if root == "" {
		return "", false
	}
	// 统一成 / 再 Clean：前端在 Windows 上拿到的相对路径是带反斜杠的，
	// 拼进 URL 或表单后原样送回来，两种分隔符都得认。
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = filepath.ToSlash(filepath.Clean("/" + rel))
	full := filepath.Join(root, filepath.FromSlash(rel))

	r, err := filepath.Rel(root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+pthSep) {
		return "", false
	}
	return full, true
}
