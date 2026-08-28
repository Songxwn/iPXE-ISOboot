// iPXE-ISOboot：Ventoy 风格的网络 ISO 启动服务。
//
// iPXE(ProxyDHCP/TFTP) 引导客户端 → 链式加载 GRUB2 → GRUB2 用 loopback
// 挂载 HTTP 上的 ISO 直接启动（Ventoy 同款机制）。支持 UEFI 与 BIOS。
package main

import (
	"context"
	"flag"
	"fmt"
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
	"ipxe-isoboot/internal/netif"
	"ipxe-isoboot/internal/proxydhcp"
	"ipxe-isoboot/internal/tftp"
	"ipxe-isoboot/internal/web"
)

var version = "dev"

func main() {
	dataDir := flag.String("data", "data", "数据目录")
	httpPort := flag.Int("http", 0, "HTTP 端口（覆盖配置）")
	noFetch := flag.Bool("no-fetch", false, "不自动下载/生成引导文件")
	showVer := flag.Bool("version", false, "显示版本")
	flag.Parse()

	if *showVer {
		fmt.Println("iPXE-ISOboot", version)
		return
	}
	log.Printf("iPXE-ISOboot %s 启动中...", version)

	abs, _ := filepath.Abs(*dataDir)
	os.MkdirAll(abs, 0o755)
	cfg, err := config.Load(abs)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	web.Version = version
	if *httpPort != 0 {
		cfg.HTTPPort = *httpPort
	}
	for _, d := range []string{cfg.ISODir(), cfg.TFTPRoot(), filepath.Join(cfg.TFTPRoot(), "grub")} {
		os.MkdirAll(d, 0o755)
	}

	// 准备引导文件（iPXE 下载 + GRUB2 网络镜像生成）
	if !*noFetch {
		go func() {
			bootfiles.EnsureIPXE(cfg.TFTPRoot())
			sip := cfg.ServerIP
			if sip == "" {
				sip = netif.LocalIPFor(nil).String()
			}
			prefix := fmt.Sprintf("(http,%s:%d)/grub", sip, cfg.HTTPPort)
			st := bootfiles.EnsureGRUB(filepath.Join(cfg.TFTPRoot(), "grub"), prefix)
			if !st.HasEFI && !st.HasBIOS {
				log.Printf("[bootfiles] GRUB2 镜像未生成：\n%s", st.Hint)
			}
		}()
	}

	mstore, err := menu.New(cfg.MenuFile())
	if err != nil {
		log.Fatalf("加载菜单失败: %v", err)
	}

	// TFTP
	go func() {
		if err := tftp.New(cfg.TFTPRoot(), cfg.TFTPPort).Serve(); err != nil {
			log.Printf("[tftp] 启动失败（端口 69 需管理员权限或已占用）: %v", err)
		}
	}()

	// ProxyDHCP（默认关）
	dhcpMgr := proxydhcp.NewManager(cfg)
	if cfg.EnableProxyDHCP {
		dhcpMgr.Start()
	} else {
		log.Printf("[proxydhcp] 未启用（可在 Web 控制台开启）")
	}

	// HTTP
	ws := web.New(cfg, mstore, dhcpMgr)
	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.HTTPPort), Handler: ws.Handler()}
	go func() {
		log.Printf("[http] Web 控制台: http://localhost:%d  (默认 %s/%s)", cfg.HTTPPort, cfg.AdminUser, cfg.AdminPass)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[http] 启动失败: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("正在关闭...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
