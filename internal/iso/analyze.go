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

	vol := strings.ToLower(r.VolumeID())
	info := &Info{Files: files}

	// Windows 专项兜底：安装 ISO 常用 UDF，ISO9660/Joliet 下文件可能读不全，
	// 因此除标记外，再用 bootmgr / sources/install / 卷标关键词判定。
	winByFile := hasSub("bootmgr") || hasSub("/sources/install") || hasSub("/sources/boot.wim")
	winByVol := strings.Contains(vol, "windows") || strings.Contains(vol, "cccoma") ||
		strings.Contains(vol, "cpba") || strings.Contains(vol, "ccsa") ||
		strings.Contains(vol, "cena") || strings.Contains(vol, "sss_x64") ||
		strings.Contains(vol, "j_ccsa") || strings.Contains(vol, "server")
	if winByFile || winByVol {
		info.Family, info.Distro, info.Display = FamilyWindows, "windows", "Windows"
		info.Note = "Windows 安装镜像（卷标: " + r.VolumeID() + "）。" +
			"注意：Windows 无法通过网络 GRUB loopback 引导安装器，需用 wimboot + WinPE 方式，或改用其它工具。"
		return info, nil
	}

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
	info.Display = "Generic ISO (卷标: " + r.VolumeID() + ", 文件数: " + itoa(len(files)) + ")"
	info.Note = "未精确识别：通用 loopback 引导，尝试 ISO 内 /boot/grub/grub.cfg。"
	return info, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for n > 0 {
		p--
		b[p] = byte('0' + n%10)
		n /= 10
	}
	return string(b[p:])
}
