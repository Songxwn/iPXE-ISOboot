// Package iso 提供最小化的 ISO9660/Joliet 只读读取器，
// 用于探测 ISO 类型并提取 PXE 引导所需的文件（内核、initrd、boot.cfg 等）。
// 纯标准库实现，无外部依赖。
package iso

import (
	"errors"
	"io"
	"os"
	"strings"
)

const sectorSize = 2048

// Reader 读取 ISO9660 镜像。
type Reader struct {
	f        *os.File
	rootLBA  uint32
	rootSize uint32
	joliet   bool
}

// Open 打开 ISO 文件并定位根目录（优先 Joliet 以支持长文件名/大小写）。
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f}

	// 卷描述符从第 16 扇区开始
	var primaryRootLBA, primaryRootSize uint32
	var jolietRootLBA, jolietRootSize uint32
	found := false

	sector := make([]byte, sectorSize)
	for i := 16; i < 100; i++ {
		if _, err := f.ReadAt(sector, int64(i)*sectorSize); err != nil {
			break
		}
		vdType := sector[0]
		id := string(sector[1:6])
		if id != "CD001" {
			continue
		}
		if vdType == 255 { // 卷描述符终止
			break
		}
		if vdType == 1 { // 主卷描述符 (PVD)
			primaryRootLBA, primaryRootSize = parseRootDirEntry(sector[156 : 156+34])
			found = true
		}
		if vdType == 2 { // 补充卷描述符 (SVD) — 检查是否 Joliet
			// Joliet 转义序列 %/@ %/C %/E 位于偏移 88
			esc := sector[88:91]
			if isJolietEscape(esc) {
				jolietRootLBA, jolietRootSize = parseRootDirEntry(sector[156 : 156+34])
			}
		}
	}

	if jolietRootLBA != 0 {
		r.rootLBA = jolietRootLBA
		r.rootSize = jolietRootSize
		r.joliet = true
	} else if found {
		r.rootLBA = primaryRootLBA
		r.rootSize = primaryRootSize
	} else {
		f.Close()
		return nil, errors.New("不是有效的 ISO9660 镜像")
	}
	return r, nil
}

func (r *Reader) Close() error { return r.f.Close() }

func isJolietEscape(b []byte) bool {
	s := string(b)
	return s == "%/@" || s == "%/C" || s == "%/E"
}

func parseRootDirEntry(e []byte) (lba, size uint32) {
	lba = le32(e[2:6])
	size = le32(e[10:14])
	return
}

// DirEntry 表示目录项。
type DirEntry struct {
	Name  string
	LBA   uint32
	Size  uint32
	IsDir bool
}

// readDir 读取指定 LBA/Size 的目录内容。
func (r *Reader) readDir(lba, size uint32) ([]DirEntry, error) {
	data := make([]byte, size)
	if _, err := r.f.ReadAt(data, int64(lba)*sectorSize); err != nil && err != io.EOF {
		return nil, err
	}
	var entries []DirEntry
	pos := 0
	for pos < len(data) {
		recLen := int(data[pos])
		if recLen == 0 {
			// 跳到下一个扇区边界
			next := (pos/sectorSize + 1) * sectorSize
			if next <= pos || next >= len(data) {
				break
			}
			pos = next
			continue
		}
		if pos+recLen > len(data) {
			break
		}
		rec := data[pos : pos+recLen]
		eLBA := le32(rec[2:6])
		eSize := le32(rec[10:14])
		flags := rec[25]
		nameLen := int(rec[32])
		if 33+nameLen > len(rec) {
			pos += recLen
			continue
		}
		rawName := rec[33 : 33+nameLen]

		var name string
		if r.joliet {
			name = decodeUCS2(rawName)
		} else {
			name = string(rawName)
		}
		// 过滤 . 和 ..
		if nameLen == 1 && (rawName[0] == 0 || rawName[0] == 1) {
			pos += recLen
			continue
		}
		// 去掉 ISO9660 版本号 ";1"
		if idx := strings.Index(name, ";"); idx >= 0 {
			name = name[:idx]
		}
		name = strings.TrimSuffix(name, ".")

		entries = append(entries, DirEntry{
			Name:  name,
			LBA:   eLBA,
			Size:  eSize,
			IsDir: flags&0x02 != 0,
		})
		pos += recLen
	}
	return entries, nil
}

// Root 返回根目录项。
func (r *Reader) Root() ([]DirEntry, error) {
	return r.readDir(r.rootLBA, r.rootSize)
}

// ReadDirAt 读取某个子目录项。
func (r *Reader) ReadDirAt(e DirEntry) ([]DirEntry, error) {
	if !e.IsDir {
		return nil, errors.New("不是目录")
	}
	return r.readDir(e.LBA, e.Size)
}

// Walk 递归遍历所有文件，回调收到相对路径（以 / 分隔，小写不变）。
func (r *Reader) Walk(fn func(path string, e DirEntry) error) error {
	root, err := r.Root()
	if err != nil {
		return err
	}
	return r.walk("", root, fn, 0)
}

func (r *Reader) walk(prefix string, entries []DirEntry, fn func(string, DirEntry) error, depth int) error {
	if depth > 16 {
		return nil
	}
	for _, e := range entries {
		p := prefix + "/" + e.Name
		if e.IsDir {
			sub, err := r.ReadDirAt(e)
			if err != nil {
				continue
			}
			if err := r.walk(p, sub, fn, depth+1); err != nil {
				return err
			}
		} else {
			if err := fn(p, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Extract 将 ISO 内某个文件写出到 w。
func (r *Reader) Extract(e DirEntry, w io.Writer) error {
	remaining := int64(e.Size)
	buf := make([]byte, 1<<20)
	off := int64(e.LBA) * sectorSize
	for remaining > 0 {
		n := int64(len(buf))
		if n > remaining {
			n = remaining
		}
		got, err := r.f.ReadAt(buf[:n], off)
		if got > 0 {
			if _, werr := w.Write(buf[:got]); werr != nil {
				return werr
			}
			off += int64(got)
			remaining -= int64(got)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

// ReadFile 读取 ISO 内指定路径的小文件（如 boot.cfg），返回内容。
func (r *Reader) ReadFile(path string) ([]byte, error) {
	e, err := r.Find(path)
	if err != nil {
		return nil, err
	}
	data := make([]byte, e.Size)
	if _, err := r.f.ReadAt(data, int64(e.LBA)*sectorSize); err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}

// Find 按路径查找文件项（大小写不敏感）。
func (r *Reader) Find(path string) (DirEntry, error) {
	parts := strings.Split(strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/"), "/")
	cur, err := r.Root()
	if err != nil {
		return DirEntry{}, err
	}
	for i, part := range parts {
		var match *DirEntry
		for j := range cur {
			if strings.EqualFold(cur[j].Name, part) {
				match = &cur[j]
				break
			}
		}
		if match == nil {
			return DirEntry{}, errors.New("未找到: " + path)
		}
		if i == len(parts)-1 {
			return *match, nil
		}
		cur, err = r.ReadDirAt(*match)
		if err != nil {
			return DirEntry{}, err
		}
	}
	return DirEntry{}, errors.New("未找到: " + path)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// decodeUCS2 解码 Joliet 的 UCS-2 大端文件名。
func decodeUCS2(b []byte) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		r := rune(uint16(b[i])<<8 | uint16(b[i+1]))
		sb.WriteRune(r)
	}
	return sb.String()
}
