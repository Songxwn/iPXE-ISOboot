// Package menu 管理 ISO 启动菜单项的持久化。
package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry 是一个启动菜单项（对应一个 ISO）。
type Entry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	ISOName string `json:"iso_name"` // data/iso 下的文件名
	Enabled bool   `json:"enabled"`
	Order   int    `json:"order"`

	// 识别信息
	Distro string `json:"distro,omitempty"`
	Family string `json:"family,omitempty"` // 引导配方族：ubuntu/debian/rhel/arch/... 决定 grub loopback 命令

	// 可选：手动覆盖 GRUB 引导片段（留空则用配方库按 family 自动生成）
	CustomCfg string `json:"custom_cfg,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// Store 管理菜单项集合。
type Store struct {
	mu      sync.RWMutex
	path    string
	entries map[string]*Entry
}

// New 打开或创建菜单存储。
func New(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]*Entry{}}
	if b, err := os.ReadFile(path); err == nil {
		var list []*Entry
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, err
		}
		for _, e := range list {
			s.entries[e.ID] = e
		}
	}
	return s, nil
}

// List 返回按 Order 排序的所有项。
func (s *Store) List() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Get 返回指定项。
func (s *Store) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	return e, ok
}

// Put 新增或更新。
func (s *Store) Put(e *Entry) error {
	s.mu.Lock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	s.entries[e.ID] = e
	s.mu.Unlock()
	return s.save()
}

// Delete 删除。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
	return s.save()
}

func (s *Store) save() error {
	s.mu.RLock()
	list := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	s.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Order < list[j].Order })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
