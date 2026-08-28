// Package isogen 生成可启动的 ISO9660 光盘镜像，支持 El Torito
// 双引导：BIOS（no-emulation 引导镜像）与 UEFI（EFI 引导镜像/ESP）。
//
// 纯标准库实现，无外部工具依赖。专用于打包 iPXE 引导盘：
//   - BIOS：引导镜像为 iPXE 的 ipxe.lkrn（自带引导头，no-emulation 启动）
//   - UEFI：引导镜像为一个 FAT16 ESP，内含 EFI/BOOT/BOOTX64.EFI
package isogen

import (
	"encoding/binary"
	"sort"
	"strings"
)

const sector = 2048

// entry 是要写入 ISO 的一个文件。
type entry struct {
	path string // 相对根，形如 "AUTOEXEC.IPX" 或 "IPXE.LKR"
	data []byte
	lba  uint32
	size uint32
}

// Builder 构建 ISO。
type Builder struct {
	volumeID string
	files    []*entry

	biosImage []byte // El Torito BIOS 引导镜像（no-emulation），一般是 ipxe.lkrn
	efiImage  []byte // El Torito EFI 引导镜像（FAT ESP）
}

func New(volumeID string) *Builder {
	if volumeID == "" {
		volumeID = "IPXE_BOOT"
	}
	return &Builder{volumeID: strings.ToUpper(volumeID)}
}

// AddFile 向 ISO 根目录添加普通文件（供 iPXE 读取，如 autoexec.ipxe）。
func (b *Builder) AddFile(name string, data []byte) {
	b.files = append(b.files, &entry{path: isoName(name), data: data})
}

// SetBIOSBoot 设置 BIOS El Torito 引导镜像（no-emulation）。
func (b *Builder) SetBIOSBoot(img []byte) { b.biosImage = img }

// SetEFIBoot 设置 UEFI El Torito 引导镜像（FAT ESP）。
func (b *Builder) SetEFIBoot(img []byte) { b.efiImage = img }

// sectorsFor 返回容纳 n 字节所需的扇区数。
func sectorsFor(n int) uint32 { return uint32((n + sector - 1) / sector) }

// Build 生成完整 ISO 字节。
//
// 布局（每项按扇区对齐）：
//   0..15     系统区（全 0）
//   16        主卷描述符 (PVD)
//   17        引导记录卷描述符 (El Torito)
//   18        卷描述符终止符
//   19        引导目录 (Boot Catalog)
//   20        根目录记录
//   21..      引导镜像 + 各文件数据
func (b *Builder) Build() []byte {
	// 收集引导镜像作为“隐藏文件”参与布局
	bios := &entry{path: "", data: b.biosImage}
	efi := &entry{path: "", data: b.efiImage}

	// 排序普通文件（ISO9660 目录要求名称升序）
	sort.Slice(b.files, func(i, j int) bool { return b.files[i].path < b.files[j].path })

	// 分配 LBA
	const (
		pvdLBA     = 16
		bootRecLBA = 17
		termLBA    = 18
		catalogLBA = 19
		rootLBA    = 20
	)
	lba := uint32(21)

	if len(bios.data) > 0 {
		bios.lba = lba
		bios.size = uint32(len(bios.data))
		lba += sectorsFor(len(bios.data))
	}
	if len(efi.data) > 0 {
		efi.lba = lba
		efi.size = uint32(len(efi.data))
		lba += sectorsFor(len(efi.data))
	}
	for _, f := range b.files {
		f.lba = lba
		f.size = uint32(len(f.data))
		if len(f.data) == 0 {
			lba += 1
		} else {
			lba += sectorsFor(len(f.data))
		}
	}
	totalSectors := lba

	img := make([]byte, int(totalSectors)*sector)

	// ---- PVD ----
	writePVD(img[pvdLBA*sector:], b.volumeID, totalSectors, rootLBA)

	// ---- Boot Record Volume Descriptor (El Torito) ----
	writeBootRecord(img[bootRecLBA*sector:], catalogLBA)

	// ---- Volume Descriptor Set Terminator ----
	term := img[termLBA*sector:]
	term[0] = 255
	copy(term[1:6], []byte("CD001"))
	term[6] = 1

	// ---- Boot Catalog ----
	writeBootCatalog(img[catalogLBA*sector:], bios, efi)

	// ---- 根目录记录 ----
	writeRootDir(img[rootLBA*sector:], rootLBA, b.files)

	// ---- 写入引导镜像与文件数据 ----
	if len(bios.data) > 0 {
		copy(img[bios.lba*sector:], bios.data)
	}
	if len(efi.data) > 0 {
		copy(img[efi.lba*sector:], efi.data)
	}
	for _, f := range b.files {
		copy(img[f.lba*sector:], f.data)
	}

	return img
}

