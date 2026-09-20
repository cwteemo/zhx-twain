// mkicon 把一个 .ico 打包成 Go 能直接用的 Windows 资源文件（.syso）。
//
// 用途：让 scansvc.exe 在资源管理器里有图标，托盘也能拿到它
// （tray_windows.go 里是 ExtractIconW(exe)，取的就是 exe 自己的图标）。
//
// 为什么自己写：Go 本身不会把图标打进 exe，通常的做法是用 github.com/akavel/rsrc 生成
// .syso。但那是个额外的命令行工具，现场机器多半没网、也不该为了换个图标去装东西。
// 这个工具就在仓库里，几十行，`go run` 一下就出结果。
//
// 用法（改了 favicon.ico 之后跑一次，把产物提交进去）：
//
//	cd TWAIN-Samples\Twain_App_sample01\src\scansvc
//	go run ./tools/mkicon favicon.ico rsrc_windows_amd64.syso
//	go run ./tools/mkicon favicon.ico rsrc_windows_386.syso
//
// 文件名必须是 *_windows_amd64.syso / *_windows_386.syso：Go 只在编对应平台时
// 才带上它，别的平台（比如在 Linux 上 go vet）会自动忽略，不会报错。
// 目标架构也是从这个文件名认出来的——COFF 的 machine 字段和重定位类型跟架构相关，
// 拿 amd64 的 .syso 去链 32 位 exe，链接器会直接报"machine type conflict"。
//
// 图标里的每一张图原样搬进资源，不做缩放——Windows 自己会挑合适的尺寸。
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Windows 资源类型编号，见 winuser.h。
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// coffArch 是一个目标架构在 COFF 里的两处差异。
type coffArch struct {
	name    string
	machine uint16 // IMAGE_FILE_MACHINE_*
	// 相对映像基址的 32 位地址重定位，资源表要用的就是它。
	// amd64 是 IMAGE_REL_AMD64_ADDR32NB，i386 是 IMAGE_REL_I386_DIR32NB，编号不同。
	relocDir32NB uint16
}

var (
	archAMD64 = coffArch{name: "amd64", machine: 0x8664, relocDir32NB: 0x0003}
	arch386   = coffArch{name: "386", machine: 0x014c, relocDir32NB: 0x0007}
)

// archFromSysoName 按 .syso 的文件名认出目标架构。
// Go 就是按这个后缀决定哪个平台带上这个文件的，架构信息本来就在文件名里，
// 再加一个命令行参数只会多一个能填错的地方。
func archFromSysoName(path string) (coffArch, error) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "_amd64"):
		return archAMD64, nil
	case strings.Contains(base, "_386"):
		return arch386, nil
	default:
		return coffArch{}, fmt.Errorf("从文件名认不出架构: %s（应形如 rsrc_windows_amd64.syso 或 rsrc_windows_386.syso）", base)
	}
}

// icoEntry 是 .ico 文件里的一条目录项（ICONDIRENTRY）。
type icoEntry struct {
	Width, Height, Colors, Reserved byte
	Planes, BitCount                uint16
	BytesInRes, Offset              uint32
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "用法: go run ./tools/mkicon <输入.ico> <输出.syso>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "失败:", err)
		os.Exit(1)
	}
}

