// Package fat 生成一个最小化的 FAT16 文件系统镜像，
// 用于作为 El Torito EFI 引导镜像（EFI System Partition）。
// 纯标准库实现，仅支持写入少量根目录/子目录文件，足够放置 iPXE EFI 与脚本。
package fat

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

const (
	sectorSize     = 512
	sectorsPerClus = 4 // 每簇 4 扇区 = 2KB
	clusterSize    = sectorSize * sectorsPerClus
	reservedSecs   = 1
	numFATs        = 2
	rootEntries    = 512 // 根目录项数
)

// file 表示要写入的文件（含所在目录路径）。
type file struct {
	dir  string // 相对目录，如 "" 或 "EFI/BOOT"
	name string // 文件名 8.3
	data []byte
}

// Builder 收集文件并生成 FAT16 镜像。
type Builder struct {
	files []file
}

func New() *Builder { return &Builder{} }

// AddFile 添加一个文件，path 形如 "EFI/BOOT/BOOTX64.EFI"。
func (b *Builder) AddFile(path string, data []byte) {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "/")
	idx := strings.LastIndex(path, "/")
	dir, name := "", path
	if idx >= 0 {
		dir = path[:idx]
		name = path[idx+1:]
	}
	b.files = append(b.files, file{dir: strings.ToUpper(dir), name: strings.ToUpper(name), data: data})
}

// dirNode 用于构建目录树。
type dirNode struct {
	name     string
	children map[string]*dirNode
	files    []file
	cluster  uint16
}

