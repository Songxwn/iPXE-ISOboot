// Package netif 枚举网络接口，供 Web 选择 DHCP 监听网卡与 IP 探测。
package netif

import (
	"net"
	"sort"
)

// Interface 描述一个网卡。
type Interface struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	Up   bool     `json:"up"`
}

// List 返回有 IPv4 的非回环网卡。
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
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil {
					v4 = append(v4, ip4.String())
				}
			}
		}
		if len(v4) == 0 {
			continue
		}
		out = append(out, Interface{ifc.Name, ifc.HardwareAddr.String(), v4, ifc.Flags&net.FlagUp != 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FindByName 按名返回网卡。
func FindByName(name string) *net.Interface {
	if name == "" {
		return nil
	}
	ifc, _ := net.InterfaceByName(name)
	return ifc
}

// PrimaryIPv4 返回网卡首个 IPv4。
func PrimaryIPv4(ifc *net.Interface) net.IP {
	if ifc == nil {
		return nil
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip4 := n.IP.To4(); ip4 != nil {
				return ip4
			}
		}
	}
	return nil
}

// Contains 判断 IP 是否属于该网卡子网。
func Contains(ifc *net.Interface, ip net.IP) bool {
	if ifc == nil {
		return false
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.Contains(ip) {
			return true
		}
	}
	return false
}

// LocalIPFor 返回与对端同子网的本机 IPv4。
func LocalIPFor(peer net.IP) net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return net.IPv4(127, 0, 0, 1)
	}
	var fallback net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := n.IP.To4()
			if ip4 == nil {
				continue
			}
			if fallback == nil {
				fallback = ip4
			}
			if peer != nil && n.Contains(peer) {
				return ip4
			}
		}
	}
	if fallback != nil {
		return fallback
	}
	return net.IPv4(127, 0, 0, 1)
}
