package iso

import (
	"strings"
)

// Kind 表示探测出的 ISO 类型。
type Kind string

const (
	KindUnknown Kind = "unknown"
	KindLinux   Kind = "linux"
	KindWindows Kind = "windows"
	KindESXi    Kind = "esxi"
)

// Info 是分析结果。
type Info struct {
	Kind       Kind     `json:"kind"`
	Distro     string   `json:"distro"`      // 发行版标识，如 ubuntu / debian / rocky
	Display    string   `json:"display"`     // 人类可读名称
	Kernel     string   `json:"kernel"`      // ISO 内内核相对路径
	Initrd     string   `json:"initrd"`      // ISO 内 initrd 相对路径
	BootMethod string   `json:"boot_method"` // 推荐引导方式：sanboot/linux/windows/esxi/memdisk
	IsLive     bool     `json:"is_live"`     // 是否 live 系统（可 sanboot / toram）
	Files      []string `json:"files"`       // 全部文件清单
	Note       string   `json:"note"`
}

// rule 是一条 ISO 识别规则（借鉴 Ventoy 的 ISO 支持思路）。
type rule struct {
	distro  string
	display string
	// 特征：ISO 内需存在的路径子串（全部满足）
	markers []string
	// 内核/initrd 候选路径（按序取首个存在的）
	kernels []string
	initrds []string
	// 是否 live（可 sanboot / toram）
	live bool
	// 推荐引导方式；空则由 hasKernel 决定
	method string
}

