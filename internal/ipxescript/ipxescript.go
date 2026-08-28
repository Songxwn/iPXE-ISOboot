// Package ipxescript 生成 iPXE 的引导脚本，负责把控制权链式移交给 GRUB2。
//
// iPXE 完成网络初始化后，按固件平台加载对应的 GRUB2 网络引导镜像：
//   - BIOS：GRUB2 的 core 镜像（grub_bios.pxe / core.0）
//   - UEFI x64：grubx64.efi
//   - UEFI ia32：grubia32.efi
//
// GRUB2 启动后会去 (http)/grub/grub.cfg 读取由本服务动态生成的菜单，
// 用 loopback 机制挂载并引导各 ISO（Ventoy 同款思路）。
package ipxescript

import "fmt"

// Boot 返回 iPXE 主脚本内容。httpBase 形如 http://192.168.1.10:8081。
func Boot(httpBase string) string {
	return fmt.Sprintf(`#!ipxe

echo iPXE-ISOboot: chainloading GRUB2 ...
set http_base %s

# 确保有网络
isset ${net0/ip} || dhcp || echo DHCP failed, continuing ...

# 传给 GRUB 的变量：HTTP 根，用于 GRUB 定位 grub.cfg 与 ISO
set grub_prefix ${http_base}/grub

iseq ${platform} efi && goto uefi || goto bios

:uefi
# 按 UEFI 位数选择 grub 二进制
iseq ${buildarch} i386 && goto uefi32 || goto uefi64
:uefi64
echo Loading grubx64.efi ...
chain ${http_base}/grub/grubx64.efi || goto fail
goto done
:uefi32
echo Loading grubia32.efi ...
chain ${http_base}/grub/grubia32.efi || goto fail
goto done

:bios
echo Loading GRUB2 (BIOS) ...
chain ${http_base}/grub/grub_bios.pxe || goto fail
goto done

:fail
echo Failed to load GRUB2. Dropping to iPXE shell.
shell

:done
`, httpBase)
}