// writePVD 写主卷描述符。
func writePVD(s []byte, volID string, totalSectors, rootLBA uint32) {
	s[0] = 1 // 类型：主卷描述符
	copy(s[1:6], []byte("CD001"))
	s[6] = 1 // 版本
	// 系统标识 (8..40)、卷标识 (40..72)
	copy(s[8:40], []byte(padRight("", 32)))
	copy(s[40:72], []byte(padRight(volID, 32)))
	// 卷空间大小 (80..88) both-endian
	both32(s[80:88], totalSectors)
	// 卷集大小 (120), 序号 (124)
	both16(s[120:124], 1)
	both16(s[124:128], 1)
	// 逻辑块大小 (128..132)
	both16(s[128:132], sector)
	// 路径表大小 (132..140) —— 简化：置 0（多数固件不强制）
	both32(s[132:140], 0)

	// 根目录记录 (156..190)，34 字节
	writeDirRecord(s[156:190], rootLBA, sector, true, "\x00")

	// 各字符串字段填空格
	for _, r := range [][2]int{{190, 318}, {318, 446}, {446, 574}, {574, 702}, {702, 739}} {
		for i := r[0]; i < r[1]; i++ {
			s[i] = ' '
		}
	}
	s[881] = 1 // 文件结构版本
}

// writeBootRecord 写 El Torito 引导记录卷描述符。
func writeBootRecord(s []byte, catalogLBA uint32) {
	s[0] = 0 // 引导记录
	copy(s[1:6], []byte("CD001"))
	s[6] = 1
	copy(s[7:39], []byte(padRight("EL TORITO SPECIFICATION", 32)))
	// 71..75：引导目录 LBA
	binary.LittleEndian.PutUint32(s[71:75], catalogLBA)
}

// writeBootCatalog 写引导目录（含 BIOS 默认项 + EFI 段）。
func writeBootCatalog(s []byte, bios, efi *entry) {
	// --- 验证项 (Validation Entry, 32 字节) ---
	s[0] = 1    // header id
	s[1] = 0    // 平台：0=80x86
	// 25..27 保留
	copy(s[28:], []byte{}) // ID 字符串留空
	// 校验和：使全项 16 位求和为 0
	s[30] = 0x55
	s[31] = 0xAA
	setCatalogChecksum(s[0:32])

	off := 32
	// --- 默认引导项 (BIOS) ---
	if len(bios.data) > 0 {
		e := s[off : off+32]
		e[0] = 0x88 // 可引导
		e[1] = 0    // no emulation
		binary.LittleEndian.PutUint16(e[2:4], 0)                  // load segment (0=默认 0x7C0)
		e[4] = 0                                                  // 系统类型
		binary.LittleEndian.PutUint16(e[6:8], bootSectorCount(bios.size)) // 扇区数(512 计)
		binary.LittleEndian.PutUint32(e[8:12], bios.lba)          // 引导镜像 LBA (2048 计)
		off += 32
	} else {
		// 无 BIOS 镜像也要占位一个非引导默认项
		off += 32
	}

	// --- EFI 段头 + 段内项 ---
	if len(efi.data) > 0 {
		sh := s[off : off+32]
		sh[0] = 0x91 // 段头，final=? 用 0x91 表示最后一个 section header
		sh[1] = 0xEF // 平台：0xEF = EFI
		binary.LittleEndian.PutUint16(sh[2:4], 1) // 段内项数
		off += 32

		e := s[off : off+32]
		e[0] = 0x88 // 可引导
		e[1] = 0    // no emulation
		binary.LittleEndian.PutUint16(e[2:4], 0)
		binary.LittleEndian.PutUint16(e[6:8], bootSectorCount(efi.size))
		binary.LittleEndian.PutUint32(e[8:12], efi.lba)
		off += 32
	}
}

