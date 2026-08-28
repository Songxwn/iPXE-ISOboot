# iPXE-ISOboot

一站式 **PXE 网络装机服务**。上传 ISO，通过 Web 控制台编辑启动菜单，即可让内网任意机器网络启动装机。

- ✅ **Linux 各系列发行版**（Ubuntu / Debian / CentOS / Rocky / AlmaLinux / Fedora / openSUSE / Arch / Alpine 等）
- ✅ **Windows**（通过 wimboot 启动 WinPE / 安装镜像）
- ✅ **VMware ESXi**（mboot multiboot 引导）
- ✅ **UEFI 与传统 BIOS** 双模式（自动按客户端体系结构下发引导文件）
- ✅ **Web 控制台**：上传 ISO、自动分析镜像类型、提取引导文件、可视化编辑启动菜单
- ✅ **可选择 DHCP 监听网卡**：多网卡环境下限定在指定内网网卡响应，避免干扰其他网段
- ✅ **在线生成自定义 iPXE 引导 ISO**：无法配置 PXE/DHCP 的环境可下载引导盘，支持自定义连接地址、网卡取 IP 方式、静态 IP、VLAN，UEFI/BIOS 双启动
- ✅ **单二进制、零依赖**：内置 ProxyDHCP + TFTP + HTTP，纯 Go 标准库实现
- ✅ **GitHub Actions 自动编译并发布多平台版本**

## 快速开始

### 1. Linux 一键部署（curl 从 Releases 下载）

在 Linux 服务器上直接从 [Releases](../../releases) 下载对应架构的二进制。以下命令自动识别 amd64/arm64/arm 架构并下载最新版：

```bash
# 自动识别架构并下载最新版到 /usr/local/bin/ipxe-isoboot
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GOARCH=amd64 ;;
  aarch64) GOARCH=arm64 ;;
  armv7l)  GOARCH=arm   ;;
  *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac

# 取最新版本号
VER=$(curl -fsSL https://api.github.com/repos/Songxwn/iPXE-ISOboot/releases/latest \
      | grep -oP '"tag_name":\s*"\K[^"]+')

# 下载并安装
sudo curl -fL -o /usr/local/bin/ipxe-isoboot \
  "https://github.com/Songxwn/iPXE-ISOboot/releases/download/${VER}/ipxe-isoboot_${VER}_linux_${GOARCH}"
sudo chmod +x /usr/local/bin/ipxe-isoboot
ipxe-isoboot -version
```

只想下载固定版本（例如 v1.0.0，amd64）：

```bash
sudo curl -fL -o /usr/local/bin/ipxe-isoboot \
  https://github.com/Songxwn/iPXE-ISOboot/releases/download/v1.0.0/ipxe-isoboot_v1.0.0_linux_amd64
sudo chmod +x /usr/local/bin/ipxe-isoboot
```

> 也可自行编译：`go build -o ipxe-isoboot .`

### 2. 运行

默认 HTTP 控制台端口为 **8081**。

```bash
# Linux（需要 root 以监听 67/69 特权端口）
sudo ipxe-isoboot -data /opt/ipxe-isoboot/data

# 指定端口（覆盖默认 8081）
sudo ipxe-isoboot -data /opt/ipxe-isoboot/data -http 8081

# Windows（以管理员身份运行）
ipxe-isoboot.exe -data .\data
```

首次运行会：
- 创建 `data/` 目录（`iso/`、`tftp/`、`extract/`）
- 自动尝试从 `boot.ipxe.org` 下载 iPXE 引导文件到 `data/tftp/`（无外网时可手动放置）

#### 注册为 systemd 服务（可选，开机自启）

```bash
sudo mkdir -p /opt/ipxe-isoboot/data
sudo tee /etc/systemd/system/ipxe-isoboot.service >/dev/null <<'EOF'
[Unit]
Description=iPXE-ISOboot PXE 网络装机服务
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/ipxe-isoboot -data /opt/ipxe-isoboot/data -http 8081
Restart=on-failure
# 允许非 root 绑定 67/69 特权端口（或直接以 root 运行去掉此行）
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now ipxe-isoboot
sudo systemctl status ipxe-isoboot
```

### 3. 打开控制台

浏览器访问 `http://<服务器IP>:8081`，默认账号 **admin / admin**（请在“设置”中修改）。

> 若使用云服务器/防火墙，记得放行 TCP `8081`（控制台/HTTP）、UDP `69`（TFTP）、UDP `67`（ProxyDHCP，同内网二层才有意义）。

## 使用流程

1. **准备引导文件**：确认 `data/tftp/` 下有 `undionly.kpxe`（BIOS）、`ipxe.efi`（UEFI x64）等。程序会自动下载；也可从 https://boot.ipxe.org 手动获取。Windows 装机还需 `wimboot`。
2. **配置网络引导**，二选一：
   - **内置 ProxyDHCP**（推荐）：在“设置”开启，与现有 DHCP 共存，无需改动路由器。
   - **现有 DHCP**：将 `next-server` 指向本机 IP，`filename` 按架构设为 `undionly.kpxe` 或 `ipxe.efi`。
