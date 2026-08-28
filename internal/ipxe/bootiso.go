package ipxe

import (
	"fmt"
	"strings"
)

// BootISOParams 描述生成自定义引导 ISO 时用户可配置的项。
type BootISOParams struct {
	ChainURL string // iPXE 启动后 chain 的目标地址，如 http://192.168.1.10:8081/boot.ipxe

	// 网卡获取 IP 方式
	IPMode  string // "dhcp" 或 "static"
	NetIf   string // 指定网卡，如 net0；空=自动尝试所有网卡
	IP      string // 静态：IP 地址
	Netmask string // 静态：子网掩码
	Gateway string // 静态：网关
	DNS     string // 静态：DNS

	// VLAN
	VLANID int // >0 时在指定网卡上创建 VLAN

	// 其他
	Timeout int // 连接重试/等待秒数
}

// AutoExec 生成 iPXE 的 autoexec.ipxe 脚本内容。
//
// iPXE 从其启动介质（ISO/U 盘的 FAT 或光盘根目录）自动读取并执行本脚本。
// 脚本负责：配置网卡（DHCP 或静态、可选 VLAN），随后 chain 到服务器菜单。
func AutoExec(p BootISOParams) string {
	var b strings.Builder
	b.WriteString("#!ipxe\n\n")
	b.WriteString("echo iPXE-ISOboot custom boot disk\n")
	b.WriteString("echo Initializing network...\n\n")

	iface := p.NetIf
	if iface == "" {
		iface = "net0"
	}

	// VLAN：在物理网卡上创建 VLAN 接口
	if p.VLANID > 0 {
		b.WriteString(fmt.Sprintf("echo Creating VLAN %d ...\n", p.VLANID))
		b.WriteString(fmt.Sprintf("vcreate --tag %d %s || echo VLAN creation failed\n", p.VLANID, iface))
		// VLAN 接口通常命名为 <iface>-<tag>
		iface = fmt.Sprintf("%s-%d", iface, p.VLANID)
	}

	switch strings.ToLower(p.IPMode) {
	case "static":
		b.WriteString("echo Using static IP configuration\n")
		b.WriteString(fmt.Sprintf("set %s/ip %s\n", iface, p.IP))
		if p.Netmask != "" {
			b.WriteString(fmt.Sprintf("set %s/netmask %s\n", iface, p.Netmask))
		}
		if p.Gateway != "" {
			b.WriteString(fmt.Sprintf("set %s/gateway %s\n", iface, p.Gateway))
		}
		if p.DNS != "" {
			b.WriteString(fmt.Sprintf("set %s/dns %s\n", iface, p.DNS))
		}
		b.WriteString(fmt.Sprintf("ifopen %s\n", iface))
	default: // dhcp
		b.WriteString("echo Acquiring address via DHCP\n")
		if p.NetIf != "" || p.VLANID > 0 {
			b.WriteString(fmt.Sprintf("dhcp %s || goto retry\n", iface))
		} else {
			b.WriteString("dhcp || goto retry\n")
		}
	}

	b.WriteString("\n")
	b.WriteString("echo Connecting to server: " + p.ChainURL + "\n")
	b.WriteString("chain " + p.ChainURL + " || goto retry\n\n")

	// 失败重试
	b.WriteString(":retry\n")
	to := p.Timeout
	if to <= 0 {
		to = 5
	}
	b.WriteString(fmt.Sprintf("echo Network or connection failed, retrying in %ds...\n", to))
	b.WriteString(fmt.Sprintf("sleep %d\n", to))
	b.WriteString("echo Press Ctrl-B for the iPXE command line to troubleshoot\n")
	b.WriteString("reboot\n")

	return b.String()
}
