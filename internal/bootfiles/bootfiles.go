// Package bootfiles 准备网络引导所需的二进制：iPXE 与 GRUB2 网络镜像。
//
// iPXE 二进制从 boot.ipxe.org 下载。
//
// GRUB2 网络镜像（grubx64.efi / grubia32.efi / grub_bios.pxe）需内嵌一个
// prefix，使其启动后从 (http,server)/grub 读取 grub.cfg。这类镜像最好用
// grub-mkimage 在服务器本地生成（能保证模块齐全、prefix 正确）。因此本包
// 检测 grub-mkimage 是否可用并据此生成；不可用则提示安装。
package bootfiles

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// iPXE 二进制候选源。
var ipxeFiles = []struct {
	name string
	urls []string
}{
	{"undionly.kpxe", []string{"https://boot.ipxe.org/undionly.kpxe"}},
	{"ipxe.efi", []string{
		"https://boot.ipxe.org/x86_64-efi/ipxe.efi",
		"https://boot.ipxe.org/x86_64-efi/snponly.efi",
	}},
	{"ipxe32.efi", []string{
		"https://boot.ipxe.org/i386-efi/ipxe.efi",
		"https://boot.ipxe.org/i386-efi/snponly.efi",
	}},
}

// 通用模块（各平台都有）。
var grubCommonModules = []string{
	"normal", "http", "net", "tftp", "linux", "loopback",
	"iso9660", "part_gpt", "part_msdos", "fat", "ntfs", "ext2", "search",
	"echo", "test", "configfile", "all_video", "gfxterm", "halt", "reboot",
	"chain", "probe", "regexp", "sleep", "cat", "ls", "boot", "gzio",
}

// EFI 平台专用模块。
var grubEFIModules = []string{"efinet", "linuxefi", "efi_gop", "efi_uga"}

// BIOS(i386-pc) 平台专用模块。pxe = BIOS PXE 网络；biosdisk/vbe 等。
var grubBIOSModules = []string{"pxe", "biosdisk", "vbe", "vga"}

// modulesFor 返回指定 grub-mkimage 格式对应的模块集合。
func modulesFor(format string) []string {
	m := append([]string{}, grubCommonModules...)
	if len(format) >= 3 && format[len(format)-3:] == "efi" {
		return append(m, grubEFIModules...)
	}
	// i386-pc / i386-pc-pxe
	return append(m, grubBIOSModules...)
}

// EnsureIPXE 确保 iPXE 二进制就位。
func EnsureIPXE(tftpRoot string) {
	os.MkdirAll(tftpRoot, 0o755)
	client := &http.Client{Timeout: 120 * time.Second}
	for _, f := range ipxeFiles {
		dst := filepath.Join(tftpRoot, f.name)
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			continue
		}
		ok := false
		var last error
		for _, u := range f.urls {
			if err := download(client, u, dst); err != nil {
				last = err
				continue
			}
			log.Printf("[bootfiles] 已下载 %s", f.name)
			ok = true
			break
		}
		if !ok {
			log.Printf("[bootfiles] 下载 %s 失败: %v（可手动放到 %s）", f.name, last, tftpRoot)
		}
	}
}

// GrubStatus 描述 GRUB2 镜像生成工具的可用性。
type GrubStatus struct {
	Mkimage bool   `json:"mkimage"` // grub-mkimage 是否可用
	HasEFI  bool   `json:"has_efi"` // 是否已生成 EFI 镜像
	HasBIOS bool   `json:"has_bios"`
	Hint    string `json:"hint"`
}

// EnsureGRUB 生成 GRUB2 网络镜像到 grubDir。
// serverPrefix 形如 (http,192.168.1.10:8081)/grub，供 grub 启动后定位 grub.cfg。
func EnsureGRUB(grubDir, serverPrefix string) GrubStatus {
	os.MkdirAll(grubDir, 0o755)
	st := GrubStatus{}
	mk := findMkimage()
	st.Mkimage = mk != ""
	if mk == "" {
		st.Hint = "未找到 grub-mkimage，无法生成 GRUB2 网络镜像。请安装：\n" +
			"  Debian/Ubuntu: apt-get install -y grub-efi-amd64-bin grub-pc-bin grub-common\n" +
			"  RHEL/Rocky:    dnf install -y grub2-efi-x64-modules grub2-pc-modules grub2-tools"
		return st
	}

	// 内嵌启动脚本：GRUB 由 iPXE chainload 起来后网络未初始化，需先加载网络
	// 模块并 DHCP，再去 HTTP 拉取真正的 grub.cfg，否则会掉进命令行。
	// EFI 用 efinet，BIOS 用 pxe，模块不同，故分别生成。
	efiEmbed := filepath.Join(grubDir, "embed_efi.cfg")
	os.WriteFile(efiEmbed, []byte(embedScript(serverPrefix, true)), 0o644)
	biosEmbed := filepath.Join(grubDir, "embed_bios.cfg")
	os.WriteFile(biosEmbed, []byte(embedScript(serverPrefix, false)), 0o644)

	// EFI x64
	efi := filepath.Join(grubDir, "grubx64.efi")
	if err := runMkimage(mk, "x86_64-efi", efi, efiEmbed, serverPrefix); err != nil {
		log.Printf("[bootfiles] 生成 grubx64.efi 失败: %v", err)
	} else {
		st.HasEFI = true
		log.Printf("[bootfiles] 已生成 grubx64.efi")
	}
	// EFI ia32（可选，失败不致命）
	ia32 := filepath.Join(grubDir, "grubia32.efi")
	_ = runMkimage(mk, "i386-efi", ia32, efiEmbed, serverPrefix)

	// BIOS PXE
	bios := filepath.Join(grubDir, "grub_bios.pxe")
	if err := runMkimage(mk, "i386-pc-pxe", bios, biosEmbed, serverPrefix); err != nil {
		log.Printf("[bootfiles] 生成 grub_bios.pxe 失败: %v", err)
	} else {
		st.HasBIOS = true
		log.Printf("[bootfiles] 已生成 grub_bios.pxe")
	}

	if !st.HasEFI && !st.HasBIOS {
		st.Hint = "grub-mkimage 存在但生成失败，请检查是否安装了对应平台模块包" +
			"（grub-efi-amd64-bin / grub-pc-bin）。"
	}
	return st
}

// embedScript 生成内嵌启动脚本。efi=true 用 efinet，否则用 BIOS pxe。
func embedScript(prefix string, efi bool) string {
	net := "insmod efinet\n"
	if !efi {
		net = "insmod pxe\n"
	}
	return "insmod part_gpt\ninsmod part_msdos\ninsmod net\n" + net +
		"insmod http\ninsmod tftp\ninsmod normal\ninsmod linux\n" +
		"set prefix=" + prefix + "\n" +
		"if [ -z \"${net_default_ip}\" ]; then net_bootp; fi\n" +
		"configfile " + prefix + "/grub.cfg\n"
}

func runMkimage(mk, format, out, embedCfg, prefix string) error {
	args := []string{"-O", format, "-o", out, "-p", prefix, "-c", embedCfg}
	args = append(args, modulesFor(format)...)
	cmd := exec.Command(mk, args...)
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, string(o))
	}
	return nil
}

func findMkimage() string {
	for _, n := range []string{"grub-mkimage", "grub2-mkimage"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func download(client *http.Client, url, dst string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, url)
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
