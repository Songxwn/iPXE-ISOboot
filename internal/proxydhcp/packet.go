package proxydhcp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// message 是解析后的 DHCP 报文（仅保留我们需要的字段）。
type message struct {
	op      byte
	htype   byte
	hlen    byte
	xid     uint32
	flags   uint16
	ciaddr  net.IP
	yiaddr  net.IP
	siaddr  net.IP
	giaddr  net.IP
	chaddr  []byte // 16 字节
	options map[byte][]byte
}

func parse(b []byte) (*message, error) {
	if len(b) < 240 {
		return nil, errors.New("报文过短")
	}
	m := &message{
		op:      b[0],
		htype:   b[1],
		hlen:    b[2],
		xid:     binary.BigEndian.Uint32(b[4:8]),
		flags:   binary.BigEndian.Uint16(b[10:12]),
		ciaddr:  net.IP(b[12:16]),
		yiaddr:  net.IP(b[16:20]),
		siaddr:  net.IP(b[20:24]),
		giaddr:  net.IP(b[24:28]),
		chaddr:  b[28:44],
		options: map[byte][]byte{},
	}
	// magic cookie
	if binary.BigEndian.Uint32(b[236:240]) != magicCookie {
		return nil, errors.New("magic cookie 不匹配")
	}
	// 解析选项
	i := 240
	for i < len(b) {
		code := b[i]
		if code == optPad {
			i++
			continue
		}
		if code == optEnd {
			break
		}
		if i+1 >= len(b) {
			break
		}
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		m.options[code] = b[i+2 : i+2+l]
		i += 2 + l
	}
	return m, nil
}

// buildReply 构造 ProxyDHCP OFFER/ACK 报文。
func buildReply(req *message, serverIP net.IP, bootfile string, msgType byte) []byte {
	buf := make([]byte, 300)
	buf[0] = opBootReply
	buf[1] = req.htype
	buf[2] = req.hlen
	buf[3] = 0
	binary.BigEndian.PutUint32(buf[4:8], req.xid)
	binary.BigEndian.PutUint16(buf[10:12], req.flags)
	// ciaddr = 0, yiaddr = 0 (ProxyDHCP 不分配地址)
	copy(buf[20:24], serverIP.To4()) // siaddr = 引导服务器
	copy(buf[24:28], req.giaddr.To4())
	copy(buf[28:44], req.chaddr)

	// sname (server host name, 64) 与 file (128) 字段。
	// 大多数 PXE 固件从 file 字段读取引导文件名。
	copy(buf[108:108+len(bootfile)], []byte(bootfile))

	// magic cookie
	binary.BigEndian.PutUint32(buf[236:240], magicCookie)

	opts := []byte{}
	// 53 消息类型
	opts = append(opts, optMessageType, 1, msgType)
	// 54 服务器标识
	opts = append(opts, optServerID, 4)
	opts = append(opts, serverIP.To4()...)
	// 60 供应商类别，必须回 PXEClient 让固件接受
	pxe := []byte("PXEClient")
	opts = append(opts, optVendorClassID, byte(len(pxe)))
	opts = append(opts, pxe...)
	// 66 TFTP 服务器名
	sip := []byte(serverIP.String())
	opts = append(opts, optTFTPServerName, byte(len(sip)))
	opts = append(opts, sip...)
	// 67 引导文件名
	bf := []byte(bootfile)
	opts = append(opts, optBootFileName, byte(len(bf)))
	opts = append(opts, bf...)
	opts = append(opts, optEnd)

	buf = append(buf, opts...)
	// 至少填充到 300 字节
	for len(buf) < 300 {
		buf = append(buf, 0)
	}
	return buf
}

func macStr(chaddr []byte) string {
	if len(chaddr) < 6 {
		return "??"
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		chaddr[0], chaddr[1], chaddr[2], chaddr[3], chaddr[4], chaddr[5])
}

// localIPFor 返回与给定对端处于同一子网/可路由的本机 IPv4 地址。
func localIPFor(peer net.IP) net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return net.IPv4(127, 0, 0, 1)
	}
	var fallback net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if fallback == nil {
				fallback = ip4
			}
			if peer != nil && ipnet.Contains(peer) {
				return ip4
			}
		}
	}
	if fallback != nil {
		return fallback
	}
	return net.IPv4(127, 0, 0, 1)
}