// Build 生成 FAT16 镜像字节。镜像总大小按内容向上取整到至少 2.88MB。
func (b *Builder) Build() ([]byte, error) {
	// 计算所需数据簇数
	var dataClusters int
	countClusters := func(size int) int {
		if size == 0 {
			return 1
		}
		return (size + clusterSize - 1) / clusterSize
	}

	// 构建目录树
	root := &dirNode{children: map[string]*dirNode{}}
	for _, f := range b.files {
		node := root
		if f.dir != "" {
			for _, part := range strings.Split(f.dir, "/") {
				if part == "" {
					continue
				}
				if node.children[part] == nil {
					node.children[part] = &dirNode{name: part, children: map[string]*dirNode{}}
				}
				node = node.children[part]
			}
		}
		node.files = append(node.files, f)
		dataClusters += countClusters(len(f.data))
	}

	// 子目录本身各占 1 簇（简化：假定每目录项数不超过一簇）
	var subdirs []*dirNode
	var collect func(n *dirNode)
	collect = func(n *dirNode) {
		for _, c := range n.children {
			subdirs = append(subdirs, c)
			dataClusters += 1
			collect(c)
		}
	}
	collect(root)

	// 至少保证一定容量，避免过小
	if dataClusters < 64 {
		dataClusters = 64
	}
	totalClusters := dataClusters + 16 // 余量

	// FAT16 每项 2 字节
	fatBytes := (totalClusters + 2) * 2
	fatSecs := (fatBytes + sectorSize - 1) / sectorSize
	rootDirSecs := (rootEntries*32 + sectorSize - 1) / sectorSize

	dataStartSec := reservedSecs + numFATs*fatSecs + rootDirSecs
	totalSecs := dataStartSec + totalClusters*sectorsPerClus

	img := make([]byte, totalSecs*sectorSize)

	// ---- 引导扇区 (BPB) ----
	bs := img[0:sectorSize]
	bs[0], bs[1], bs[2] = 0xEB, 0x3C, 0x90 // jmp
	copy(bs[3:11], []byte("MSWIN4.1"))
	binary.LittleEndian.PutUint16(bs[11:13], sectorSize)
	bs[13] = sectorsPerClus
	binary.LittleEndian.PutUint16(bs[14:16], reservedSecs)
	bs[16] = numFATs
	binary.LittleEndian.PutUint16(bs[17:19], rootEntries)
	if totalSecs < 0x10000 {
		binary.LittleEndian.PutUint16(bs[19:21], uint16(totalSecs))
	} else {
		binary.LittleEndian.PutUint32(bs[32:36], uint32(totalSecs))
	}
	bs[21] = 0xF8 // 媒体类型：固定盘
	binary.LittleEndian.PutUint16(bs[22:24], uint16(fatSecs))
	binary.LittleEndian.PutUint16(bs[24:26], 32) // 每磁道扇区
	binary.LittleEndian.PutUint16(bs[26:28], 64) // 磁头数
	bs[38] = 0x29                                // 扩展引导标记
	binary.LittleEndian.PutUint32(bs[39:43], 0x12345678)
	copy(bs[43:54], []byte("IPXEBOOT   "))
	copy(bs[54:62], []byte("FAT16   "))
	bs[510], bs[511] = 0x55, 0xAA

	// ---- 分配簇 ----
	// FAT[0],FAT[1] 保留；数据簇从 2 开始
	fat := make([]uint16, totalClusters+2)
	fat[0] = 0xFFF8
	fat[1] = 0xFFFF
	nextFree := uint16(2)

	alloc := func(nClus int) []uint16 {
		var chain []uint16
		for i := 0; i < nClus; i++ {
			chain = append(chain, nextFree)
			nextFree++
		}
		for i := 0; i < len(chain); i++ {
			if i == len(chain)-1 {
				fat[chain[i]] = 0xFFFF
			} else {
				fat[chain[i]] = chain[i+1]
			}
		}
		return chain
	}

	dataArea := img[dataStartSec*sectorSize:]
	writeCluster := func(clus uint16, data []byte) {
		off := int(clus-2) * clusterSize
		if off+len(data) <= len(dataArea) {
			copy(dataArea[off:], data)
		}
	}

	// 先给所有子目录分配簇号
	for _, d := range subdirs {
		c := alloc(1)
		d.cluster = c[0]
	}

	// 写文件数据，记录每文件起始簇
	type placed struct {
		f     file
		clus  uint16
		size  int
	}
	var writeFiles func(n *dirNode) []placed
	writeFiles = func(n *dirNode) []placed {
		var out []placed
		for _, f := range n.files {
			nClus := countClusters(len(f.data))
			chain := alloc(nClus)
			for i, cl := range chain {
				start := i * clusterSize
				end := start + clusterSize
				if end > len(f.data) {
					end = len(f.data)
				}
				if start < len(f.data) {
					writeCluster(cl, f.data[start:end])
				}
			}
			var first uint16
			if len(chain) > 0 {
				first = chain[0]
			}
			out = append(out, placed{f: f, clus: first, size: len(f.data)})
		}
		return out
	}

	// 生成目录项字节
	makeEntries := func(files []placed, subs map[string]*dirNode, self, parent uint16, withDots bool) []byte {
		var buf bytes.Buffer
		if withDots {
			buf.Write(dirEntry(".", 0x10, self, 0))
			buf.Write(dirEntry("..", 0x10, parent, 0))
		}
		for name, d := range subs {
			buf.Write(dirEntry(name, 0x10, d.cluster, 0))
		}
		for _, p := range files {
			buf.Write(dirEntry(p.f.name, 0x20, p.clus, uint32(p.size)))
		}
		return buf.Bytes()
	}

	// 递归写子目录内容
	var processDir func(n *dirNode, self, parent uint16)
	processDir = func(n *dirNode, self, parent uint16) {
		pf := writeFiles(n)
		ents := makeEntries(pf, n.children, self, parent, self != 0)
		if self == 0 {
			// 根目录写到 root dir 区
			rootOff := (reservedSecs + numFATs*fatSecs) * sectorSize
			copy(img[rootOff:], ents)
		} else {
			writeCluster(self, ents)
		}
		for _, c := range n.children {
			processDir(c, c.cluster, self)
		}
	}
	processDir(root, 0, 0)

	// ---- 写 FAT 表 ----
	fatOff := reservedSecs * sectorSize
	for copyIdx := 0; copyIdx < numFATs; copyIdx++ {
		base := fatOff + copyIdx*fatSecs*sectorSize
		for i, v := range fat {
			binary.LittleEndian.PutUint16(img[base+i*2:], v)
		}
	}

	if nextFree-2 > uint16(totalClusters) {
		return nil, errors.New("fat: 簇溢出")
	}
	return img, nil
}

// dirEntry 生成 32 字节 8.3 目录项。
func dirEntry(name string, attr byte, cluster uint16, size uint32) []byte {
	e := make([]byte, 32)
	n, ext := split83(name)
	copy(e[0:8], n)
	copy(e[8:11], ext)
	e[11] = attr
	// 时间戳
	t := time.Now()
	dosTime := uint16(t.Hour()<<11 | t.Minute()<<5 | t.Second()/2)
	dosDate := uint16((t.Year()-1980)<<9 | int(t.Month())<<5 | t.Day())
	binary.LittleEndian.PutUint16(e[22:24], dosTime)
	binary.LittleEndian.PutUint16(e[24:26], dosDate)
	binary.LittleEndian.PutUint16(e[26:28], cluster)
	binary.LittleEndian.PutUint32(e[28:32], size)
	return e
}

func split83(name string) (string, string) {
	name = strings.ToUpper(name)
	if name == "." {
		return padRight(".", 8), padRight("", 3)
	}
	if name == ".." {
		return padRight("..", 8), padRight("", 3)
	}
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
	return padRight(base, 8), padRight(ext, 3)
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
