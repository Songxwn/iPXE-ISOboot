// Package bootiso 生成自定义 iPXE 引导 ISO：把用户配置生成的
// autoexec.ipxe 与预置的 iPXE 二进制打包为 UEFI/BIOS 双启动光盘。
//
// 生成真正可被主流固件引导的双启动 ISO 需要与 iPXE 官方 genfsimg 相同的
// 工具链（参见 https://github.com/ipxe/ipxe util/genfsimg）：
//   - xorriso（或 genisoimage/mkisofs）：组装 ISO9660 + El Torito
//   - mtools（mformat/mcopy）：构造 UEFI 用的 FAT ESP
//   - isolinux.bin / ldlinux.c32（syslinux）：BIOS 引导加载器
//
// 这些在 Debian/Ubuntu 上通过以下命令安装：
//
//	apt-get install -y xorriso mtools isolinux syslinux-common
//
// 本包检测工具是否可用；不可用时返回清晰的错误提示，指导用户安装。
package bootiso

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"ipxe-isoboot/internal/ipxe"
)

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

// ToolStatus 描述生成 ISO 所需外部工具的可用情况。
type ToolStatus struct {
	Xorriso  bool `json:"xorriso"`
	Mformat  bool `json:"mformat"`
	Mcopy    bool `json:"mcopy"`
	Isolinux bool `json:"isolinux"` // 是否找到 isolinux.bin
	OK       bool `json:"ok"`       // 是否满足生成条件
	Hint     string `json:"hint"`   // 缺失时的安装提示
}

