package proxydhcp

import (
	"log"
	"sync"

	"ipxe-isoboot/internal/config"
)

// Manager 管理 ProxyDHCP 的运行时启停，供 Web 动态控制。
type Manager struct {
	cfg     *config.Config
	mu      sync.Mutex
	server  *Server
	running bool
}

func NewManager(cfg *config.Config) *Manager { return &Manager{cfg: cfg} }

// Running 返回运行状态。
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Start 启动（若未运行）。
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
			log.Printf("[proxydhcp] 停止/失败: %v", err)
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}
	}()
	log.Printf("[proxydhcp] 已启动")
}

// Stop 停止（若在运行）。
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

// Apply 按配置应用启停。
func (m *Manager) Apply() {
	if m.cfg.EnableProxyDHCP {
		m.Stop()
		m.Start()
	} else {
		m.Stop()
	}
}