// bootSectorCount 返回 El Torito “扇区计数”字段（以 512 字节虚拟扇区计）。
func bootSectorCount(size uint32) uint16 {
	n := (size + 511) / 512
	if n > 0xFFFF {
		return 0xFFFF
	}
	if n == 0 {
		return 1
	}
	return uint16(n)
}

func setCatalogChecksum(s []byte) {
	var sum uint16
	for i := 0; i < len(s); i += 2 {
		sum += binary.LittleEndian.Uint16(s[i : i+2])
	}
	// checksum 位于 28..30，先清零再补足
	cur := binary.LittleEndian.Uint16(s[28:30])
	sum -= cur
	binary.LittleEndian.PutUint16(s[28:30], uint16(0x10000-uint32(sum)&0xFFFF))
}

// writeRootDir 写根目录区（含 . 与 .. 及各文件项）。
func writeRootDir(s []byte, rootLBA uint32, files []*entry) {
	pos := 0
	pos += writeDirRecord(s[pos:], rootLBA, sector, true, "\x00")   // .
	pos += writeDirRecord(s[pos:], rootLBA, sector, true, "\x01")   // ..
	for _, f := range files {
		rec := makeDirRecord(f.lba, f.size, false, f.path)
		// 目录记录不得跨越扇区边界
		if pos%sector+len(rec) > sector {
			pos = (pos/sector + 1) * sector
		}
		copy(s[pos:], rec)
		pos += len(rec)
	}
}

// writeDirRecord 在 dst 写入一条目录记录，返回长度。
func writeDirRecord(dst []byte, lba, size uint32, isDir bool, name string) int {
	rec := makeDirRecord(lba, size, isDir, name)
	copy(dst, rec)
	return len(rec)
}

// makeDirRecord 构造一条 ISO9660 目录记录。
func makeDirRecord(lba, size uint32, isDir bool, name string) []byte {
	nameLen := len(name)
	recLen := 33 + nameLen
	if recLen%2 != 0 {
		recLen++ // 偶数对齐（含填充）
	}
	r := make([]byte, recLen)
	r[0] = byte(recLen)
	r[1] = 0 // 扩展属性长度
	both32(r[2:10], lba)
	both32(r[10:18], size)
	// 日期 (18..25) 全 0 即可
	if isDir {
		r[25] = 0x02 // 目录标志
	}
	r[26] = 0 // 交错单元
	r[27] = 0 // 交错间隔
	both16(r[28:32], 1) // 卷序号
	r[32] = byte(nameLen)
	copy(r[33:33+nameLen], []byte(name))
	return r
}

// isoName 规范化为 ISO9660 Level 1 兼容名（大写 8.3 + ;1）。
func isoName(name string) string {
	name = strings.ToUpper(name)
	name = strings.ReplaceAll(name, "/", "")
	base, ext := name, ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		base = name[:i]
		ext = name[i+1:]
	}
	if len(base) > 8 {
		base = base[:8]
	}
	if len(ext) > 3 {
		ext = ext[:3]
	}
	if ext != "" {
		return base + "." + ext + ";1"
	}
	return base + ";1"
}

// both16 写 both-endian 16 位（先小端后大端）。
func both16(dst []byte, v uint16) {
	binary.LittleEndian.PutUint16(dst[0:2], v)
	binary.BigEndian.PutUint16(dst[2:4], v)
}

// both32 写 both-endian 32 位。
func both32(dst []byte, v uint32) {
	binary.LittleEndian.PutUint32(dst[0:4], v)
	binary.BigEndian.PutUint32(dst[4:8], v)
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	if len(s) > n {
		s = s[:n]
	}
	return s
}
