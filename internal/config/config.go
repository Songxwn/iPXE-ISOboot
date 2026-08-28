// Package config 管理应用配置的加载与持久化。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config 是全局配置，持久化到 data/config.json。
type Config struct {
	ServerIP        string `json:"server_ip"`         // 对外服务 IP，留空自动探测
	HTTPPort        int    `json:"http_port"`         // Web/文件服务端口
	TFTPPort        int    `json:"tftp_port"`         // TFTP 端口
	EnableProxyDHCP bool   `json:"enable_proxy_dhcp"` // 是否启用内置 ProxyDHCP（默认关）
	DHCPInterface   string `json:"dhcp_interface"`    // 指定监听网卡（空=全部）

	AdminUser string `json:"admin_user"`
	AdminPass string `json:"admin_pass"`

	MenuTimeout int    `json:"menu_timeout"` // GRUB 菜单超时秒数
	MenuTitle   string `json:"menu_title"`   // 菜单标题

	DataDir string `json:"-"`
}

var (
	mu      sync.RWMutex
	current *Config
	cfgPath string
)

// Default 返回默认配置。
func Default(dataDir string) *Config {
	return &Config{
		HTTPPort:        8081,
		TFTPPort:        69,
		EnableProxyDHCP: false,
		AdminUser:       "admin",
		AdminPass:       "admin",
		MenuTimeout:     30,
		MenuTitle:       "iPXE-ISOboot (Ventoy-style network boot)",
		DataDir:         dataDir,
	}
}

// Load 从磁盘加载配置，不存在则创建默认。
func Load(dataDir string) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()
	cfgPath = filepath.Join(dataDir, "config.json")
	cfg := Default(dataDir)
	if b, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, err
		}
		cfg.DataDir = dataDir
	} else if err := saveLocked(cfg); err != nil {
		return nil, err
	}
	current = cfg
	return cfg, nil
}

// Get 返回当前配置。
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Save 持久化配置。
func Save(cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()
	current = cfg
	return saveLocked(cfg)
}

func saveLocked(cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

// 目录辅助
func (c *Config) ISODir() string   { return filepath.Join(c.DataDir, "iso") }
func (c *Config) TFTPRoot() string { return filepath.Join(c.DataDir, "tftp") }
func (c *Config) MenuFile() string { return filepath.Join(c.DataDir, "menu.json") }
