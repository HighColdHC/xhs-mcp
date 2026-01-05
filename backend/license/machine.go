package license

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
)

// GetMachineID 获取机器唯一标识
// 使用多个标识符组合，确保可靠性
func GetMachineID() (string, error) {
	if runtime.GOOS == "windows" {
		return getWindowsMachineID()
	}
	return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
}

// getWindowsMachineID 获取 Windows 机器码
// 使用: 主机名 + MAC地址 + 用户名
func getWindowsMachineID() (string, error) {
	// 获取主机名
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// 获取第一个可用的 MAC 地址
	macAddr := getMACAddress()

	// 获取用户名
	username := os.Getenv("USERNAME")
	if username == "" {
		username = os.Getenv("USER")
	}

	// 组合多个标识符
	combined := hostname + "-" + macAddr + "-" + username
	hash := md5.New()
	hash.Write([]byte(combined))
	return hex.EncodeToString(hash.Sum(nil))[:16], nil // 取前16位
}

// getMACAddress 获取第一个可用的 MAC 地址
func getMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "00:00:00:00:00:00"
	}

	for _, iface := range interfaces {
		// 跳过回环接口和没有 MAC 地址的接口
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String()
	}

	return "00:00:00:00:00:00"
}
