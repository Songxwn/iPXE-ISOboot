// Package iso 提供最小化 ISO9660/Joliet 只读读取器，用于探测 ISO 类型。
// 纯标准库实现。
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
	volID    string // 卷标 (volume identifier)
}

// VolumeID 返回卷标。
func (r *Reader) VolumeID() string { return r.volID }

// Open 打开 ISO 并定位根目录（优先 Joliet）。
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f}
	var pLBA, pSize, jLBA, jSize uint32
	found := false
	sector := make([]byte, sectorSize)
	for i := 16; i < 100; i++ {
		if _, err := f.ReadAt(sector, int64(i)*sectorSize); err != nil {
			break
		}
		if string(sector[1:6]) != "CD001" {
			continue
		}
		switch sector[0] {
		case 255:
			i = 100
		case 1:
			pLBA, pSize = le32(sector[158:162]), le32(sector[166:170])
			r.volID = strings.TrimSpace(string(sector[40:72]))
			found = true
		case 2:
			esc := string(sector[88:91])
			if esc == "%/@" || esc == "%/C" || esc == "%/E" {
				jLBA, jSize = le32(sector[158:162]), le32(sector[166:170])
			}
		}
	}
	if jLBA != 0 {
		r.rootLBA, r.rootSize, r.joliet = jLBA, jSize, true
	} else if found {
		r.rootLBA, r.rootSize = pLBA, pSize
	} else {
		f.Close()
		return nil, errors.New("不是有效的 ISO9660 镜像")
	}
	return r, nil
}

func (r *Reader) Close() error { return r.f.Close() }

// DirEntry 表示目录项。
type DirEntry struct {
	Name  string
	LBA   uint32
	Size  uint32
	IsDir bool
}

func (r *Reader) readDir(lba, size uint32) ([]DirEntry, error) {
	data := make([]byte, size)
	if _, err := r.f.ReadAt(data, int64(lba)*sectorSize); err != nil && err != io.EOF {
		return nil, err
	}
	var out []DirEntry
	pos := 0
	for pos < len(data) {
		recLen := int(data[pos])
		if recLen == 0 {
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
		eLBA, eSize, flags := le32(rec[2:6]), le32(rec[10:14]), rec[25]
		nameLen := int(rec[32])
		if 33+nameLen > len(rec) {
			pos += recLen
			continue
		}
		raw := rec[33 : 33+nameLen]
		if nameLen == 1 && (raw[0] == 0 || raw[0] == 1) {
			pos += recLen
			continue
		}
		var name string
		if r.joliet {
			name = decodeUCS2(raw)
		} else {
			name = string(raw)
		}
		if i := strings.Index(name, ";"); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSuffix(name, ".")
		out = append(out, DirEntry{Name: name, LBA: eLBA, Size: eSize, IsDir: flags&0x02 != 0})
		pos += recLen
	}
	return out, nil
}

// Walk 递归遍历所有文件路径（小写以 / 分隔），最多 depth 层。
func (r *Reader) Walk(fn func(path string)) error {
	root, err := r.readDir(r.rootLBA, r.rootSize)
	if err != nil {
		return err
	}
	r.walk("", root, fn, 0)
	return nil
}

func (r *Reader) walk(prefix string, entries []DirEntry, fn func(string), depth int) {
	if depth > 12 {
		return
	}
	for _, e := range entries {
		p := prefix + "/" + e.Name
		if e.IsDir {
			if sub, err := r.readDir(e.LBA, e.Size); err == nil {
				r.walk(p, sub, fn, depth+1)
			}
		} else {
			fn(p)
		}
	}
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func decodeUCS2(b []byte) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		sb.WriteRune(rune(uint16(b[i])<<8 | uint16(b[i+1])))
	}
	return sb.String()
}