// rules 是识别规则库。顺序即优先级（越靠前越先匹配）。
var rules = []rule{
	// ---- 专用/工具/虚拟化平台（优先，特征明显）----
	{distro: "proxmox", display: "Proxmox VE",
		markers: []string{"/proxmox", "/boot/linux26"},
		kernels: []string{"/boot/linux26"}, initrds: []string{"/boot/initrd.img"}, live: false},
	{distro: "pfsense", display: "pfSense",
		markers: []string{"/pfsense"}, live: true, method: "sanboot"},
	{distro: "truenas", display: "TrueNAS",
		markers: []string{"/truenas"}, live: true, method: "sanboot"},
	{distro: "clonezilla", display: "Clonezilla",
		markers: []string{"/live/filesystem.squashfs", "/live/vmlinuz"},
		kernels: []string{"/live/vmlinuz"}, initrds: []string{"/live/initrd.img"}, live: true},
	{distro: "gparted", display: "GParted Live",
		markers: []string{"/live/vmlinuz", "/gparted"},
		kernels: []string{"/live/vmlinuz"}, initrds: []string{"/live/initrd.img"}, live: true},
	{distro: "memtest", display: "Memtest86",
		markers: []string{"/memtest"}, method: "memdisk"},

	// ---- 主流发行版 ----
	{distro: "ubuntu", display: "Ubuntu",
		markers: []string{"/casper/vmlinuz"},
		kernels: []string{"/casper/vmlinuz", "/casper/vmlinuz.efi"},
		initrds: []string{"/casper/initrd", "/casper/initrd.lz", "/casper/initrd.gz"}, live: true},
	{distro: "debian", display: "Debian (live)",
		markers: []string{"/live/vmlinuz"},
		kernels: []string{"/live/vmlinuz"}, initrds: []string{"/live/initrd.img"}, live: true},
	{distro: "debian", display: "Debian (installer)",
		markers: []string{"/install.amd/vmlinuz"},
		kernels: []string{"/install.amd/vmlinuz"}, initrds: []string{"/install.amd/initrd.gz"}, live: false},
	{distro: "kali", display: "Kali Linux",
		markers: []string{"/live/vmlinuz", "/.disk/info"}, // 结合卷标再判定
		kernels: []string{"/live/vmlinuz"}, initrds: []string{"/live/initrd.img"}, live: true},
	{distro: "rocky", display: "Rocky Linux",
		markers: []string{"/images/pxeboot/vmlinuz", "/rocky"},
		kernels: []string{"/images/pxeboot/vmlinuz"}, initrds: []string{"/images/pxeboot/initrd.img"}, live: false},
	{distro: "almalinux", display: "AlmaLinux",
		markers: []string{"/images/pxeboot/vmlinuz", "/alma"},
		kernels: []string{"/images/pxeboot/vmlinuz"}, initrds: []string{"/images/pxeboot/initrd.img"}, live: false},
	{distro: "centos", display: "CentOS",
		markers: []string{"/images/pxeboot/vmlinuz", "/centos"},
		kernels: []string{"/images/pxeboot/vmlinuz"}, initrds: []string{"/images/pxeboot/initrd.img"}, live: false},
	{distro: "fedora", display: "Fedora",
		markers: []string{"/images/pxeboot/vmlinuz"},
		kernels: []string{"/images/pxeboot/vmlinuz", "/isolinux/vmlinuz"},
		initrds: []string{"/images/pxeboot/initrd.img", "/isolinux/initrd.img"}, live: false},
	{distro: "rhel", display: "RHEL 系",
		markers: []string{"/images/pxeboot/vmlinuz"},
		kernels: []string{"/images/pxeboot/vmlinuz"}, initrds: []string{"/images/pxeboot/initrd.img"}, live: false},
	{distro: "opensuse", display: "openSUSE",
		markers: []string{"/boot/x86_64/loader/linux"},
		kernels: []string{"/boot/x86_64/loader/linux"}, initrds: []string{"/boot/x86_64/loader/initrd"}, live: false},
	{distro: "arch", display: "Arch Linux",
		markers: []string{"/arch/boot/x86_64/vmlinuz-linux"},
		kernels: []string{"/arch/boot/x86_64/vmlinuz-linux"},
		initrds: []string{"/arch/boot/x86_64/initramfs-linux.img"}, live: true},
	{distro: "alpine", display: "Alpine Linux",
		markers: []string{"/boot/vmlinuz-lts"},
		kernels: []string{"/boot/vmlinuz-lts", "/boot/vmlinuz-virt"},
		initrds: []string{"/boot/initramfs-lts", "/boot/initramfs-virt"}, live: true},
	{distro: "manjaro", display: "Manjaro",
		markers: []string{"/manjaro"}, live: true, method: "sanboot"},
	{distro: "mint", display: "Linux Mint",
		markers: []string{"/casper/vmlinuz", "/.disk/info"},
		kernels: []string{"/casper/vmlinuz"}, initrds: []string{"/casper/initrd.lz", "/casper/initrd"}, live: true},
}

