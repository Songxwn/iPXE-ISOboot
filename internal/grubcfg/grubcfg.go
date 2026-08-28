// Package grubcfg 生成 GRUB2 的 grub.cfg。
//
// 核心是 Ventoy 同款的 loopback 机制：GRUB2 通过 HTTP 把远程 ISO 当作
// loop 设备挂载，读取其中的内核/initrd/grub.cfg 直接引导，无需提取内核。
//
// 各发行版的 live/安装器对 loopback 挂载需要不同的内核参数（告诉系统
// 从哪个 loop 设备/ISO 找根文件系统），这里按“配方族”生成对应命令。
package grubcfg

import (
	"fmt"
	"strings"

	"ipxe-isoboot/internal/iso"
	"ipxe-isoboot/internal/menu"
)

// Context 提供生成 grub.cfg 所需的服务器信息。
type Context struct {
	HTTPBase string // 形如 http://192.168.1.10:8081
	Timeout  int
	Title    string
}

// Generate 生成完整 grub.cfg。
func Generate(entries []*menu.Entry, ctx Context) string {
	var b strings.Builder
	b.WriteString("set timeout=" + itoa(ctx.Timeout) + "\n")
	b.WriteString("set default=0\n")
	b.WriteString("insmod all_video\ninsmod http\ninsmod loopback\ninsmod iso9660\n")
	b.WriteString("insmod part_gpt\ninsmod part_msdos\ninsmod ntfs\ninsmod fat\n\n")
	// 设置菜单标题
	title := ctx.Title
	if title == "" {
		title = "iPXE-ISOboot"
	}
	b.WriteString("set menu_title=\"" + escapeQuotes(title) + "\"\n\n")

	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		b.WriteString("menuentry " + quote(e.Title) + " {\n")
		if e.CustomCfg != "" {
			// 用户自定义引导片段（原样输出，${iso_url} 会被替换）
			body := strings.ReplaceAll(e.CustomCfg, "${iso_url}", isoURL(ctx.HTTPBase, e))
			b.WriteString(indent(body))
		} else {
			b.WriteString(indent(recipe(e, ctx)))
		}
		b.WriteString("}\n\n")
	}

	// 通用项
	b.WriteString("menuentry \"Reboot\" { reboot }\n")
	b.WriteString("menuentry \"Shutdown\" { halt }\n")
	if hasEFI() {
		b.WriteString("if [ \"${grub_platform}\" = \"efi\" ]; then\n")
		b.WriteString("  menuentry \"UEFI Firmware Settings\" { fwsetup }\nfi\n")
	}
	return b.String()
}

func isoURL(base string, e *menu.Entry) string {
	return base + "/files/iso/" + e.ISOName
}

// recipe 依据配方族生成 loopback 引导命令。
//
// 通用模式：
//
//	set iso=(http,SERVER)/files/iso/xxx.iso   # 通过 HTTP 定位 ISO
//	loopback loop $iso
//	linux (loop)/内核 参数...  findiso/fromiso=... 指向 iso
//	initrd (loop)/initrd
func recipe(e *menu.Entry, ctx Context) string {
	url := isoURL(ctx.HTTPBase, e)
	// GRUB 通过 http 访问：需要 (http,host:port)/path 形式；这里用 GRUB 的
	// 网络设备语法。iPXE chain 到 GRUB 后，GRUB 的 ${net_default_server} 已就绪，
	// 直接用完整 URL 更稳妥。
	var b strings.Builder
	b.WriteString("set isofile=\"" + url + "\"\n")
	b.WriteString("loopback loop \"${isofile}\"\n")

	switch e.Family {
	case iso.FamilyUbuntu, iso.FamilyMint:
		b.WriteString("linux (loop)/casper/vmlinuz boot=casper iso-scan/filename=\"${isofile}\" url=\"${isofile}\" ip=dhcp ---\n")
		b.WriteString("initrd (loop)/casper/initrd\n")
	case iso.FamilyDebianLive, iso.FamilyKali:
		b.WriteString("linux (loop)/live/vmlinuz boot=live fetch=\"${isofile}\" ip=dhcp ---\n")
		b.WriteString("initrd (loop)/live/initrd.img\n")
	case iso.FamilyDebianInst:
		b.WriteString("linux (loop)/install.amd/vmlinuz auto=true priority=critical fetch=\"${isofile}\" ip=dhcp ---\n")
		b.WriteString("initrd (loop)/install.amd/initrd.gz\n")
	case iso.FamilyRHEL:
		b.WriteString("linux (loop)/images/pxeboot/vmlinuz inst.repo=\"${isofile}\" inst.stage2=\"${isofile}\" ip=dhcp\n")
		b.WriteString("initrd (loop)/images/pxeboot/initrd.img\n")
	case iso.FamilyArch:
		b.WriteString("linux (loop)/arch/boot/x86_64/vmlinuz-linux img_dev=/dev/disk/by-label/ARCH img_loop=\"${isofile}\" archisobasedir=arch ip=dhcp\n")
		b.WriteString("initrd (loop)/arch/boot/x86_64/initramfs-linux.img\n")
	case iso.FamilyAlpine:
		b.WriteString("linux (loop)/boot/vmlinuz-lts modloop=\"${isofile}\" alpine_repo= ip=dhcp\n")
		b.WriteString("initrd (loop)/boot/initramfs-lts\n")
	case iso.FamilyOpenSUSE:
		b.WriteString("linux (loop)/boot/x86_64/loader/linux install=\"${isofile}\" ip=dhcp\n")
		b.WriteString("initrd (loop)/boot/x86_64/loader/initrd\n")
	case iso.FamilyWindows:
		// Windows 无法用 GRUB loopback 直接启动安装器；提示改用 wimboot 方式。
		b.WriteString("echo \"Windows ISO cannot boot via GRUB loopback over network.\"\n")
		b.WriteString("echo \"Use wimboot method instead (see docs).\"\n")
		b.WriteString("sleep 5\n")
	default: // generic：尝试 ISO 内 grub.cfg
		b.WriteString("if [ -e (loop)/boot/grub/grub.cfg ]; then\n")
		b.WriteString("  configfile (loop)/boot/grub/grub.cfg\n")
		b.WriteString("elif [ -e (loop)/boot/grub/loopback.cfg ]; then\n")
		b.WriteString("  configfile (loop)/boot/grub/loopback.cfg\n")
		b.WriteString("else\n")
		b.WriteString("  echo \"No grub.cfg found inside ISO; cannot auto-boot.\"\n")
		b.WriteString("  sleep 5\n")
		b.WriteString("fi\n")
	}
	return b.String()
}

func hasEFI() bool { return true }

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func quote(s string) string  { return "\"" + escapeQuotes(s) + "\"" }
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, "\"", "'")
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }
