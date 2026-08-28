// Package proxydhcp 实现一个最小化的 ProxyDHCP 服务。
//
// ProxyDHCP 与网络中现有的 DHCP 服务器共存：它不分配 IP，只在
// DHCPDISCOVER/REQUEST 时补充 PXE 引导信息（下一跳服务器、启动文件名）。
// 依据 RFC 2131/2132 与 PXE 规范，通过监听 UDP:67 并向 :68 或 :4011 回应。
//
// 支持根据客户端体系结构 (Option 93, Client System Architecture) 区分
// BIOS / UEFI(x64/ia32/arm64) 并下发不同的引导文件。
package proxydhcp

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"sync/atomic"

	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/netif"
)

const (
	opBootRequest = 1
	opBootReply   = 2

	optPad             = 0
	optEnd             = 255
	optMessageType     = 53
	optServerID        = 54
	optParamRequest    = 55
	optVendorClassID   = 60 // "PXEClient"
	optClientArch      = 93 // Client System Architecture
	optUserClass       = 77 // iPXE 会设置为 "iPXE"
	optTFTPServerName  = 66
	optBootFileName    = 67

	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5

	magicCookie = 0x63825363
)

// 客户端体系结构 (RFC 4578 Option 93)
const (
	archBIOS      = 0x0000 // Intel x86PC
	archUEFIx86   = 0x0006 // EFI IA32
	archUEFIx64   = 0x0007 // EFI x86-64
	archUEFIBC    = 0x0009 // EFI x86-64 (BC)
	archUEFIarm64 = 0x000b // EFI ARM64
)

// Server 是 ProxyDHCP 服务。
type Server struct {
	cfg    *config.Config
	conn   *net.UDPConn
	ifc    *net.Interface // 选定的网卡（nil 表示全部）
	closed atomic.Bool    // 是否已请求停止
}

func New(cfg *config.Config) *Server { return &Server{cfg: cfg} }

// Stop 停止服务：关闭监听套接字以中断阻塞的读取循环。
func (s *Server) Stop() {
	s.closed.Store(true)
	if s.conn != nil {
		s.conn.Close()
	}
}

// bootFileFor 根据架构与是否已运行 iPXE 决定下发的文件名。
//
// 逻辑：
//  1. 客户端首次 PXE（固件网卡）→ 下发 iPXE 二进制（TFTP）。
//  2. 客户端已经是 iPXE（UserClass=iPXE）→ 下发 HTTP 上的 boot.ipxe 脚本，
//     避免 iPXE 链式加载自身死循环。
func (s *Server) bootFileFor(arch uint16, isIPXE bool, httpURL string) string {
	if isIPXE {
		return httpURL + "/boot.ipxe"
	}
	switch arch {
	case archUEFIx64, archUEFIBC:
		return "ipxe.efi"
	case archUEFIx86:
		return "ipxe32.efi"
	case archUEFIarm64:
		return "ipxe-arm64.efi"
	default: // BIOS
		return "undionly.kpxe"
	}
}

// Serve 启动监听循环（阻塞）。
func (s *Server) Serve() error {
	// 解析选定网卡（若配置了）。用于来源过滤与 next-server IP。
	s.ifc = netif.FindByName(s.cfg.DHCPInterface)
	if s.cfg.DHCPInterface != "" && s.ifc == nil {
		log.Printf("[proxydhcp] 警告: 找不到网卡 %q，将监听全部网卡", s.cfg.DHCPInterface)
	}

	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 67}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	s.conn = conn
	if s.ifc != nil {
		log.Printf("[proxydhcp] 监听 udp/67 (仅网卡 %s)", s.ifc.Name)
	} else {
		log.Printf("[proxydhcp] 监听 udp/67 (全部网卡)")
	}

	buf := make([]byte, 1500)
	for {
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if s.closed.Load() {
				log.Printf("[proxydhcp] 已停止")
				return nil
			}
			log.Printf("[proxydhcp] 读取错误: %v", err)
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go s.handle(pkt, raddr)
	}
}

func (s *Server) handle(pkt []byte, raddr *net.UDPAddr) {
	msg, err := parse(pkt)
	if err != nil {
		return
	}
	if msg.op != opBootRequest {
		return
	}
	// 只处理 PXEClient
	if !bytes.HasPrefix(msg.options[optVendorClassID], []byte("PXEClient")) {
		return
	}

	mt := byte(0)
	if v := msg.options[optMessageType]; len(v) == 1 {
		mt = v[0]
	}
	if mt != dhcpDiscover && mt != dhcpRequest {
		return
	}

	// 网卡过滤：若指定了网卡，则只响应来自该网卡子网的请求。
	// PXE 裸包源 IP 常为 0.0.0.0（无法判断），此时放行由 OS 交付的包；
	// 经 DHCP relay 转发的请求用 giaddr 判断子网归属。
	if s.ifc != nil {
		if !msg.giaddr.Equal(net.IPv4zero) && !msg.giaddr.IsUnspecified() {
			if !netif.Contains(s.ifc, msg.giaddr) {
				return
			}
		} else if !raddr.IP.Equal(net.IPv4zero) && !raddr.IP.IsUnspecified() {
			if !netif.Contains(s.ifc, raddr.IP) {
				return
			}
		}
	}

	arch := uint16(archBIOS)
	if v := msg.options[optClientArch]; len(v) == 2 {
		arch = binary.BigEndian.Uint16(v)
	}
	isIPXE := bytes.Contains(msg.options[optUserClass], []byte("iPXE"))

	serverIP := s.serverIP(raddr)
	httpURL := "http://" + serverIP.String() + ":" + itoa(s.cfg.HTTPPort)
	bootfile := s.bootFileFor(arch, isIPXE, httpURL)

	replyType := byte(dhcpOffer)
	if mt == dhcpRequest {
		replyType = dhcpAck
	}

	reply := buildReply(msg, serverIP, bootfile, replyType)

	// 广播回复到 68（PXE 客户端在无地址阶段监听广播）。
	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	if raddr.Port == 4011 { // PXE 重定向请求
		dst = &net.UDPAddr{IP: raddr.IP, Port: 4011}
	}
	if _, err := s.conn.WriteToUDP(reply, dst); err != nil {
		log.Printf("[proxydhcp] 发送失败: %v", err)
		return
	}
	log.Printf("[proxydhcp] %s arch=0x%04x ipxe=%v -> %s", macStr(msg.chaddr), arch, isIPXE, bootfile)
}

func (s *Server) serverIP(raddr *net.UDPAddr) net.IP {
	if s.cfg.ServerIP != "" {
		if ip := net.ParseIP(s.cfg.ServerIP); ip != nil {
			return ip.To4()
		}
	}
	// 若指定了网卡，用该网卡的 IP 作为 next-server。
	if s.ifc != nil {
		if ip := netif.PrimaryIPv4(s.ifc); ip != nil {
			return ip
		}
	}
	return localIPFor(raddr.IP)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
