package web

import (
	"io"
	"path/filepath"
	"strings"
)

// slug 从标题生成安全的 iPXE 标签 ID（仅 [a-z0-9_-]）。
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-' || r == '.':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	return out
}

// sanitizeName 清理文件名，防止路径穿越。
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// copyBuf 用大缓冲区流式拷贝（适合数 GB 的 ISO）。
func copyBuf(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 4<<20) // 4MB
	return io.CopyBuffer(dst, src, buf)
}
