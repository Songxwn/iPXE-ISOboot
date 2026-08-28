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

// file 表示需准备的引导文件及其下载源。
type file struct {
	name string
	url  string
}

// 官方 iPXE 预编译二进制（boot.ipxe.org）与 wimboot（ipxe.org）。
var files = []file{
	{"undionly.kpxe", "https://boot.ipxe.org/undionly.kpxe"},   // BIOS PXE
	{"ipxe.efi", "https://boot.ipxe.org/ipxe.efi"},             // UEFI x64
	{"ipxe32.efi", "https://boot.ipxe.org/ipxe32.efi"},         // UEFI ia32
	{"ipxe-arm64.efi", "https://boot.ipxe.org/arm64-efi/snponly.efi"}, // UEFI arm64
	{"wimboot", "https://github.com/ipxe/wimboot/releases/latest/download/wimboot"}, // Windows 引导
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
		if err := download(client, f.url, dst); err != nil {
			log.Printf("[bootfiles] 下载 %s 失败: %v", f.name, err)
			continue
		}
		log.Printf("[bootfiles] 已下载 %s", f.name)
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