func run(icoPath, sysoPath string) error {
	raw, err := os.ReadFile(icoPath)
	if err != nil {
		return err
	}
	entries, images, err := parseICO(raw)
	if err != nil {
		return err
	}

	target, err := archFromSysoName(sysoPath)
	if err != nil {
		return err
	}

	rsrc := buildResourceSection(entries, images)
	obj := buildCOFF(rsrc, target)

	if err := os.WriteFile(sysoPath, obj, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s -> %s（%d 张图，%d 字节）\n", icoPath, sysoPath, len(entries), len(obj))
	return nil
}

// parseICO 读出 .ico 里的目录项和每张图的原始字节。
func parseICO(raw []byte) ([]icoEntry, [][]byte, error) {
	if len(raw) < 6 {
		return nil, nil, fmt.Errorf("文件太小，不是 .ico")
	}
	if binary.LittleEndian.Uint16(raw[0:2]) != 0 || binary.LittleEndian.Uint16(raw[2:4]) != 1 {
		return nil, nil, fmt.Errorf("不是 .ico（文件头不对）")
	}
	count := int(binary.LittleEndian.Uint16(raw[4:6]))
	if count == 0 {
		return nil, nil, fmt.Errorf("图标里一张图都没有")
	}

	entries := make([]icoEntry, count)
	images := make([][]byte, count)
	for i := 0; i < count; i++ {
		p := 6 + i*16
		if p+16 > len(raw) {
			return nil, nil, fmt.Errorf("第 %d 条目录项越界", i)
		}
		e := icoEntry{
			Width: raw[p], Height: raw[p+1], Colors: raw[p+2], Reserved: raw[p+3],
			Planes:     binary.LittleEndian.Uint16(raw[p+4:]),
			BitCount:   binary.LittleEndian.Uint16(raw[p+6:]),
			BytesInRes: binary.LittleEndian.Uint32(raw[p+8:]),
			Offset:     binary.LittleEndian.Uint32(raw[p+12:]),
		}
		if int(e.Offset)+int(e.BytesInRes) > len(raw) {
			return nil, nil, fmt.Errorf("第 %d 张图的数据越界", i)
		}
		entries[i] = e
		images[i] = raw[e.Offset : e.Offset+e.BytesInRes]
	}
	return entries, images, nil
}

// resource 是要写进 .rsrc 的一条资源。
type resource struct {
	typ, id uint16
	data    []byte
}

// buildResourceSection 拼出 .rsrc 节的内容：
// 三层目录（类型 -> 名称/ID -> 语言）+ 数据项 + 原始数据。
// 返回节内容，以及每个"数据项里 OffsetToData 字段"在节内的偏移（要给它们建重定位）。
func buildResourceSection(entries []icoEntry, images [][]byte) rsrcSection {
	var res []resource
	for i := range images {
		res = append(res, resource{typ: rtIcon, id: uint16(i + 1), data: images[i]})
	}
	// GROUP_ICON：目录头 + 每张图一条 14 字节的项（和 ICONDIRENTRY 一样，
	// 只是最后 4 字节的文件偏移换成 2 字节的 RT_ICON 资源 id）。
	var grp bytes.Buffer
	binary.Write(&grp, binary.LittleEndian, uint16(0)) // 保留
	binary.Write(&grp, binary.LittleEndian, uint16(1)) // 类型：图标
	binary.Write(&grp, binary.LittleEndian, uint16(len(entries)))
	for i, e := range entries {
		grp.Write([]byte{e.Width, e.Height, e.Colors, e.Reserved})
		binary.Write(&grp, binary.LittleEndian, e.Planes)
		binary.Write(&grp, binary.LittleEndian, e.BitCount)
		binary.Write(&grp, binary.LittleEndian, e.BytesInRes)
		binary.Write(&grp, binary.LittleEndian, uint16(i+1)) // 对应的 RT_ICON id
	}
	res = append(res, resource{typ: rtGroupIcon, id: 1, data: grp.Bytes()})

	return layoutResources(res)
}

type rsrcSection struct {
	data []byte
	// relocs 是每个数据项 OffsetToData 字段在节内的偏移。
	// 这些字段存的是"数据在节内的偏移"，链接器按重定位加上节的 RVA 才是最终地址。
	relocs []uint32
}

const (
	dirHeaderSize   = 16 // IMAGE_RESOURCE_DIRECTORY
	dirEntrySize    = 8  // IMAGE_RESOURCE_DIRECTORY_ENTRY
	dataEntrySize   = 16 // IMAGE_RESOURCE_DATA_ENTRY
	subdirFlag      = 0x80000000
	defaultLanguage = 0x0409 // 英语（美国）。图标不含文字，语言无所谓，但必须有一层。
)

// layoutResources 按 PE 的三层资源目录布局算好偏移再写出来。
// 层级：根（按类型）-> 每个类型（按 id）-> 每个 id（按语言）-> 数据项。
func layoutResources(res []resource) rsrcSection {
	// 按类型分组，类型和 id 都要升序——Windows 是二分查找的。
	types := map[uint16][]resource{}
	var typeOrder []uint16
	for _, r := range res {
		if _, ok := types[r.typ]; !ok {
			typeOrder = append(typeOrder, r.typ)
		}
		types[r.typ] = append(types[r.typ], r)
	}
	sortUint16(typeOrder)
	for _, t := range typeOrder {
		sortResByID(types[t])
	}

	// 先算各块大小：根目录、类型目录、语言目录、数据项、数据本身
	total := dirHeaderSize + dirEntrySize*len(typeOrder) // 根
	for _, t := range typeOrder {
		total += dirHeaderSize + dirEntrySize*len(types[t]) // 该类型下按 id
		total += (dirHeaderSize + dirEntrySize) * len(types[t])
	}
	dataEntryStart := total
	nRes := len(res)
	dataStart := dataEntryStart + dataEntrySize*nRes

	buf := make([]byte, dataStart)
	var relocs []uint32
	var payload []byte

	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(buf[off:], v) }
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(buf[off:], v) }

	// 根目录
	pos := 0
	put16(pos+12, 0)                      // 按名字的条目数：没有
	put16(pos+14, uint16(len(typeOrder))) // 按 id 的条目数
	entryPos := pos + dirHeaderSize
	nextDir := dirHeaderSize + dirEntrySize*len(typeOrder)
	dataEntryIdx := 0

	for _, t := range typeOrder {
		list := types[t]
		// 根里的一条：类型 id -> 子目录偏移
		put32(entryPos, uint32(t))
		put32(entryPos+4, uint32(nextDir)|subdirFlag)
		entryPos += dirEntrySize

		// 类型目录
		typeDir := nextDir
		put16(typeDir+12, 0)
		put16(typeDir+14, uint16(len(list)))
		langDir := typeDir + dirHeaderSize + dirEntrySize*len(list)
		idPos := typeDir + dirHeaderSize

		for _, r := range list {
			put32(idPos, uint32(r.id))
			put32(idPos+4, uint32(langDir)|subdirFlag)
			idPos += dirEntrySize

			// 语言目录：只有一条，指向数据项
			put16(langDir+12, 0)
			put16(langDir+14, 1)
			de := dataEntryStart + dataEntryIdx*dataEntrySize
			put32(langDir+dirHeaderSize, defaultLanguage)
			put32(langDir+dirHeaderSize+4, uint32(de)) // 不带 subdirFlag：指向数据项

			// 数据项：偏移先填"节内偏移"，链接器按重定位补上节的 RVA
			dataOff := dataStart + len(payload)
			put32(de, uint32(dataOff))
			put32(de+4, uint32(len(r.data)))
			put32(de+8, 0) // CodePage
			put32(de+12, 0)
			relocs = append(relocs, uint32(de))

			payload = append(payload, r.data...)
			// 每条数据按 8 字节对齐，省得后面的项跨边界
			for len(payload)%8 != 0 {
				payload = append(payload, 0)
			}

			langDir += dirHeaderSize + dirEntrySize
			dataEntryIdx++
		}
		nextDir = langDir
	}

	return rsrcSection{data: append(buf, payload...), relocs: relocs}
}

