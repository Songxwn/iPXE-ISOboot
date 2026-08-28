package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// OSType 表示启动项的操作系统类别，决定 iPXE 脚本的生成方式。
type OSType string

const (
	TypeLinux    OSType = "linux"    // 通用 Linux：kernel + initrd + append
	TypeWindows  OSType = "windows"  // Windows：wimboot + bcd/boot.sdi/wim
	TypeESXi     OSType = "esxi"     // VMware ESXi：mboot.c32/mboot.efi + boot.cfg
	TypeSanBoot  OSType = "sanboot"  // 直接 sanboot ISO（memdisk/iso 直挂）
	TypeMemdisk  OSType = "memdisk"  // 整个 ISO 下载到内存当虚拟光驱（仅 BIOS）
	TypeCustom   OSType = "custom"   // 用户自定义 iPXE 脚本片段
)

// Entry 是一个启动菜单项。
type Entry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     OSType `json:"type"`
	Enabled  bool   `json:"enabled"`
	Order    int    `json:"order"`

	// 关联的 ISO（用于展示与自动生成路径），可为空
	ISOName string `json:"iso_name,omitempty"`

	// Linux
	Kernel string `json:"kernel,omitempty"` // 相对 HTTP 根的路径，如 /files/extract/ubuntu/casper/vmlinuz
	Initrd string `json:"initrd,omitempty"`
	Append string `json:"append,omitempty"` // 内核参数
	ToRAM  bool   `json:"toram,omitempty"`  // 加载到内存运行（live 系统驻留 RAM）
	Distro string `json:"distro,omitempty"` // 发行版标识，用于生成 toram 参数

	// Windows
	Wimboot   string `json:"wimboot,omitempty"`   // wimboot 路径（一般固定 /files/tftp/wimboot）
	Bootmgr   string `json:"bootmgr,omitempty"`   // bootmgr（ISO 根 /bootmgr）
	BCD       string `json:"bcd,omitempty"`       // /boot/bcd
	BootSDI   string `json:"boot_sdi,omitempty"`  // /boot/boot.sdi
	BootWIM   string `json:"boot_wim,omitempty"`  // /sources/boot.wim
	WinExtras string `json:"win_extras,omitempty"` // 额外 wimboot 行（可选）

	// ESXi
	MbootC32 string `json:"mboot_c32,omitempty"`
	MbootEFI string `json:"mboot_efi,omitempty"`
	BootCFG  string `json:"boot_cfg,omitempty"` // 修改后的 boot.cfg 的 HTTP 路径

	// SanBoot / Custom
	SanURL string `json:"san_url,omitempty"`
	Script string `json:"script,omitempty"` // Custom 类型的原始 iPXE 脚本

	CreatedAt time.Time `json:"created_at"`
}

// Store 管理所有菜单项的持久化。
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

// Put 新增或更新一项并持久化。
func (s *Store) Put(e *Entry) error {
	s.mu.Lock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	s.entries[e.ID] = e
	s.mu.Unlock()
	return s.save()
}

// Delete 删除一项并持久化。
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
