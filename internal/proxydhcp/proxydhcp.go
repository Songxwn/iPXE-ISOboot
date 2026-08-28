// Package proxydhcp 实现最小化 ProxyDHCP：与现有 DHCP 共存，仅补充 PXE
// 引导信息（按客户端架构下发 iPXE 二进制）。默认关闭，可在 Web 开启。
package proxydhcp

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"

	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/netif"
)

const (
	optMessageType   = 53
	optServerID      = 54
	optVendorClassID = 60
	optClientArch    = 93
	optUserClass     = 77
	optTFTPServer    = 66
	optBootFile      = 67
	optEnd           = 255
	magicCookie      = 0x63825363

	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5
)

// 客户端架构 (RFC 4578 option 93)
const (
	archBIOS      = 0x0000
	archUEFIx86   = 0x0006
	archUEFIx64   = 0x0007
	archUEFIBC    = 0x0009
	archUEFIarm64 = 0x000b
)

// Server 是 ProxyDHCP 服务。
type Server struct {
	cfg  *config.Config
	conn *net.UDPConn
	ifc  *net.Interface
}

func New(cfg *config.Config) *Server { return &Server{cfg: cfg} }

func (s *Server) bootFile(arch uint16, isIPXE bool, httpURL string) string {
	if isIPXE {
		return httpURL + "/boot.ipxe"
	}
	switch arch {
	case archUEFIx64, archUEFIBC:
		return "ipxe.efi"
	case archUEFIx86:
		return "ipxe32.efi"
	case archUEFIarm64:
		return "ipxe.efi"
	default:
		return "undionly.kpxe"
	}
}

// Serve 阻塞运行监听循环。
func (s *Server) Serve() error {
	s.ifc = netif.FindByName(s.cfg.DHCPInterface)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 67})
	if err != nil {
		return err
	}
	s.conn = conn
	log.Printf("[proxydhcp] 监听 udp/67")
	buf := make([]byte, 1500)
	for {
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go s.handle(pkt, raddr)
	}
}

// Stop 关闭监听。
func (s *Server) Stop() {
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *Server) handle(pkt []byte, raddr *net.UDPAddr) {
	m, err := parse(pkt)
	if err != nil || m.op != 1 {
		return
	}
	if !bytes.HasPrefix(m.options[optVendorClassID], []byte("PXEClient")) {
		return
	}
	mt := byte(0)
	if v := m.options[optMessageType]; len(v) == 1 {
		mt = v[0]
	}
	if mt != dhcpDiscover && mt != dhcpRequest {
		return
	}
	if s.ifc != nil {
		if !m.giaddr.Equal(net.IPv4zero) && !netif.Contains(s.ifc, m.giaddr) {
			return
		}
	}
	arch := uint16(archBIOS)
	if v := m.options[optClientArch]; len(v) == 2 {
		arch = binary.BigEndian.Uint16(v)
	}
	isIPXE := bytes.Contains(m.options[optUserClass], []byte("iPXE"))
	sip := s.serverIP(raddr)
	httpURL := "http://" + sip.String() + ":" + itoa(s.cfg.HTTPPort)
	bf := s.bootFile(arch, isIPXE, httpURL)
	rt := byte(dhcpOffer)
	if mt == dhcpRequest {
		rt = dhcpAck
	}
	reply := build(m, sip, bf, rt)
	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	if raddr.Port == 4011 {
		dst = &net.UDPAddr{IP: raddr.IP, Port: 4011}
	}
	s.conn.WriteToUDP(reply, dst)
	log.Printf("[proxydhcp] %s arch=0x%04x ipxe=%v -> %s", mac(m.chaddr), arch, isIPXE, bf)
}

func (s *Server) serverIP(raddr *net.UDPAddr) net.IP {
	if s.cfg.ServerIP != "" {
		if ip := net.ParseIP(s.cfg.ServerIP); ip != nil {
			return ip.To4()
		}
	}
	if s.ifc != nil {
		if ip := netif.PrimaryIPv4(s.ifc); ip != nil {
			return ip
		}
	}
	return netif.LocalIPFor(raddr.IP)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
