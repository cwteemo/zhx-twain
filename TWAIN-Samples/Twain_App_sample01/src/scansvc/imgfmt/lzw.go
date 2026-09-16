package imgfmt

// TIFF 用的 LZW 压缩。
//
// 标准库的 compress/lzw 看着像能用，其实不行：TIFF 这一版有个 "early change"——
// 码长要在字典**还差一个就满**的时候提前加宽（511 而不是 512 时就换 10 位）。
// 标准库没有这个行为，写出来的文件在 Photoshop、看图软件、libtiff 里都是花的。
// 所以这里按 TIFF 6.0 规范 Section 13 自己写一份，一共几十行。

import "io"

const (
	lzwClearCode = 256  // 字典重置
	lzwEOICode   = 257  // 数据结束
	lzwFirstCode = 258  // 前 258 个码是保留的
	lzwMaxCode   = 4094 // 到这个数就必须重置，码长最多 12 位
)

// lzwWriter 把字节流压成 TIFF 规范的 LZW 码流，高位在前。
type lzwWriter struct {
	w io.Writer

	// dict 的键是 (前缀码 << 8 | 后一个字节)，值是这个组合对应的码。
	dict     map[uint32]uint16
	nextCode uint16
	width    uint  // 当前码长，9~12
	prefix   int32 // 还没输出的前缀码，-1 表示还没开始

	acc     uint32 // 攒够 8 位就吐一个字节
	accBits uint
	buf     []byte
	err     error
}

func newLZWWriter(w io.Writer) *lzwWriter {
	e := &lzwWriter{w: w, prefix: -1}
	e.reset()
	e.emit(lzwClearCode) // 规范要求码流以 Clear 开头
	return e
}

func (e *lzwWriter) reset() {
	e.dict = make(map[uint32]uint16, 1<<11)
	e.nextCode = lzwFirstCode
	e.width = 9
}

func (e *lzwWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	for _, b := range p {
		if e.prefix < 0 {
			e.prefix = int32(b)
			continue
		}
		key := uint32(e.prefix)<<8 | uint32(b)
		if code, ok := e.dict[key]; ok {
			e.prefix = int32(code)
			continue
		}

		e.emit(uint16(e.prefix))
		e.dict[key] = e.nextCode
		e.nextCode++

		switch {
		case e.nextCode >= lzwMaxCode:
			// 字典满了：发个 Clear 重新开始。解码器看到 Clear 会做同样的事。
			e.emit(lzwClearCode)
			e.reset()
		case e.nextCode == 1<<e.width:
			// 加宽时机：解码器那边是"下一个待填条目 +1 到了 2^码长"就加宽
			// （x/image/tiff/lzw 里的 hi+1 >= overflow，注释直言这个 +1 就是 TIFF 和
			// 标准 LZW 的差别）。本编码器的 nextCode 正好等于解码器的 hi+1，
			// 所以条件写成 nextCode == 2^码长。差一位，整个码流就对不上，
			// 解码器会直接报 "lzw: invalid code"。
			e.width++
		}
		e.prefix = int32(b)
	}
	if e.err != nil {
		return 0, e.err
	}
	return len(p), nil
}

// Close 把剩下的前缀和结束码写出去并冲刷位缓冲。写完必须调，否则文件是坏的。
func (e *lzwWriter) Close() error {
	if e.err != nil {
		return e.err
	}
	if e.prefix >= 0 {
		e.emit(uint16(e.prefix))
		e.prefix = -1
	}
	e.emit(lzwEOICode)
	if e.accBits > 0 {
		// 不足 8 位的用 0 补齐
		e.buf = append(e.buf, byte(e.acc<<(8-e.accBits)))
		e.acc, e.accBits = 0, 0
	}
	e.flush()
	return e.err
}

// emit 按当前码长写一个码，高位在前。
func (e *lzwWriter) emit(code uint16) {
	if e.err != nil {
		return
	}
	e.acc = e.acc<<e.width | uint32(code)
	e.accBits += e.width
	for e.accBits >= 8 {
		e.accBits -= 8
		e.buf = append(e.buf, byte(e.acc>>e.accBits))
	}
	e.acc &= 1<<e.accBits - 1
	if len(e.buf) >= 4096 {
		e.flush()
	}
}

func (e *lzwWriter) flush() {
	if e.err != nil || len(e.buf) == 0 {
		return
	}
	_, e.err = e.w.Write(e.buf)
	e.buf = e.buf[:0]
}