// buildCOFF 把 .rsrc 节包成一个 COFF 目标文件（amd64）。
// Go 编译时会把同目录下的 .syso 当成外部目标文件交给链接器。
func buildCOFF(rsrc rsrcSection, target coffArch) []byte {
	const sectionRsrcFlag = 0x40000040

	symbolCount := 1 // 一个节符号就够：重定位都指向节首
	sizeOfRelocs := len(rsrc.relocs) * 10
	headerSize := 20 + 40 // 文件头 + 一个节头
	dataOffset := headerSize
	relocOffset := dataOffset + len(rsrc.data)
	symOffset := relocOffset + sizeOfRelocs

	out := make([]byte, symOffset+symbolCount*18+4)
	le := binary.LittleEndian

	// 文件头
	le.PutUint16(out[0:], target.machine)
	le.PutUint16(out[2:], 1) // 节数
	le.PutUint32(out[8:], uint32(symOffset))
	le.PutUint32(out[12:], uint32(symbolCount))

	// 节头
	copy(out[20:28], ".rsrc\x00\x00\x00")
	le.PutUint32(out[20+16:], uint32(len(rsrc.data))) // SizeOfRawData
	le.PutUint32(out[20+20:], uint32(dataOffset))     // PointerToRawData
	le.PutUint32(out[20+24:], uint32(relocOffset))    // PointerToRelocations
	le.PutUint16(out[20+32:], uint16(len(rsrc.relocs)))
	le.PutUint32(out[20+36:], sectionRsrcFlag) // 已初始化数据 + 可读

	copy(out[dataOffset:], rsrc.data)

	for i, r := range rsrc.relocs {
		p := relocOffset + i*10
		le.PutUint32(out[p:], r)   // 要改的位置（节内偏移）
		le.PutUint32(out[p+4:], 0) // 符号表下标：第 0 个，即节符号
		le.PutUint16(out[p+8:], target.relocDir32NB)
	}

	// 符号表：一个 .rsrc 节符号
	copy(out[symOffset:symOffset+8], ".rsrc\x00\x00\x00")
	le.PutUint32(out[symOffset+8:], 0)  // Value：节首
	le.PutUint16(out[symOffset+12:], 1) // SectionNumber：第 1 节
	out[symOffset+16] = 3               // StorageClass = IMAGE_SYM_CLASS_STATIC
	out[symOffset+17] = 0               // 没有辅助符号

	// 字符串表：只有长度字段（4）
	le.PutUint32(out[symOffset+symbolCount*18:], 4)
	return out
}

func sortUint16(a []uint16) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func sortResByID(a []resource) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1].id > a[j].id; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