// Analyze 遍历 ISO，探测类型并给出引导建议。
func Analyze(path string) (*Info, error) {
	r, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	info := &Info{Kind: KindUnknown}
	var all []string
	lower := map[string]bool{}

	_ = r.Walk(func(p string, e DirEntry) error {
		all = append(all, p)
		lower[strings.ToLower(p)] = true
		return nil
	})
	info.Files = all

	hasExact := func(p string) bool { return lower[strings.ToLower(p)] }
	hasSub := func(sub string) (string, bool) {
		sub = strings.ToLower(sub)
		for lp := range lower {
			if strings.Contains(lp, sub) {
				return lp, true
			}
		}
		return "", false
	}
	joined := strings.ToLower(strings.Join(all, "\n"))

	// --- Windows 探测（最高优先，特征稳定）---
	_, hasBootWim := hasSub("/sources/boot.wim")
	_, hasBootmgr := hasSub("bootmgr")
	if hasBootWim && hasBootmgr {
		info.Kind = KindWindows
		info.Distro = "windows"
		info.Display = "Windows"
		info.BootMethod = "windows"
		info.Note = "Windows 安装镜像；用 wimboot 引导 WinPE。"
		return info, nil
	}

	// --- ESXi 探测 ---
	if hasExact("/boot.cfg") || func() bool { _, ok := hasSub("/boot.cfg"); return ok }() {
		if _, hasMboot := hasSub("mboot"); hasMboot || strings.Contains(joined, "vmware") {
			info.Kind = KindESXi
			info.Distro = "esxi"
			info.Display = "VMware ESXi"
			info.BootMethod = "esxi"
			info.Note = "VMware ESXi；用 mboot(multiboot) + boot.cfg 引导。"
			return info, nil
		}
	}

	// --- 规则库匹配 ---
	for _, rl := range rules {
		matched := true
		for _, m := range rl.markers {
			if _, ok := hasSub(m); !ok {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		info.Kind = KindLinux
		info.Distro = rl.distro
		info.Display = rl.display
		info.IsLive = rl.live
		for _, k := range rl.kernels {
			if p, ok := hasSub(k); ok {
				info.Kernel = p
				break
			}
		}
		for _, ir := range rl.initrds {
			if p, ok := hasSub(ir); ok {
				info.Initrd = p
				break
			}
		}
		// 决定推荐引导方式
		if rl.method != "" {
			info.BootMethod = rl.method
		} else if info.Kernel != "" {
			// live 系统优先 sanboot（像 Ventoy 一样直挂），失败可切内核方式
			if rl.live {
				info.BootMethod = "sanboot"
			} else {
				info.BootMethod = "linux"
			}
		} else {
			info.BootMethod = "sanboot"
		}
		info.Note = buildNote(info)
		return info, nil
	}

	// --- 通用兜底：找到任意内核则按 Linux，否则 sanboot ---
	kernelFallback := []string{"vmlinuz", "/boot/kernel", "bzimage"}
	initrdFallback := []string{"initrd", "initramfs"}
	for _, c := range kernelFallback {
		if p, ok := hasSub(c); ok {
			info.Kernel = p
			break
		}
	}
	for _, c := range initrdFallback {
		if p, ok := hasSub(c); ok {
			info.Initrd = p
			break
		}
	}
	info.Distro = guessDistro(joined)
	info.Display = info.Distro
	if info.Kernel != "" {
		info.Kind = KindLinux
		info.IsLive = true
		info.BootMethod = "sanboot" // 默认像 Ventoy 一样先尝试直挂
		info.Note = "已探测到内核；默认 sanboot 直挂，若失败可改为内核引导方式。"
	} else {
		info.Kind = KindUnknown
		info.BootMethod = "sanboot"
		info.Note = "未能识别类型；将以 sanboot 直挂 ISO 尝试引导（类似 Ventoy）。"
	}
	return info, nil
}

func buildNote(i *Info) string {
	switch i.BootMethod {
	case "sanboot":
		return i.Display + "（live/工具）：默认 sanboot 直挂 ISO，无需提取内核；若失败可切换为内核引导。"
	case "linux":
		return i.Display + "（安装器）：用 kernel + initrd 引导，会自动填入网络安装参数。"
	case "memdisk":
		return i.Display + "：建议 memdisk 整盘入内存（仅 BIOS）。"
	default:
		return i.Display
	}
}

func guessDistro(joined string) string {
	switch {
	case strings.Contains(joined, "casper"):
		return "ubuntu"
	case strings.Contains(joined, "kali"):
		return "kali"
	case strings.Contains(joined, "debian"):
		return "debian"
	case strings.Contains(joined, "centos"):
		return "centos"
	case strings.Contains(joined, "rocky"):
		return "rocky"
	case strings.Contains(joined, "almalinux"), strings.Contains(joined, "alma"):
		return "almalinux"
	case strings.Contains(joined, "fedora"):
		return "fedora"
	case strings.Contains(joined, "opensuse"), strings.Contains(joined, "suse"):
		return "opensuse"
	case strings.Contains(joined, "arch"):
		return "arch"
	case strings.Contains(joined, "alpine"):
		return "alpine"
	default:
		return "linux"
	}
}
