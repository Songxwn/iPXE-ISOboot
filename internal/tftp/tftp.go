// Package tftp 实现只读 TFTP 服务器 (RFC 1350 + blksize/tsize 选项)，
// 用于向 PXE 固件下发 iPXE 二进制。
package tftp

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	opRRQ   = 1
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6
)

// Server 提供 root 下的只读文件。
type Server struct {
	root string
	port int
}

func New(root string, port int) *Server {
	if port == 0 {
		port = 69
	}
	return &Server{root: root, port: port}
}

func (s *Server) Serve() error {
	os.MkdirAll(s.root, 0o755)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: s.port})
	if err != nil {
		return err
	}
	log.Printf("[tftp] 监听 udp/%d root=%s", s.port, s.root)
	buf := make([]byte, 1500)
	for {
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		req := make([]byte, n)
		copy(req, buf[:n])
		go s.handle(req, raddr)
	}
}

func (s *Server) handle(req []byte, raddr *net.UDPAddr) {
	if len(req) < 2 || binary.BigEndian.Uint16(req[0:2]) != opRRQ {
		return
	}
	sess, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return
	}
	defer sess.Close()

	fields := splitNull(req[2:])
	if len(fields) < 2 {
		return
	}
	filename := fields[0]
	opts := map[string]string{}
	for i := 2; i+1 < len(fields); i += 2 {
		opts[strings.ToLower(fields[i])] = fields[i+1]
	}
	path, err := s.safePath(filename)
	if err != nil {
		sendErr(sess, 2, "access denied")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		sendErr(sess, 1, "not found")
		log.Printf("[tftp] 404 %s", filename)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	log.Printf("[tftp] RRQ %s (%d)", filename, fi.Size())

	blksize := 512
	if v, ok := opts["blksize"]; ok {
		if n, e := strconv.Atoi(v); e == nil && n >= 8 && n <= 65464 {
			blksize = n
		}
	}
	if len(opts) > 0 {
		oack := []byte{0, opOACK}
		for k := range opts {
			switch k {
			case "blksize":
				oack = appendOpt(oack, "blksize", strconv.Itoa(blksize))
			case "tsize":
				oack = appendOpt(oack, "tsize", strconv.FormatInt(fi.Size(), 10))
			}
		}
		sess.Write(oack)
		if !waitACK(sess, 0) {
			return
		}
	}

	block := uint16(1)
	data := make([]byte, blksize)
	for {
		n, rerr := f.Read(data)
		pkt := make([]byte, 4+n)
		pkt[1] = opDATA
		binary.BigEndian.PutUint16(pkt[2:4], block)
		copy(pkt[4:], data[:n])
		if !sendWaitACK(sess, pkt, block) {
			return
		}
		if n < blksize {
			break
		}
		if rerr != nil {
			break
		}
		block++
	}
	log.Printf("[tftp] done %s", filename)
}

func (s *Server) safePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	full := filepath.Join(s.root, filepath.Clean("/"+name))
	rootAbs, _ := filepath.Abs(s.root)
	fullAbs, _ := filepath.Abs(full)
	if !strings.HasPrefix(fullAbs, rootAbs) {
		return "", fmt.Errorf("path escape")
	}
	return full, nil
}

func sendWaitACK(sess *net.UDPConn, pkt []byte, block uint16) bool {
	for r := 0; r < 5; r++ {
		if _, err := sess.Write(pkt); err != nil {
			return false
		}
		if waitACK(sess, block) {
			return true
		}
	}
	return false
}

func waitACK(sess *net.UDPConn, block uint16) bool {
	buf := make([]byte, 64)
	sess.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := sess.Read(buf)
	if err != nil || n < 4 || binary.BigEndian.Uint16(buf[0:2]) != opACK {
		return false
	}
	return binary.BigEndian.Uint16(buf[2:4]) == block
}

func sendErr(sess *net.UDPConn, code uint16, msg string) {
	pkt := []byte{0, opERROR, byte(code >> 8), byte(code)}
	pkt = append(pkt, []byte(msg)...)
	pkt = append(pkt, 0)
	sess.Write(pkt)
}

func splitNull(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == 0 {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	return out
}

func appendOpt(b []byte, k, v string) []byte {
	b = append(b, []byte(k)...)
	b = append(b, 0)
	b = append(b, []byte(v)...)
	b = append(b, 0)
	return b
}