// CheckTools 检测生成引导 ISO 所需的工具链。
func CheckTools() ToolStatus {
	st := ToolStatus{}
	st.Xorriso = hasCmd("xorriso")
	st.Mformat = hasCmd("mformat")
	st.Mcopy = hasCmd("mcopy")
	st.Isolinux = findIsolinux() != ""
	st.OK = st.Xorriso && st.Mformat && st.Mcopy && st.Isolinux
	if !st.OK {
		st.Hint = "缺少生成 ISO 所需工具，请在服务器执行：\n" +
			"  Debian/Ubuntu: apt-get install -y xorriso mtools isolinux syslinux-common\n" +
			"  RHEL/Rocky:    dnf install -y xorriso mtools syslinux"
	}
	return st
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// findIsolinux 在常见路径查找 isolinux.bin。
func findIsolinux() string {
	for _, dir := range []string{
		"/usr/lib/ISOLINUX",
		"/usr/lib/syslinux",
		"/usr/share/syslinux",
		"/usr/lib/syslinux/bios",
		"/usr/share/syslinux/bios",
	} {
		p := filepath.Join(dir, "isolinux.bin")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func findSyslinuxFile(name string) string {
	for _, dir := range []string{
		"/usr/lib/ISOLINUX",
		"/usr/lib/syslinux",
		"/usr/lib/syslinux/modules/bios",
		"/usr/share/syslinux",
		"/usr/share/syslinux/modules/bios",
		"/usr/lib/syslinux/bios",
		"/usr/share/syslinux/bios",
	} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// Generate 生成引导 ISO 并返回其字节内容。
//
// tftpRoot 用作缓存 iPXE 二进制的目录（复用已下载文件）。
func Generate(tftpRoot string, p ipxe.BootISOParams) ([]byte, error) {
	st := CheckTools()
	if !st.OK {
		return nil, errors.New(st.Hint)
	}

	script := []byte(ipxe.AutoExec(p))

	lkrn, err := fetchCached(filepath.Join(tftpRoot, "ipxe.lkrn"), lkrnURLs)
	if err != nil {
		return nil, fmt.Errorf("获取 ipxe.lkrn 失败: %w", err)
	}
	efi, err := fetchCached(filepath.Join(tftpRoot, "ipxe.efi"), efiURLs)
	if err != nil {
		return nil, fmt.Errorf("获取 ipxe.efi 失败: %w", err)
	}

	// 临时工作目录
	work, err := os.MkdirTemp("", "ipxeiso")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	isoDir := filepath.Join(work, "iso")
	fatDir := filepath.Join(work, "fat", "EFI", "BOOT")
	os.MkdirAll(isoDir, 0o755)
	os.MkdirAll(fatDir, 0o755)

	// --- BIOS 侧：isolinux + ipxe.lkrn + autoexec(initrd) ---
	if err := copyFile(findIsolinux(), filepath.Join(isoDir, "isolinux.bin")); err != nil {
		return nil, fmt.Errorf("复制 isolinux.bin 失败: %w", err)
	}
	if ld := findSyslinuxFile("ldlinux.c32"); ld != "" {
		_ = copyFile(ld, filepath.Join(isoDir, "ldlinux.c32"))
	}
	if err := os.WriteFile(filepath.Join(isoDir, "ipxe.lkrn"), lkrn, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(isoDir, "autoexec.ipxe"), script, 0o644); err != nil {
		return nil, err
	}
	cfg := "SAY iPXE-ISOboot 引导盘\nTIMEOUT 30\nDEFAULT ipxe\nLABEL ipxe\n KERNEL ipxe.lkrn\n APPEND initrd=autoexec.ipxe\n"
	if err := os.WriteFile(filepath.Join(isoDir, "isolinux.cfg"), []byte(cfg), 0o644); err != nil {
		return nil, err
	}

	// --- UEFI 侧：FAT ESP，内含 EFI/BOOT/BOOTX64.EFI + autoexec ---
	os.WriteFile(filepath.Join(fatDir, "BOOTX64.EFI"), efi, 0o644)
	os.WriteFile(filepath.Join(work, "fat", "autoexec.ipxe"), script, 0o644)
	espImg := filepath.Join(isoDir, "esp.img")
	if err := buildESP(espImg, filepath.Join(work, "fat")); err != nil {
		return nil, fmt.Errorf("构造 EFI 引导镜像失败: %w", err)
	}

	// --- 用 xorriso 组装双启动 ISO ---
	outISO := filepath.Join(work, "out.iso")
	args := []string{
		"-as", "mkisofs",
		"-quiet",
		"-volid", "IPXE_BOOT",
		"-J", "-R", "-l",
		"-no-emul-boot", "-eltorito-boot", "isolinux.bin",
		"-boot-load-size", "4", "-boot-info-table",
		"-eltorito-catalog", "boot.cat",
		"-eltorito-alt-boot", "-no-emul-boot", "-e", "esp.img",
		"-isohybrid-gpt-basdat",
		"-o", outISO,
		isoDir,
	}
	cmd := exec.Command("xorriso", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("xorriso 失败: %v\n%s", err, string(out))
	}

	return os.ReadFile(outISO)
}

// buildESP 用 mtools 从目录构造 FAT ESP 镜像。
func buildESP(imgPath, srcDir string) error {
	// 计算大小（KB）并留余量
	var used int64
	filepath.Walk(srcDir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			used += fi.Size()
		}
		return nil
	})
	sizeKB := used/1024 + 512 // 余量
	if sizeKB < 1440 {
		sizeKB = 1440
	}

	f, err := os.Create(imgPath)
	if err != nil {
		return err
	}
	if err := f.Truncate(sizeKB * 1024); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// mformat 建立 FAT 文件系统
	if out, err := exec.Command("mformat", "-i", imgPath, "-v", "IPXE", "::").CombinedOutput(); err != nil {
		return fmt.Errorf("mformat: %v\n%s", err, out)
	}
	// mcopy 递归复制目录内容
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		if out, err := exec.Command("mcopy", "-s", "-i", imgPath, src, "::").CombinedOutput(); err != nil {
			return fmt.Errorf("mcopy %s: %v\n%s", e.Name(), err, out)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	if src == "" {
		return errors.New("源文件不存在")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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
