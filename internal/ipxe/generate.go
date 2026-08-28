package ipxe

import (
	"fmt"
	"strings"

	"ipxe-isoboot/internal/menu"
)

// BootContext 提供生成脚本所需的服务器信息。
type BootContext struct {
	BaseURL string // 形如 http://192.168.1.10:8081
	Timeout int    // 菜单超时秒数
	Default string // 默认项 ID
}

// Menu 根据菜单项生成完整的 iPXE 主菜单脚本。
func Menu(entries []*menu.Entry, ctx BootContext) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n\n")
	b.WriteString("set base " + ctx.BaseURL + "\n")
	b.WriteString(":start\n")
	b.WriteString("menu iPXE-ISOboot Network Boot Menu\n")

	timeout := ctx.Timeout * 1000
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		line := fmt.Sprintf("item %s %s\n", e.ID, e.Title)
		b.WriteString(line)
	}
	b.WriteString("item --gap -- ----------------------------\n")
	b.WriteString("item shell     iPXE Shell\n")
	b.WriteString("item reboot    Reboot\n")
	b.WriteString("item exit      Boot from local disk\n")

	if ctx.Default != "" {
		b.WriteString(fmt.Sprintf("choose --default %s --timeout %d selected || goto cancel\n", ctx.Default, timeout))
	} else {
		b.WriteString(fmt.Sprintf("choose --timeout %d selected || goto cancel\n", timeout))
	}
	b.WriteString("goto ${selected}\n\n")

	// 通用控制项
	b.WriteString(":cancel\n")
	b.WriteString("echo Cancelled, exiting in 3s...\nsleep 3\ngoto exit\n\n")
	b.WriteString(":shell\nshell\ngoto start\n\n")
	b.WriteString(":reboot\nreboot\n\n")
	b.WriteString(":exit\nexit\n\n")

	// 各启动项标签
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		b.WriteString(fmt.Sprintf(":%s\n", e.ID))
		b.WriteString("echo Loading " + e.Title + " ...\n")
		b.WriteString(EntryScript(e, ctx))
		b.WriteString("goto start\n\n")
	}

	return b.String()
}

// EntryScript 生成单个启动项的 iPXE 命令片段（不含标签）。
func EntryScript(e *menu.Entry, ctx BootContext) string {
	switch e.Type {
	case menu.TypeLinux:
		return linuxScript(e)
	case menu.TypeWindows:
		return windowsScript(e)
	case menu.TypeESXi:
		return esxiScript(e)
	case menu.TypeSanBoot:
		return sanbootScript(e)
	case menu.TypeMemdisk:
		return memdiskScript(e)
	case menu.TypeCustom:
		return e.Script + "\n"
	default:
		return "echo Unknown entry type\n"
	}
}

func url(base, p string) string {
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func linuxScript(e *menu.Entry) string {
	var b strings.Builder
	b.WriteString("kernel " + url("${base}", e.Kernel))
	if e.Append != "" {
		b.WriteString(" " + e.Append)
	}
	if e.ToRAM {
		b.WriteString(" " + toramParams(e.Distro))
	}
	b.WriteString("\n")
	if e.Initrd != "" {
		b.WriteString("initrd " + url("${base}", e.Initrd) + "\n")
	}
	b.WriteString("boot || goto failed\n")
	b.WriteString(":failed\necho Boot failed\nsleep 3\n")
	return b.String()
}

// toramParams 依发行版返回“启动后驻留内存”的内核参数（UEFI/BIOS 通用）。
func toramParams(distro string) string {
	switch distro {
	case "ubuntu":
		return "toram"
	case "debian":
		// Debian live 用 toram；netinst 无此参数（会被忽略）
		return "toram"
	case "centos", "rocky", "almalinux", "fedora":
		return "rd.live.ram=1"
	case "opensuse":
		return "mediacheck=0 toram"
	case "alpine":
		return "" // Alpine 默认即在 RAM 运行
	case "arch":
		return "copytoram=y"
	default:
		return "toram"
	}
}

// memdiskScript 生成把整个 ISO 下载到内存当虚拟光驱的脚本（仅 BIOS）。
func memdiskScript(e *menu.Entry) string {
	var b strings.Builder
	// UEFI 下 memdisk 不可用，给出提示并退回失败分支。
	b.WriteString("iseq ${platform} efi && goto memdisk_uefi ||\n")
	b.WriteString("kernel " + url("${base}", "/files/tftp/memdisk") + " iso raw\n")
	b.WriteString("initrd " + url("${base}", e.SanURL) + "\n")
	b.WriteString("boot || goto failed\n")
	b.WriteString(":memdisk_uefi\n")
	b.WriteString("echo memdisk (whole-ISO to RAM) is BIOS-only and not supported under UEFI.\n")
	b.WriteString("echo Use a Linux entry with 'Load to RAM (toram)' for UEFI instead.\n")
	b.WriteString("sleep 5\n")
	b.WriteString(":failed\necho Boot failed\nsleep 3\n")
	return b.String()
}

func windowsScript(e *menu.Entry) string {
	var b strings.Builder
	wimboot := e.Wimboot
	if wimboot == "" {
		wimboot = "/files/tftp/wimboot"
	}
	b.WriteString("kernel " + url("${base}", wimboot) + " gui\n")
	// wimboot 通过多条 initrd 载入各文件，文件名（第二参数）必须精确匹配。
	// 顺序建议：bootmgr、BCD、boot.sdi、boot.wim。
	if e.Bootmgr != "" {
		b.WriteString("initrd " + url("${base}", e.Bootmgr) + " bootmgr\n")
	}
	if e.BCD != "" {
		b.WriteString("initrd " + url("${base}", e.BCD) + " BCD\n")
	}
	if e.BootSDI != "" {
		b.WriteString("initrd " + url("${base}", e.BootSDI) + " boot.sdi\n")
	}
	if e.BootWIM != "" {
		b.WriteString("initrd " + url("${base}", e.BootWIM) + " boot.wim\n")
	}
	if e.WinExtras != "" {
		b.WriteString(e.WinExtras + "\n")
	}
	b.WriteString("boot || goto failed\n")
	b.WriteString(":failed\necho Boot failed\nsleep 3\n")
	return b.String()
}

func esxiScript(e *menu.Entry) string {
	var b strings.Builder
	// ESXi 使用 mboot（multiboot）。UEFI 用 mboot.efi，BIOS 用 mboot.c32。
	// 通过 iPXE 变量 ${platform} 判断（efi / pcbios）。
	b.WriteString("iseq ${platform} efi && goto esxi_efi || goto esxi_bios\n")
	b.WriteString(":esxi_efi\n")
	if e.MbootEFI != "" {
		b.WriteString("kernel " + url("${base}", e.MbootEFI) + " -c " + url("${base}", e.BootCFG) + "\n")
	}
	b.WriteString("boot || goto failed\n")
	b.WriteString(":esxi_bios\n")
	if e.MbootC32 != "" {
		b.WriteString("kernel " + url("${base}", e.MbootC32) + " -c " + url("${base}", e.BootCFG) + "\n")
	}
	b.WriteString("boot || goto failed\n")
	b.WriteString(":failed\necho Boot failed\nsleep 3\n")
	return b.String()
}

func sanbootScript(e *menu.Entry) string {
	var b strings.Builder
	b.WriteString("sanboot --no-describe " + url("${base}", e.SanURL) + " || goto failed\n")
	b.WriteString(":failed\necho Boot failed\nsleep 3\n")
	return b.String()
}
