// Package netif 枚举本机网络接口，供 Web 选择 DHCP 监听网卡。
package netif

import (
	"net"
	"sort"
)

// Interface 描述一个可用于 DHCP 的网卡。
type Interface struct {
	Name string   `json:"name"` // 系统网卡名
	MAC  string   `json:"mac"`  // 硬件地址
	IPv4 []string `json:"ipv4"` // 该网卡的 IPv4 地址列表
	Up   bool     `json:"up"`   // 是否已启用
}

// List 返回所有拥有 IPv4 地址、非回环的网卡。
func List() []Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Interface
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		var v4 []string
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					v4 = append(v4, ip4.String())
				}
			}
		}
		if len(v4) == 0 {
			continue // 仅展示有 IPv4 的网卡
		}
		out = append(out, Interface{
			Name: ifc.Name,
			MAC:  ifc.HardwareAddr.String(),
			IPv4: v4,
			Up:   ifc.Flags&net.FlagUp != 0,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FindByName 按名称返回网卡；找不到返回 nil。
func FindByName(name string) *net.Interface {
	if name == "" {
		return nil
	}
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	return ifc
}

// PrimaryIPv4 返回指定网卡的首个 IPv4 地址；网卡不存在或无地址返回 nil。
func PrimaryIPv4(ifc *net.Interface) net.IP {
	if ifc == nil {
		return nil
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4
			}
		}
	}
	return nil
}

// Contains 判断给定 IP 是否属于该网卡的某个子网。
func Contains(ifc *net.Interface, ip net.IP) bool {
	if ifc == nil {
		return false
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.Contains(ip) {
				return true
			}
		}
	}
	return false
}
