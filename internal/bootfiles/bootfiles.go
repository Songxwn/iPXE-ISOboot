// Package bootfiles 负责准备 PXE 引导所需的 iPXE 二进制与 wimboot。
//
// 首次运行时，如目标文件缺失，会尝试从官方地址下载。若无外网，
// 用户可手动将这些文件放入 TFTP 根目录。
package bootfiles

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// file 表示需准备的引导文件及其候选下载源（按顺序尝试）。
type file struct {
	name string
	urls []string
}

// 官方 iPXE 预编译二进制（boot.ipxe.org）与 wimboot（ipxe.org）。
// boot.ipxe.org 的 EFI 二进制位于各架构子目录下（根目录无 ipxe.efi），
// 因此提供多个候选路径以增强兼容性。
var files = []file{
	{"undionly.kpxe", []string{
		"https://boot.ipxe.org/undionly.kpxe",
	}}, // BIOS PXE
	{"ipxe.efi", []string{
		"https://boot.ipxe.org/x86_64-efi/ipxe.efi",
		"https://boot.ipxe.org/x86_64-efi/snponly.efi",
	}}, // UEFI x64
	{"ipxe32.efi", []string{
		"https://boot.ipxe.org/i386-efi/ipxe.efi",
		"https://boot.ipxe.org/i386-efi/snponly.efi",
	}}, // UEFI ia32
	{"ipxe-arm64.efi", []string{
		"https://boot.ipxe.org/arm64-efi/snponly.efi",
		"https://boot.ipxe.org/arm64-efi/ipxe.efi",
	}}, // UEFI arm64
	{"wimboot", []string{
		"https://github.com/ipxe/wimboot/releases/latest/download/wimboot",
	}}, // Windows 引导
}

// Ensure 确保引导文件就位，缺失则下载。
func Ensure(tftpRoot string) error {
	if err := os.MkdirAll(tftpRoot, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 120 * time.Second}
	for _, f := range files {
		dst := filepath.Join(tftpRoot, f.name)
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			continue // 已存在
		}
		var lastErr error
		ok := false
		for _, u := range f.urls {
			if err := download(client, u, dst); err != nil {
				lastErr = err
				continue
			}
			log.Printf("[bootfiles] 已下载 %s (来源 %s)", f.name, u)
			ok = true
			break
		}
		if !ok {
			log.Printf("[bootfiles] 下载 %s 失败: %v（可手动放置到 %s）", f.name, lastErr, tftpRoot)
		}
	}
	return nil
}

func download(client *http.Client, url, dst string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpError{resp.StatusCode, url}
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dst)
}

type httpError struct {
	code int
	url  string
}

func (e *httpError) Error() string {
	return "HTTP " + itoa(e.code) + " " + e.url
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
