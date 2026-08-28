package web

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ipxe-isoboot/internal/bootiso"
	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/ipxe"
	"ipxe-isoboot/internal/iso"
	"ipxe-isoboot/internal/menu"
	"ipxe-isoboot/internal/netif"
)

// baseURL 依据配置或请求推断对外 HTTP 基地址。
func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.ServerIP != "" {
		return fmt.Sprintf("http://%s:%d", s.cfg.ServerIP, s.cfg.HTTPPort)
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return fmt.Sprintf("http://%s:%d", host, s.cfg.HTTPPort)
}

// GET /boot.ipxe —— 动态生成 iPXE 菜单脚本
func (s *Server) handleBootScript(w http.ResponseWriter, r *http.Request) {
	ctx := ipxe.BootContext{
		BaseURL: s.baseURL(r),
		Timeout: s.cfg.DefaultMenuTimeout,
		Default: s.cfg.DefaultEntryID,
	}
	script := ipxe.Menu(s.menu.List(), ctx)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(script))
}

// GET /api/preview —— 预览脚本（控制台用）
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	ctx := ipxe.BootContext{
		BaseURL: s.baseURL(r),
		Timeout: s.cfg.DefaultMenuTimeout,
		Default: s.cfg.DefaultEntryID,
	}
	writeJSON(w, map[string]string{"script": ipxe.Menu(s.menu.List(), ctx)})
}

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"server_ip":         s.cfg.ServerIP,
		"http_port":         s.cfg.HTTPPort,
		"tftp_port":         s.cfg.TFTPPort,
		"enable_proxy_dhcp": s.cfg.EnableProxyDHCP,
		"base_url":          s.baseURL(r),
		"boot_script_url":   s.baseURL(r) + "/boot.ipxe",
	})
}

// GET /api/interfaces —— 列出本机网卡供选择 DHCP 监听网卡
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
		// 仅允许修改这些字段
		s.cfg.ServerIP = strings.TrimSpace(in.ServerIP)
		s.cfg.EnableProxyDHCP = in.EnableProxyDHCP
		s.cfg.DHCPInterface = strings.TrimSpace(in.DHCPInterface)
		s.cfg.DefaultMenuTimeout = in.DefaultMenuTimeout
		s.cfg.DefaultEntryID = in.DefaultEntryID
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
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	http.Error(w, "method", 405)
}

// GET /api/menu  |  POST /api/menu
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

// ISOFile 描述一个已上传的 ISO。
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
		out = append(out, ISOFile{Name: e.Name(), Size: fi.Size()})
	}
	writeJSON(w, out)
}

// POST /api/upload  (multipart, 流式写入，支持大文件)
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
		if _, err := copyBuf(f, part); err != nil {
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

// POST /api/analyze {"name":"xxx.iso"}
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	path := filepath.Join(s.cfg.ISODir(), sanitizeName(req.Name))
	info, err := iso.Analyze(path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, info)
}

// POST /api/extract {"name":"x.iso","files":["/casper/vmlinuz","/casper/initrd"],"dest":"ubuntu"}
// 将 ISO 内文件提取到 extract/<dest>/ 供 HTTP 直接下载。
func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string   `json:"name"`
		Files []string `json:"files"`
		Dest  string   `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	path := filepath.Join(s.cfg.ISODir(), sanitizeName(req.Name))
	rd, err := iso.Open(path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rd.Close()

	dest := sanitizeName(req.Dest)
	if dest == "" {
		dest = strings.TrimSuffix(sanitizeName(req.Name), ".iso")
	}
	base := filepath.Join(s.cfg.ExtractDir(), dest)
	os.MkdirAll(base, 0o755)

	var extracted []string
	for _, fp := range req.Files {
		e, err := rd.Find(fp)
		if err != nil {
			continue
		}
		out := filepath.Join(base, filepath.FromSlash(strings.TrimPrefix(fp, "/")))
		os.MkdirAll(filepath.Dir(out), 0o755)
		of, err := os.Create(out)
		if err != nil {
			continue
		}
		rd.Extract(e, of)
		of.Close()
		rel := "/files/extract/" + dest + "/" + strings.TrimPrefix(strings.ReplaceAll(fp, "\\", "/"), "/")
		extracted = append(extracted, rel)
	}
	writeJSON(w, map[string]any{"ok": true, "paths": extracted})
}

// POST /api/delete-iso {"name":"x.iso"}
func (s *Server) handleDeleteISO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	path := filepath.Join(s.cfg.ISODir(), sanitizeName(req.Name))
	if err := os.Remove(path); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// POST /api/gen-boot-iso —— 生成自定义 iPXE 引导 ISO 并直接下载。
func (s *Server) handleGenBootISO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", 405)
		return
	}
	var req struct {
		ChainURL string `json:"chain_url"`
		IPMode   string `json:"ip_mode"`
		NetIf    string `json:"net_if"`
		IP       string `json:"ip"`
		Netmask  string `json:"netmask"`
		Gateway  string `json:"gateway"`
		DNS      string `json:"dns"`
		VLANID   int    `json:"vlan_id"`
		Timeout  int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if req.ChainURL == "" {
		req.ChainURL = s.baseURL(r) + "/boot.ipxe"
	}
	params := ipxe.BootISOParams{
		ChainURL: req.ChainURL,
		IPMode:   req.IPMode,
		NetIf:    req.NetIf,
		IP:       req.IP,
		Netmask:  req.Netmask,
		Gateway:  req.Gateway,
		DNS:      req.DNS,
		VLANID:   req.VLANID,
		Timeout:  req.Timeout,
	}
	data, err := bootiso.Generate(s.cfg.TFTPRoot(), params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="ipxe-boot.iso"`)
	w.Header().Set("Content-Length", itoaLen(len(data)))
	w.Write(data)
}

// POST /api/preview-autoexec —— 预览将嵌入引导 ISO 的 autoexec.ipxe。
func (s *Server) handlePreviewAutoexec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChainURL string `json:"chain_url"`
		IPMode   string `json:"ip_mode"`
		NetIf    string `json:"net_if"`
		IP       string `json:"ip"`
		Netmask  string `json:"netmask"`
		Gateway  string `json:"gateway"`
		DNS      string `json:"dns"`
		VLANID   int    `json:"vlan_id"`
		Timeout  int    `json:"timeout"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ChainURL == "" {
		req.ChainURL = s.baseURL(r) + "/boot.ipxe"
	}
	script := ipxe.AutoExec(ipxe.BootISOParams{
		ChainURL: req.ChainURL, IPMode: req.IPMode, NetIf: req.NetIf,
		IP: req.IP, Netmask: req.Netmask, Gateway: req.Gateway,
		DNS: req.DNS, VLANID: req.VLANID, Timeout: req.Timeout,
	})
	writeJSON(w, map[string]string{"script": script})
}

func itoaLen(n int) string { return fmt.Sprintf("%d", n) }
