// Package tftp 实现一个只读 TFTP 服务器 (RFC 1350)，
// 支持 blksize/tsize/windowsize 选项协商 (RFC 2347/2348/2349/7440)，
// 用于向 PXE 固件下发 iPXE 引导二进制。
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
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6
)

// Server 提供指定根目录下的只读文件。
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
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: s.port}
	conn, err := net.ListenUDP("udp4", addr)
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
	if len(req) < 2 {
		return
	}
	op := binary.BigEndian.Uint16(req[0:2])
	// 每个传输使用独立随机端口
	sess, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return
	}
	defer sess.Close()

	if op != opRRQ {
		sendError(sess, 4, "仅支持读取")
		return
	}

	fields := splitNull(req[2:])
	if len(fields) < 2 {
		sendError(sess, 4, "非法请求")
		return
	}
	filename := fields[0]
	// 解析选项
	opts := map[string]string{}
	for i := 2; i+1 < len(fields); i += 2 {
		opts[strings.ToLower(fields[i])] = fields[i+1]
	}

	path, err := s.safePath(filename)
	if err != nil {
		sendError(sess, 2, "拒绝访问")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		sendError(sess, 1, "文件不存在: "+filename)
		log.Printf("[tftp] 404 %s", filename)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	log.Printf("[tftp] RRQ %s (%d bytes)", filename, fi.Size())

	blksize := 512
	if v, ok := opts["blksize"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 8 && n <= 65464 {
			blksize = n
		}
	}

	// 若客户端请求了选项，先回 OACK
	if len(opts) > 0 {
		oack := []byte{0, opOACK}
		for k, v := range opts {
			switch k {
			case "blksize":
				oack = appendOpt(oack, "blksize", strconv.Itoa(blksize))
			case "tsize":
				oack = appendOpt(oack, "tsize", strconv.FormatInt(fi.Size(), 10))
			case "windowsize":
				oack = appendOpt(oack, "windowsize", v)
			case "timeout":
				oack = appendOpt(oack, "timeout", v)
			}
		}
		if _, err := sess.Write(oack); err != nil {
			return
		}
		// 等待对 OACK 的 ACK (block 0)
		if !waitACK(sess, 0) {
			return
		}
	}

	// 发送数据块
	block := uint16(1)
	data := make([]byte, blksize)
	for {
		n, rerr := f.Read(data)
		pkt := make([]byte, 4+n)
		pkt[0] = 0
		pkt[1] = opDATA
		binary.BigEndian.PutUint16(pkt[2:4], block)
		copy(pkt[4:], data[:n])

		if !sendAndWaitACK(sess, pkt, block) {
			return
		}
		if n < blksize {
			break // 最后一块
		}
		if rerr != nil {
			break
		}
		block++
	}
	log.Printf("[tftp] 完成 %s", filename)
}

func (s *Server) safePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean("/" + name)
	full := filepath.Join(s.root, clean)
	rootAbs, _ := filepath.Abs(s.root)
	fullAbs, _ := filepath.Abs(full)
	if !strings.HasPrefix(fullAbs, rootAbs) {
		return "", fmt.Errorf("路径越界")
	}
	return full, nil
}

func sendAndWaitACK(sess *net.UDPConn, pkt []byte, block uint16) bool {
	for retry := 0; retry < 5; retry++ {
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
	if err != nil || n < 4 {
		return false
	}
	if binary.BigEndian.Uint16(buf[0:2]) != opACK {
		return false
	}
	return binary.BigEndian.Uint16(buf[2:4]) == block
}

func sendError(sess *net.UDPConn, code uint16, msg string) {
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
