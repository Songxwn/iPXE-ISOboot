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
	b.WriteString("menu iPXE-ISOboot 网络装机菜单\n")

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
	b.WriteString("item reboot    重启\n")
	b.WriteString("item exit      退出到本地磁盘\n")

	if ctx.Default != "" {
		b.WriteString(fmt.Sprintf("choose --default %s --timeout %d selected || goto cancel\n", ctx.Default, timeout))
	} else {
		b.WriteString(fmt.Sprintf("choose --timeout %d selected || goto cancel\n", timeout))
	}
	b.WriteString("goto ${selected}\n\n")

	// 通用控制项
	b.WriteString(":cancel\n")
	b.WriteString("echo 已取消，3 秒后退出...\nsleep 3\ngoto exit\n\n")
	b.WriteString(":shell\nshell\ngoto start\n\n")
	b.WriteString(":reboot\nreboot\n\n")
	b.WriteString(":exit\nexit\n\n")

	// 各启动项标签
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		b.WriteString(fmt.Sprintf(":%s\n", e.ID))
		b.WriteString("echo 正在加载 " + e.Title + " ...\n")
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
	case menu.TypeCustom:
		return e.Script + "\n"
	default:
		return "echo 未知启动项类型\n"
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
	b.WriteString("\n")
	if e.Initrd != "" {
		b.WriteString("initrd " + url("${base}", e.Initrd) + "\n")
	}
	b.WriteString("boot || goto failed\n")
	b.WriteString(":failed\necho 启动失败\nsleep 3\n")
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
	b.WriteString(":failed\necho 启动失败\nsleep 3\n")
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
	b.WriteString(":failed\necho 启动失败\nsleep 3\n")
	return b.String()
}

func sanbootScript(e *menu.Entry) string {
	var b strings.Builder
	b.WriteString("sanboot --no-describe " + url("${base}", e.SanURL) + " || goto failed\n")
	b.WriteString(":failed\necho 启动失败\nsleep 3\n")
	return b.String()
}
