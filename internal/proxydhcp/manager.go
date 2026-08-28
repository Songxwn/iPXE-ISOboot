package proxydhcp

import (
	"log"
	"sync"

	"ipxe-isoboot/internal/config"
)

// Manager 管理 ProxyDHCP 服务的生命周期，支持运行时动态启停，
// 供 Web 控制台在保存配置后按需开启/关闭。
type Manager struct {
	cfg *config.Config

	mu      sync.Mutex
	server  *Server
	running bool
}

func NewManager(cfg *config.Config) *Manager { return &Manager{cfg: cfg} }

// Running 返回当前是否在运行。
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Start 启动 ProxyDHCP（若未运行）。
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	s := New(m.cfg)
	m.server = s
	m.running = true
	go func() {
		if err := s.Serve(); err != nil {
			log.Printf("[proxydhcp] 启动失败（端口 67 可能需要管理员权限或已被占用）: %v", err)
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}
	}()
	log.Printf("[proxydhcp] 已启动")
}

// Stop 停止 ProxyDHCP（若在运行）。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	if m.server != nil {
		m.server.Stop()
		m.server = nil
	}
	m.running = false
}

// Apply 根据配置的 EnableProxyDHCP 应用启停状态。
// 若相关配置（如网卡）变化，会先停后启以生效。
func (m *Manager) Apply() {
	if m.cfg.EnableProxyDHCP {
		// 重启以应用可能变化的网卡设置
		m.Stop()
		m.Start()
	} else {
		m.Stop()
	}
}
