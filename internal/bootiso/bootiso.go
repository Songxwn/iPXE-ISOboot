// Package bootiso 生成自定义 iPXE 引导 ISO：把用户配置生成的
// autoexec.ipxe 与预置的 iPXE 二进制打包为 UEFI/BIOS 双启动光盘。
//
// 实现要点（纯 Go，无外部工具）：
//   - BIOS：以 iPXE 的 ipxe.lkrn 作为 El Torito no-emulation 引导镜像。
//     iPXE lkrn 自带引导头，可直接被 BIOS 从光盘引导。
//   - UEFI：构造一个 FAT16 ESP 作为 El Torito EFI 引导镜像，内含
//     EFI/BOOT/BOOTX64.EFI（= ipxe.efi）。
//   - 自定义脚本：iPXE 二进制均为“可从启动盘读取 autoexec.ipxe”的构建
//     （boot.ipxe.org 的 snponly/ipxe 默认支持 VLAN/DHCP/chain 命令）。
//     脚本同时写入 ISO 根目录，并作为 initrd 附加，最大化兼容性。
package bootiso

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"ipxe-isoboot/internal/fat"
	"ipxe-isoboot/internal/ipxe"
	"ipxe-isoboot/internal/isogen"
)

// 预置 iPXE 二进制（含 EMBED 脚本可读取启动盘 autoexec 的通用构建）。
// 这些是 boot.ipxe.org 官方预编译产物。
// 候选下载源（按顺序尝试）。boot.ipxe.org 的 EFI 位于架构子目录。
var (
	lkrnURLs = []string{
		"https://boot.ipxe.org/ipxe.lkrn", // BIOS 可引导内核镜像
	}
	efiURLs = []string{
		"https://boot.ipxe.org/x86_64-efi/ipxe.efi", // UEFI x64
		"https://boot.ipxe.org/x86_64-efi/snponly.efi",
	}
)

// Generate 生成引导 ISO 并返回其字节内容。
//
// tftpRoot 用作缓存 iPXE 二进制的目录（复用已下载文件）。
func Generate(tftpRoot string, p ipxe.BootISOParams) ([]byte, error) {
	script := []byte(ipxe.AutoExec(p))

	lkrn, err := fetchCached(filepath.Join(tftpRoot, "ipxe.lkrn"), lkrnURLs)
	if err != nil {
		return nil, fmt.Errorf("获取 ipxe.lkrn 失败: %w", err)
	}
	efi, err := fetchCached(filepath.Join(tftpRoot, "ipxe.efi"), efiURLs)
	if err != nil {
		return nil, fmt.Errorf("获取 ipxe.efi 失败: %w", err)
	}

	// 构造 UEFI 引导用的 FAT ESP
	esp := fat.New()
	esp.AddFile("EFI/BOOT/BOOTX64.EFI", efi)
	esp.AddFile("AUTOEXEC.IPXE", script) // iPXE efi 可从 ESP 读取
	espImg, err := esp.Build()
	if err != nil {
		return nil, fmt.Errorf("构造 EFI 引导镜像失败: %w", err)
	}

	// 组装 ISO
	b := isogen.New("IPXE_BOOT")
	b.SetBIOSBoot(lkrn)   // BIOS：no-emulation 引导 ipxe.lkrn
	b.SetEFIBoot(espImg)  // UEFI：EFI 段引导 FAT ESP
	b.AddFile("AUTOEXEC.IPXE", script)
	b.AddFile("README.TXT", []byte(
		"iPXE-ISOboot 自定义引导盘\r\n"+
			"启动后将自动连接: "+p.ChainURL+"\r\n"))

	return b.Build(), nil
}

// fetchCached 若本地缓存存在则直接返回，否则按候选 URL 顺序下载并缓存。
func fetchCached(path string, urls []string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return data, nil
	}
	client := &http.Client{Timeout: 120 * time.Second}
	var lastErr error
	for _, url := range urls {
		data, err := fetchURL(client, url)
		if err != nil {
			lastErr = err
			continue
		}
		_ = os.WriteFile(path, data, 0o644)
		return data, nil
	}
	return nil, lastErr
}

// fetchURL 下载单个 URL 的内容。
func fetchURL(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}
