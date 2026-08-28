// iPXE-ISOboot：一站式 PXE 网络装机服务。
//
// 集成 ProxyDHCP + TFTP + HTTP(Web 控制台)，支持 Linux 各发行版、
// Windows、VMware ESXi 的 ISO 网络启动装机；支持 UEFI 与传统 BIOS。
//
// 使用：./ipxe-isoboot -data ./data -http 8081
// 首次运行会创建 data 目录并尝试下载 iPXE 引导文件到 data/tftp。
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ipxe-isoboot/internal/bootfiles"
	"ipxe-isoboot/internal/config"
	"ipxe-isoboot/internal/menu"
	"ipxe-isoboot/internal/proxydhcp"
	"ipxe-isoboot/internal/tftp"
	"ipxe-isoboot/internal/web"
)

var version = "dev" // 由 ldflags 注入

func main() {
	var (
		dataDir  = flag.String("data", "data", "数据目录")
		httpPort = flag.Int("http", 0, "HTTP 端口（覆盖配置）")
		noDHCP   = flag.Bool("no-dhcp", false, "禁用内置 ProxyDHCP")
		noFetch  = flag.Bool("no-fetch", false, "启动时不尝试下载 iPXE 引导文件")
		showVer  = flag.Bool("version", false, "显示版本")
	)
	flag.Parse()

	if *showVer {
		log.Printf("iPXE-ISOboot %s", version)
		return
	}

	log.Printf("iPXE-ISOboot %s 启动中...", version)

	abs, _ := filepath.Abs(*dataDir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		log.Fatalf("无法创建数据目录: %v", err)
	}

	cfg, err := config.Load(abs)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if *httpPort != 0 {
		cfg.HTTPPort = *httpPort
	}
	if *noDHCP {
		cfg.EnableProxyDHCP = false
	}

	// 创建必要目录
	for _, d := range []string{cfg.ISODir(), cfg.TFTPRoot(), cfg.ExtractDir()} {
		os.MkdirAll(d, 0o755)
	}

	// 尝试准备 iPXE 引导文件（不阻塞主流程）
	if !*noFetch {
		go func() {
			if err := bootfiles.Ensure(cfg.TFTPRoot()); err != nil {
				log.Printf("[bootfiles] 准备引导文件出错（可手动放置到 %s）: %v", cfg.TFTPRoot(), err)
			}
		}()
	}

	// 菜单存储
	mstore, err := menu.New(cfg.MenuFile())
	if err != nil {
		log.Fatalf("加载菜单失败: %v", err)
	}

	// TFTP
	go func() {
		ts := tftp.New(cfg.TFTPRoot(), cfg.TFTPPort)
		if err := ts.Serve(); err != nil {
			log.Printf("[tftp] 启动失败（端口 69 可能需要管理员权限或已被占用）: %v", err)
		}
	}()

	// ProxyDHCP（默认关闭，按配置决定是否启动；可在 Web 控制台动态启停）
	dhcpMgr := proxydhcp.NewManager(cfg)
	if cfg.EnableProxyDHCP {
		dhcpMgr.Start()
	} else {
		log.Printf("[proxydhcp] 未启用（可在 Web 控制台开启）")
	}

	// HTTP / Web 控制台
	ws := web.New(cfg, mstore, dhcpMgr)
	srv := &http.Server{
		Addr:    ":" + itoa(cfg.HTTPPort),
		Handler: ws.Handler(),
	}
	go func() {
		log.Printf("[http] Web 控制台: http://localhost:%d  (默认账号 %s/%s)", cfg.HTTPPort, cfg.AdminUser, cfg.AdminPass)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[http] 启动失败: %v", err)
		}
	}()

	// 优雅退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("正在关闭...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
