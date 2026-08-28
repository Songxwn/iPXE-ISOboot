# iPXE-ISOboot

**Ventoy 风格的网络 ISO 启动服务**。上传 ISO，客户端网络启动即可直接引导——
借鉴 [Ventoy](https://github.com/ventoy/Ventoy) 的 **GRUB2 `loopback` 挂载 ISO** 机制，
把它搬到网络 PXE 环境：无需把每个 ISO 提取内核，GRUB2 直接挂载 HTTP 上的 ISO 引导。

- 网络引导链：**iPXE → GRUB2 → loopback 挂载 ISO**
- 支持 **UEFI 与传统 BIOS**（自动按客户端架构下发 iPXE，再按平台加载对应 GRUB2）
- 内置 **ProxyDHCP + TFTP + HTTP + Web 控制台**（单 Go 二进制）
- Web 上传 ISO、一键加入菜单（自动识别配方族）、编辑菜单、预览 grub.cfg
- GitHub Actions 自动编译并发布多平台版本

## 与 Ventoy 的关系

Ventoy 是**本地 U 盘**方案：GRUB2 + `loopback` 把 U 盘上的 ISO 当虚拟光驱引导。
本项目把同样的 **GRUB2 loopback 机制**用在**网络**上：ISO 放在服务器，通过 HTTP 提供，
GRUB2 用 `loopback loop (http)/iso/xxx.iso` 挂载后按发行版配方引导。

> **现实边界**：网络下 GRUB loopback 对绝大多数 Linux live/安装器有效；但个别系统
> （尤其 Windows）无法通过网络 loopback 引导安装器——这是网络传输的限制，非本项目缺陷。

## 依赖

生成 GRUB2 网络引导镜像需要 grub-mkimage（服务器端）：

```bash
# Debian / Ubuntu
sudo apt-get install -y grub-efi-amd64-bin grub-pc-bin grub-common

# RHEL / Rocky / AlmaLinux
sudo dnf install -y grub2-efi-x64-modules grub2-pc-modules grub2-tools
```

## 部署（Linux, curl 从 Releases 下载）

```bash
ARCH=$(uname -m); case "$ARCH" in x86_64) GOARCH=amd64;; aarch64) GOARCH=arm64;; armv7l) GOARCH=arm;; esac
VER=$(curl -fsSL https://api.github.com/repos/Songxwn/iPXE-ISOboot/releases/latest | grep -oP '"tag_name":\s*"\K[^"]+')
sudo curl -fL -o /usr/local/bin/ipxe-isoboot \
  "https://github.com/Songxwn/iPXE-ISOboot/releases/download/${VER}/ipxe-isoboot_${VER}_linux_${GOARCH}"
sudo chmod +x /usr/local/bin/ipxe-isoboot
sudo ipxe-isoboot -data /opt/ipxe-isoboot/data
```

默认 HTTP 控制台端口 **8081**，默认账号 **admin / admin**。

## 使用流程

1. 安装上面的 GRUB2 依赖，启动服务（首次会下载 iPXE、生成 GRUB2 网络镜像到 `data/tftp/grub`）。
2. 打开 `http://<IP>:8081`，在“设置”开启 ProxyDHCP（或让现有 DHCP 的 next-server 指向本机）。
3. “ISO 管理”上传 ISO → 点击 **一键加入菜单**（自动识别配方族）。
4. 目标机器网络启动 → iPXE → GRUB2 菜单 → 选中 ISO 直接引导。

## 配方族（loopback 引导规则）

| 族 | 适用 | 机制 |
|----|------|------|
| ubuntu | Ubuntu/Mint (casper) | `boot=casper iso-scan/filename=` |
| debian | Debian Live | `boot=live fetch=` |
| debian_di | Debian 安装器 | `fetch=` netinst |
| rhel | RHEL/Rocky/Alma/Fedora | `inst.repo= inst.stage2=` |
| arch | Arch | `img_loop= archisobasedir=` |
| alpine | Alpine | `modloop=` |
| opensuse | openSUSE | `install=` |
| kali | Kali | `boot=live fetch=` |
| generic | 其它 | 尝试 ISO 内 `/boot/grub/grub.cfg` |
| windows | Windows | 网络 loopback 不支持，提示改用其它方式 |

菜单项支持“自定义 grub 片段”，用 `${iso_url}` 引用 ISO 地址，完全手动控制引导命令。

## 命令行参数

```
-data string   数据目录 (默认 "data")
-http int      HTTP 端口（覆盖配置）
-no-fetch      不自动下载/生成引导文件
-version       显示版本
```

## 许可证

MIT
