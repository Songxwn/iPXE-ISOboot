package proxydhcp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

type message struct {
	op      byte
	htype   byte
	hlen    byte
	xid     uint32
	flags   uint16
	giaddr  net.IP
	chaddr  []byte
	options map[byte][]byte
}

func parse(b []byte) (*message, error) {
	if len(b) < 240 {
		return nil, errors.New("too short")
	}
	m := &message{
		op:      b[0],
		htype:   b[1],
		hlen:    b[2],
		xid:     binary.BigEndian.Uint32(b[4:8]),
		flags:   binary.BigEndian.Uint16(b[10:12]),
		giaddr:  net.IP(b[24:28]),
		chaddr:  b[28:44],
		options: map[byte][]byte{},
	}
	if binary.BigEndian.Uint32(b[236:240]) != magicCookie {
		return nil, errors.New("bad cookie")
	}
	i := 240
	for i < len(b) {
		c := b[i]
		if c == 0 {
			i++
			continue
		}
		if c == optEnd || i+1 >= len(b) {
			break
		}
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		m.options[c] = b[i+2 : i+2+l]
		i += 2 + l
	}
	return m, nil
}

func build(req *message, serverIP net.IP, bootfile string, msgType byte) []byte {
	buf := make([]byte, 300)
	buf[0] = 2
	buf[1] = req.htype
	buf[2] = req.hlen
	binary.BigEndian.PutUint32(buf[4:8], req.xid)
	binary.BigEndian.PutUint16(buf[10:12], req.flags)
	copy(buf[20:24], serverIP.To4())
	copy(buf[24:28], req.giaddr.To4())
	copy(buf[28:44], req.chaddr)
	copy(buf[108:108+len(bootfile)], bootfile)
	binary.BigEndian.PutUint32(buf[236:240], magicCookie)

	var o []byte
	o = append(o, optMessageType, 1, msgType)
	o = append(o, optServerID, 4)
	o = append(o, serverIP.To4()...)
	pxe := []byte("PXEClient")
	o = append(o, optVendorClassID, byte(len(pxe)))
	o = append(o, pxe...)
	sip := []byte(serverIP.String())
	o = append(o, optTFTPServer, byte(len(sip)))
	o = append(o, sip...)
	bf := []byte(bootfile)
	o = append(o, optBootFile, byte(len(bf)))
	o = append(o, bf...)
	o = append(o, optEnd)
	buf = append(buf, o...)
	for len(buf) < 300 {
		buf = append(buf, 0)
	}
	return buf
}

func mac(c []byte) string {
	if len(c) < 6 {
		return "??"
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", c[0], c[1], c[2], c[3], c[4], c[5])
}
