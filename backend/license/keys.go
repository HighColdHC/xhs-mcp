package license

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PredefinedKey 预置卡密配置
type PredefinedKey struct {
	Key         string // 卡密
	ExpireDays  int    // 有效期（天数）
	MaxMachines int    // 最多绑定机器数
}

// GetPredefinedKeys 获取预置卡密列表
// 从 license_keys.txt 文件读取卡密
func GetPredefinedKeys() []PredefinedKey {
	keys := make([]PredefinedKey, 0, 300)

	// 获取当前文件所在目录
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	keyFile := filepath.Join(dir, "license_keys.txt")

	// 读取卡密文件
	f, err := os.Open(keyFile)
	if err != nil {
		// 文件不存在，返回空列表
		return keys
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 解析卡密格式: PREFIX-XXXX-XXX-XXX
		parts := strings.SplitN(line, "-", 2)
		if len(parts) < 2 {
			continue
		}

		prefix := parts[0]
		var expireDays int

		switch prefix {
		case "7D":
			expireDays = 7
		case "1M":
			expireDays = 30
		case "1Y":
			expireDays = 365
		default:
			// 未知前缀，跳过
			continue
		}

		keys = append(keys, PredefinedKey{
			Key:         line,
			ExpireDays:  expireDays,
			MaxMachines: 1,
		})
	}

	return keys
}

// FindKey 查找卡密
func FindKey(key string) *PredefinedKey {
	keys := GetPredefinedKeys()
	for i := range keys {
		if keys[i].Key == key {
			return &keys[i]
		}
	}
	return nil
}
