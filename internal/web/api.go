package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/grubcfg"
	"ipxe-isoboot/internal/ipxescript"
	"ipxe-isoboot/internal/iso"
	"ipxe-isoboot/internal/menu"
	"ipxe-isoboot/internal/netif"
)

func (s *Server) httpBase(r *http.Request) string {
	if s.cfg.ServerIP != "" {
		return fmt.Sprintf("http://%s:%d", s.cfg.ServerIP, s.cfg.HTTPPort)
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return fmt.Sprintf("http://%s:%d", host, s.cfg.HTTPPort)
}

// GET /boot.ipxe
func (s *Server) handleBootIPXE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(ipxescript.Boot(s.httpBase(r))))
}

// GET /grub/grub.cfg
func (s *Server) handleGrubCfg(w http.ResponseWriter, r *http.Request) {
	ctx := grubcfg.Context{HTTPBase: s.httpBase(r), Timeout: s.cfg.MenuTimeout, Title: s.cfg.MenuTitle}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(grubcfg.Generate(s.menu.List(), ctx)))
}

// GET /api/version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": Version})
}

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	running := false
	if s.dhcpMgr != nil {
		running = s.dhcpMgr.Running()
	}
	writeJSON(w, map[string]any{
		"server_ip":         s.cfg.ServerIP,
		"http_port":         s.cfg.HTTPPort,
		"tftp_port":         s.cfg.TFTPPort,
		"enable_proxy_dhcp": s.cfg.EnableProxyDHCP,
		"dhcp_running":      running,
		"base_url":          s.httpBase(r),
		"boot_script_url":   s.httpBase(r) + "/boot.ipxe",
	})
}

// GET /api/interfaces
func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, netif.List())
}

// GET/POST /api/config
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, s.cfg)
		return
	}
	if r.Method == http.MethodPost {
		var in config.Config
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		s.cfg.ServerIP = strings.TrimSpace(in.ServerIP)
		s.cfg.EnableProxyDHCP = in.EnableProxyDHCP
		s.cfg.DHCPInterface = strings.TrimSpace(in.DHCPInterface)
		s.cfg.MenuTimeout = in.MenuTimeout
		if in.MenuTitle != "" {
			s.cfg.MenuTitle = in.MenuTitle
		}
		if in.AdminUser != "" {
			s.cfg.AdminUser = in.AdminUser
		}
		if in.AdminPass != "" {
			s.cfg.AdminPass = in.AdminPass
		}
		if err := config.Save(s.cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if s.dhcpMgr != nil {
			s.dhcpMgr.Apply()
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	http.Error(w, "method", 405)
}

// GET/POST /api/menu
func (s *Server) handleMenu(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.menu.List())
	case http.MethodPost:
		var e menu.Entry
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		if e.ID == "" {
			e.ID = slug(e.Title)
		}
		if e.ID == "" {
			http.Error(w, "标题不能为空", 400)
			return
		}
		if err := s.menu.Put(&e); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, e)
	default:
		http.Error(w, "method", 405)
	}
}

// DELETE /api/menu/{id}
func (s *Server) handleMenuItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/menu/")
	if id == "" {
		http.Error(w, "缺少 id", 400)
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.menu.Delete(id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if e, ok := s.menu.Get(id); ok {
		writeJSON(w, e)
		return
	}
	http.Error(w, "not found", 404)
}

// ISOFile 描述 ISO 文件。
type ISOFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// GET /api/isos
func (s *Server) handleISOs(w http.ResponseWriter, r *http.Request) {
	dir := s.cfg.ISODir()
	os.MkdirAll(dir, 0o755)
	ents, _ := os.ReadDir(dir)
	var out []ISOFile
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		fi, _ := e.Info()
		out = append(out, ISOFile{e.Name(), fi.Size()})
	}
	writeJSON(w, out)
}

// POST /api/upload
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", 405)
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	dir := s.cfg.ISODir()
	os.MkdirAll(dir, 0o755)
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FormName() != "file" {
			continue
		}
		name := sanitizeName(part.FileName())
		if name == "" {
			continue
		}
		dst := filepath.Join(dir, name)
		f, err := os.Create(dst)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		buf := make([]byte, 4<<20)
		if _, err := io.CopyBuffer(f, part, buf); err != nil {
			f.Close()
			http.Error(w, err.Error(), 500)
			return
		}
		f.Close()
		writeJSON(w, map[string]any{"ok": true, "name": name})
		return
	}
	http.Error(w, "未收到文件", 400)
}

// POST /api/quick-add {"name":"x.iso"}
func (s *Server) handleQuickAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	name := sanitizeName(req.Name)
	info, err := iso.Analyze(filepath.Join(s.cfg.ISODir(), name))
	if err != nil {
		http.Error(w, "分析失败: "+err.Error(), 500)
		return
	}
	title := strings.TrimSuffix(name, filepath.Ext(name))
	if info.Display != "" {
		title = title + " (" + info.Display + ")"
	}
	e := &menu.Entry{
		ID:      slug(strings.TrimSuffix(name, filepath.Ext(name))),
		Title:   title,
		ISOName: name,
		Enabled: true,
		Order:   len(s.menu.List()),
		Distro:  info.Distro,
		Family:  info.Family,
	}
	if e.ID == "" {
		e.ID = fmt.Sprintf("iso%d", len(s.menu.List())+1)
	}
	if err := s.menu.Put(e); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "entry": e, "family": info.Family, "note": info.Note})
}

// POST /api/delete-iso {"name":"x.iso"}
func (s *Server) handleDeleteISO(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	json.NewDecoder(r.Body).Decode(&req)
	if err := os.Remove(filepath.Join(s.cfg.ISODir(), sanitizeName(req.Name))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// GET /api/grub-preview
func (s *Server) handleGrubPreview(w http.ResponseWriter, r *http.Request) {
	ctx := grubcfg.Context{HTTPBase: s.httpBase(r), Timeout: s.cfg.MenuTimeout, Title: s.cfg.MenuTitle}
	writeJSON(w, map[string]string{"cfg": grubcfg.Generate(s.menu.List(), ctx)})
}

// --- helpers ---

func slug(str string) string {
	str = strings.ToLower(strings.TrimSpace(str))
	var b strings.Builder
	for _, r := range str {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-' || r == '.':
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == ".." {
		return ""
	}
	return name
}