3. **上传 ISO** → 点击“分析”自动识别类型与内核/initrd → 勾选文件“提取”到 HTTP 可访问路径。
4. **添加启动项**：在“启动菜单”新增，选择类型并填入路径（分析/提取后路径会给出提示）。
5. **目标机器网络启动**，即可看到菜单并进入装机。

## 启动菜单类型说明

| 类型 | 说明 | 关键字段 |
|------|------|---------|
| `linux` | 通用 Linux：加载 kernel + initrd | Kernel、Initrd、内核参数(append) |
| `windows` | wimboot 引导 WinPE/安装镜像 | wimboot、BCD、boot.sdi、boot.wim |
| `esxi` | VMware ESXi multiboot | mboot.c32/mboot.efi、boot.cfg |
| `sanboot` | iPXE 直挂 ISO | ISO URL |
| `custom` | 自定义 iPXE 脚本片段 | 原始脚本 |

### Linux 内核参数示例

不同发行版通过不同参数指向 ISO 源（HTTP）。例如 Ubuntu：

```
boot=casper url=http://<IP>:8081/files/iso/ubuntu.iso ip=dhcp ---
```

CentOS/Rocky：

```
inst.repo=http://<IP>:8081/files/iso/rocky.iso ip=dhcp
```

## 选择 DHCP 监听网卡

在“设置”页的 **DHCP 监听网卡** 下拉框可指定 ProxyDHCP 只在某块网卡（子网）上响应，
适合服务器有多张网卡时避免干扰生产网段。选定网卡后，下发给客户端的 next-server 地址
也会优先使用该网卡的 IP。留空则监听全部网卡并自动按客户端来源子网匹配。

## 生成自定义 iPXE 引导 ISO

适用于**无法配置 PXE/DHCP 引导**的环境（如公有云、受限网络、单机维护）。在“引导 ISO”页填写：

- **连接目标 (chain URL)**：留空默认 `http://本机:8081/boot.ipxe`，也可指向任意 iPXE 脚本
- **网卡取 IP 方式**：`DHCP 自动获取` 或 `静态 IP`（可填 IP/掩码/网关/DNS）
- **目标网卡**：如 `net0`，留空自动
- **VLAN ID**：>0 时先在网卡上创建 VLAN 再取 IP

点击“生成并下载 ISO”得到 `ipxe-boot.iso`。该 ISO：

- **UEFI/BIOS 双启动**：El Torito 同时提供 BIOS(no-emulation `ipxe.lkrn`) 与 UEFI(FAT ESP + `BOOTX64.EFI`) 引导
- 启动后按配置联网并 `chain` 到你的服务器菜单

使用方式：刻录光盘、用 `dd`/Rufus/Ventoy 写入 U 盘，或在虚拟机中挂载为光驱启动。

> 首次生成会自动从 `boot.ipxe.org` 下载 `ipxe.lkrn` 与 `ipxe.efi` 并缓存到 `data/tftp/`。

**依赖工具（服务器需安装）**：生成真正可引导的双启动 ISO 使用与 iPXE 官方相同的工具链，请先安装：

```bash
# Debian / Ubuntu
sudo apt-get install -y xorriso mtools isolinux syslinux-common

# RHEL / Rocky / AlmaLinux
sudo dnf install -y xorriso mtools syslinux
```

“引导 ISO”页顶部会显示工具链是否就绪；缺失时会给出安装提示。

## 一键加入启动菜单

在“ISO 管理”页每个 ISO 都有 **一键加入菜单** 按钮：自动分析镜像类型并创建启动项——
- Linux：自动提取内核/initrd 并填入常见网络安装参数
- Windows / ESXi / 无法识别：以 `sanboot` 直挂 ISO
随后可在“启动菜单”中进一步微调。

## 命令行参数

```
-data string    数据目录 (默认 "data")
-http int       HTTP 端口（覆盖配置文件）
-no-dhcp        禁用内置 ProxyDHCP
-no-fetch       启动时不自动下载 iPXE 引导文件
-version        显示版本
```

## 工作原理

```
客户端 PXE 启动
   │
   ▼
[ProxyDHCP :67]  读取 Option 93(架构) 与 User-Class
   │  裸固件  → 下发 iPXE 二进制（经 TFTP）
   │  已是iPXE → 下发 http://<IP>:8081/boot.ipxe
   ▼
[TFTP :69]   下发 undionly.kpxe / ipxe.efi / wimboot ...
   ▼
[HTTP :8081] /boot.ipxe 动态菜单；/files/ 提供内核/initrd/wim/ISO
   ▼
[Web 控制台] 上传、分析、提取、编辑菜单
```

## 发布新版本

推送符合 `v*` 的 Git 标签即触发自动发布：

```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions 会交叉编译 Linux/Windows/macOS 多架构二进制，生成校验和，并创建 Release。

## 权限说明

TFTP(69) 与 ProxyDHCP(67) 是特权端口：
- **Linux**：`sudo` 运行，或 `setcap 'cap_net_bind_service=+ep' ipxe-isoboot`
- **Windows**：以管理员身份运行

若无法绑定特权端口，程序仍会启动 HTTP 控制台，仅相关服务会打印告警。

## 许可证

MIT
