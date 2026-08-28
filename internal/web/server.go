// Package web 提供 Web 控制台、REST API、文件服务与 iPXE 脚本端点。
package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/menu"
)

//go:embed static/*
var staticFS embed.FS

// Server 聚合 HTTP 相关依赖。
type Server struct {
	cfg   *config.Config
	menu  *menu.Store
	mux   *http.ServeMux
}

// New 创建 Web 服务。
func New(cfg *config.Config, m *menu.Store) *Server {
	s := &Server{cfg: cfg, menu: m, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	// 静态资源（控制台）
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	// iPXE 引导脚本（无需认证，供网络启动客户端访问）
	s.mux.HandleFunc("/boot.ipxe", s.handleBootScript)

	// 文件服务：/files/ -> DataDir（供 iPXE 下载内核/initrd/wim 等）
	fileRoot := http.Dir(s.cfg.DataDir)
	s.mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(fileRoot)))

	// REST API（需认证）
	s.mux.HandleFunc("/api/login", s.handleLogin)
	s.mux.HandleFunc("/api/status", s.auth(s.handleStatus))
	s.mux.HandleFunc("/api/interfaces", s.auth(s.handleInterfaces))
	s.mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	s.mux.HandleFunc("/api/menu", s.auth(s.handleMenu))         // GET 列表 / POST 新增或更新
	s.mux.HandleFunc("/api/menu/", s.auth(s.handleMenuItem))    // DELETE /api/menu/{id}
	s.mux.HandleFunc("/api/isos", s.auth(s.handleISOs))         // GET 列表
	s.mux.HandleFunc("/api/upload", s.auth(s.handleUpload))     // POST 上传 ISO
	s.mux.HandleFunc("/api/analyze", s.auth(s.handleAnalyze))   // POST 分析 ISO
	s.mux.HandleFunc("/api/extract", s.auth(s.handleExtract))   // POST 提取 ISO 内文件
	s.mux.HandleFunc("/api/delete-iso", s.auth(s.handleDeleteISO))
	s.mux.HandleFunc("/api/preview", s.auth(s.handlePreview))   // GET 预览生成的 iPXE 脚本
	s.mux.HandleFunc("/api/gen-boot-iso", s.auth(s.handleGenBootISO))       // POST 生成引导 ISO
	s.mux.HandleFunc("/api/preview-autoexec", s.auth(s.handlePreviewAutoexec)) // POST 预览 autoexec
}

// Handler 返回顶层 http.Handler（带日志）。
func (s *Server) Handler() http.Handler {
	return logMiddleware(s.mux)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[http] %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
