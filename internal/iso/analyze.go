package iso

import "strings"

// Info 是 ISO 分析结果。
type Info struct {
	Family  string   `json:"family"`  // 引导配方族，决定 GRUB loopback 命令
	Distro  string   `json:"distro"`  // 发行版标识
	Display string   `json:"display"` // 人类可读名称
	Files   []string `json:"files"`   // 文件清单
	Note    string   `json:"note"`
}

// 配方族常量。每个族对应 grubcfg 包里一套 loopback 引导命令。
const (
	FamilyUbuntu     = "ubuntu"     // casper live
	FamilyDebianLive = "debian"     // debian live
	FamilyDebianInst = "debian_di"  // debian installer
	FamilyRHEL       = "rhel"       // RHEL/Rocky/Alma/CentOS/Fedora anaconda
	FamilyArch       = "arch"       // archiso
	FamilyAlpine     = "alpine"
	FamilyOpenSUSE   = "opensuse"
	FamilyMint       = "mint"
	FamilyKali       = "kali"
	FamilyWindows    = "windows"
	FamilyGeneric    = "generic" // 通用：尝试 ISO 内 /boot/grub/grub.cfg
)

type rule struct {
	family  string
	distro  string
	display string
	markers []string // 需全部命中（子串匹配）
}

var rules = []rule{
	{FamilyWindows, "windows", "Windows", []string{"/sources/boot.wim", "bootmgr"}},
	{FamilyUbuntu, "ubuntu", "Ubuntu", []string{"/casper/vmlinuz"}},
	{FamilyMint, "mint", "Linux Mint", []string{"/casper/vmlinuz", "/.disk/info"}},
	{FamilyKali, "kali", "Kali Linux", []string{"/live/vmlinuz", "/live/filesystem.squashfs"}},
	{FamilyDebianLive, "debian", "Debian Live", []string{"/live/vmlinuz"}},
	{FamilyDebianInst, "debian", "Debian Installer", []string{"/install.amd/vmlinuz"}},
	{FamilyRHEL, "rhel", "RHEL/Rocky/Alma/Fedora", []string{"/images/pxeboot/vmlinuz"}},
	{FamilyRHEL, "rhel", "RHEL (anaconda)", []string{"/isolinux/vmlinuz", "/.treeinfo"}},
	{FamilyArch, "arch", "Arch Linux", []string{"/arch/boot/x86_64/vmlinuz-linux"}},
	{FamilyAlpine, "alpine", "Alpine Linux", []string{"/boot/vmlinuz-lts"}},
	{FamilyOpenSUSE, "opensuse", "openSUSE", []string{"/boot/x86_64/loader/linux"}},
}

// Analyze 分析 ISO，返回配方族与识别信息。
func Analyze(path string) (*Info, error) {
	r, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	set := map[string]bool{}
	var files []string
	r.Walk(func(p string) {
		files = append(files, p)
		set[strings.ToLower(p)] = true
	})

	hasSub := func(sub string) bool {
		sub = strings.ToLower(sub)
		for k := range set {
			if strings.Contains(k, sub) {
				return true
			}
		}
		return false
	}

	info := &Info{Files: files}
	for _, rl := range rules {
		ok := true
		for _, m := range rl.markers {
			if !hasSub(m) {
				ok = false
				break
			}
		}
		if ok {
			info.Family, info.Distro, info.Display = rl.family, rl.distro, rl.display
			info.Note = rl.display + "：使用 GRUB2 loopback 挂载 ISO 引导（Ventoy 同款机制）。"
			return info, nil
		}
	}

	// 兜底：通用族，尝试 ISO 内 grub.cfg / isolinux
	info.Family = FamilyGeneric
	info.Distro = "linux"
	info.Display = "Generic ISO"
	info.Note = "未精确识别：通用 loopback 引导，尝试 ISO 内 /boot/grub/grub.cfg。"
	return info, nil
}
