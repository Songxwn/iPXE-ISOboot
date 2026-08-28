package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config 是整个应用的全局配置，会被持久化到 data/config.json。
type Config struct {
	// 网络
	ServerIP string `json:"server_ip"` // 本机对外提供服务的 IP，用于 TFTP/HTTP 地址下发；留空自动探测
	HTTPPort int    `json:"http_port"` // Web 控制台 & 文件服务端口
	TFTPPort int    `json:"tftp_port"` // TFTP 端口，默认 69

	// ProxyDHCP
	EnableProxyDHCP bool `json:"enable_proxy_dhcp"` // 是否启用内置 ProxyDHCP（与现有 DHCP 共存）

	// 认证
	AdminUser string `json:"admin_user"`
	AdminPass string `json:"admin_pass"` // 明文，首次启动生成，可在面板修改

	// 目录（相对 DataDir）
	DataDir string `json:"-"` // 运行时数据根目录

	// 引导默认项
	DefaultMenuTimeout int    `json:"default_menu_timeout"` // iPXE 菜单超时秒数
	DefaultEntryID     string `json:"default_entry_id"`     // 超时后默认启动项
}

var (
	mu      sync.RWMutex
	current *Config
	path    string
)

// Default 返回默认配置。
func Default(dataDir string) *Config {
	return &Config{
		ServerIP:           "",
		HTTPPort:           8080,
		TFTPPort:           69,
		EnableProxyDHCP:    true,
		AdminUser:          "admin",
		AdminPass:          "admin",
		DataDir:            dataDir,
		DefaultMenuTimeout: 10,
		DefaultEntryID:     "",
	}
}

// Load 从磁盘加载配置，不存在则创建默认。
func Load(dataDir string) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	path = filepath.Join(dataDir, "config.json")
	cfg := Default(dataDir)

	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, err
		}
		cfg.DataDir = dataDir
	} else {
		if err := saveLocked(cfg); err != nil {
			return nil, err
		}
	}
	current = cfg
	return cfg, nil
}

// Get 返回当前配置的副本指针（调用方勿并发写字段）。
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// 目录辅助
func (c *Config) ISODir() string    { return filepath.Join(c.DataDir, "iso") }
func (c *Config) TFTPRoot() string  { return filepath.Join(c.DataDir, "tftp") }
func (c *Config) ExtractDir() string { return filepath.Join(c.DataDir, "extract") }
func (c *Config) MenuFile() string  { return filepath.Join(c.DataDir, "menu.json") }
