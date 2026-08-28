// Package web 提供 Web 控制台、REST API、文件服务、boot.ipxe 与 grub.cfg 端点。
package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/menu"
	"ipxe-isoboot/internal/proxydhcp"
)

//go:embed static/*
var staticFS embed.FS

// Version 由 main 注入。
var Version = "dev"

// Server 聚合 HTTP 依赖。
type Server struct {
	cfg     *config.Config
	menu    *menu.Store
	dhcpMgr *proxydhcp.Manager
	mux     *http.ServeMux
}

// New 创建 Web 服务。
func New(cfg *config.Config, m *menu.Store, dhcp *proxydhcp.Manager) *Server {
	s := &Server{cfg: cfg, menu: m, dhcpMgr: dhcp, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	// 网络引导端点（免认证）
	s.mux.HandleFunc("/boot.ipxe", s.handleBootIPXE)
	s.mux.HandleFunc("/grub/grub.cfg", s.handleGrubCfg)

	// 文件服务：/files/ -> DataDir；/grub/ -> tftp/grub（GRUB 二进制）
	s.mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(s.cfg.DataDir))))
	s.mux.Handle("/grub/", http.StripPrefix("/grub/", http.FileServer(http.Dir(s.cfg.TFTPRoot()+"/grub"))))

	// API
	s.mux.HandleFunc("/api/version", s.handleVersion)
	s.mux.HandleFunc("/api/login", s.handleLogin)
	s.mux.HandleFunc("/api/status", s.auth(s.handleStatus))
	s.mux.HandleFunc("/api/interfaces", s.auth(s.handleInterfaces))
	s.mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	s.mux.HandleFunc("/api/menu", s.auth(s.handleMenu))
	s.mux.HandleFunc("/api/menu/", s.auth(s.handleMenuItem))
	s.mux.HandleFunc("/api/isos", s.auth(s.handleISOs))
	s.mux.HandleFunc("/api/upload", s.auth(s.handleUpload))
	s.mux.HandleFunc("/api/quick-add", s.auth(s.handleQuickAdd))
	s.mux.HandleFunc("/api/delete-iso", s.auth(s.handleDeleteISO))
	s.mux.HandleFunc("/api/grub-preview", s.auth(s.handleGrubPreview))
}

// Handler 返回带日志的 http.Handler。
func (s *Server) Handler() http.Handler {
	return logMW(s.mux)
}

func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[http] %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
