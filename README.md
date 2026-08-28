# iPXE-ISOboot

一站式 **PXE 网络装机服务**。上传 ISO，通过 Web 控制台编辑启动菜单，即可让内网任意机器网络启动装机。

- ✅ **Linux 各系列发行版**（Ubuntu / Debian / CentOS / Rocky / AlmaLinux / Fedora / openSUSE / Arch / Alpine 等）
- ✅ **Windows**（通过 wimboot 启动 WinPE / 安装镜像）
- ✅ **VMware ESXi**（mboot multiboot 引导）
- ✅ **UEFI 与传统 BIOS** 双模式（自动按客户端体系结构下发引导文件）
- ✅ **Web 控制台**：上传 ISO、自动分析镜像类型、提取引导文件、可视化编辑启动菜单
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
