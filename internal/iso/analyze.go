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
	Kind    Kind     `json:"kind"`
	Distro  string   `json:"distro"`  // 猜测的发行版名，如 ubuntu / centos / debian
	Kernel  string   `json:"kernel"`  // ISO 内内核相对路径
	Initrd  string   `json:"initrd"`  // ISO 内 initrd 相对路径
	Files   []string `json:"files"`   // 全部文件清单（相对路径，便于前端选择）
	Note    string   `json:"note"`
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
	lower := map[string]string{} // 小写路径 -> 原始路径

	_ = r.Walk(func(p string, e DirEntry) error {
		all = append(all, p)
		lower[strings.ToLower(p)] = p
		return nil
	})
	info.Files = all

	has := func(sub string) (string, bool) {
		sub = strings.ToLower(sub)
		for lp, orig := range lower {
			if strings.Contains(lp, sub) {
				return orig, true
			}
		}
		return "", false
	}

	// --- Windows 探测 ---
	_, hasBootWim := has("/sources/boot.wim")
	_, hasBootmgr := has("bootmgr")
	if hasBootWim && hasBootmgr {
		info.Kind = KindWindows
		info.Distro = "windows"
		info.Note = "Windows 安装镜像；使用 wimboot 启动。需将 ISO 完整内容通过 SMB/HTTP 提供，或用 wimboot 加载 boot.wim。"
		return info, nil
	}

	// --- ESXi 探测 ---
	if bootCfg, ok := has("/boot.cfg"); ok {
		if _, hasMboot := has("mboot"); hasMboot || strings.Contains(strings.ToLower(strings.Join(all, "\n")), "vmware") {
			info.Kind = KindESXi
			info.Distro = "esxi"
			info.Note = "VMware ESXi；使用 mboot(multiboot) + 修改后的 boot.cfg 启动。boot.cfg=" + bootCfg
			return info, nil
		}
	}

	// --- Linux 探测 ---
	// 常见内核位置。注意顺序：Debian/RHEL 的网络安装内核优先于光盘 isolinux 内核。
	kernelCandidates := []string{
		"/install.amd/vmlinuz",         // Debian netinst (网络安装器)
		"/install.a64/vmlinuz",         // Debian arm64
		"/install/vmlinuz",             // Debian 部分变体
		"/images/pxeboot/vmlinuz",      // RHEL/CentOS/Rocky (网络安装)
		"/casper/vmlinuz",              // Ubuntu live
		"/live/vmlinuz",                // Debian/其它 live
		"/boot/vmlinuz",
		"/isolinux/vmlinuz",
		"/arch/boot/x86_64/vmlinuz-linux",
		"/vmlinuz",
		"/boot/x86_64/loader/linux",
	}
	initrdCandidates := []string{
		"/install.amd/initrd.gz",       // Debian netinst
		"/install.a64/initrd.gz",       // Debian arm64
		"/install/initrd.gz",
		"/images/pxeboot/initrd.img",   // RHEL/CentOS/Rocky
		"/casper/initrd",               // Ubuntu live
		"/live/initrd.img",
		"/boot/initrd.img",
		"/isolinux/initrd.img",
		"/arch/boot/x86_64/initramfs-linux.img",
		"/initrd.img",
		"/boot/x86_64/loader/initrd",
	}
	for _, c := range kernelCandidates {
		if p, ok := has(c); ok {
			info.Kernel = p
			break
		}
	}
	for _, c := range initrdCandidates {
		if p, ok := has(c); ok {
			info.Initrd = p
			break
		}
	}
	// 兜底：任意 vmlinuz / initrd
	if info.Kernel == "" {
		if p, ok := has("vmlinuz"); ok {
			info.Kernel = p
		}
	}
	if info.Initrd == "" {
		if p, ok := has("initrd"); ok {
			info.Initrd = p
		}
	}

	if info.Kernel != "" {
		info.Kind = KindLinux
		info.Distro = guessDistro(all)
		info.Note = "Linux 发行版；已探测内核/initrd，请根据发行版补充内核参数（如指向 ISO 的 URL）。"
		return info, nil
	}

	info.Note = "无法自动识别，请手动配置启动项，或使用 sanboot 直接挂载 ISO。"
	return info, nil
}

func guessDistro(files []string) string {
	joined := strings.ToLower(strings.Join(files, "\n"))
	switch {
	case strings.Contains(joined, "casper"):
		return "ubuntu"
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
