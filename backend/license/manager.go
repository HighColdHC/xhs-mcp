package license

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LicenseInfo 授权信息
type LicenseInfo struct {
	Key         string    `json:"key"`          // 卡密
	MachineID   string    `json:"machine_id"`   // 机器码
	ActivatedAt time.Time `json:"activated_at"` // 激活时间
	ExpireAt    time.Time `json:"expire_at"`    // 过期时间
}

// LicenseStatus 授权状态
type LicenseStatus struct {
	Licensed      bool      `json:"licensed"`        // 是否已授权
	Key           string    `json:"key,omitempty"`   // 完整卡密（前端回显用）
	KeyMasked     string    `json:"key_masked,omitempty"` // 掩码卡密（显示用）
	MachineID     string    `json:"machine_id,omitempty"`
	ExpireAt      time.Time `json:"expire_at,omitempty"`
	DaysRemaining int       `json:"days_remaining,omitempty"` // 剩余天数
}

// Manager 授权管理器
type Manager struct {
	licenseFile string
	info        *LicenseInfo
}

// NewManager 创建授权管理器
func NewManager(dataDir string) *Manager {
	licenseFile := filepath.Join(dataDir, "license.json")
	mgr := &Manager{
		licenseFile: licenseFile,
	}
	mgr.load()
	return mgr
}

// load 加载授权信息
func (m *Manager) load() error {
	data, err := os.ReadFile(m.licenseFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，未授权
		}
		return err
	}

	var info LicenseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	m.info = &info
	return nil
}

// save 保存授权信息
func (m *Manager) save() error {
	if m.info == nil {
		return os.Remove(m.licenseFile)
	}

	data, err := json.MarshalIndent(m.info, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.licenseFile, data, 0644)
}

// GetStatus 获取授权状态
func (m *Manager) GetStatus() LicenseStatus {
	if m.info == nil {
		return LicenseStatus{Licensed: false}
	}

	// 检查是否过期
	if time.Now().After(m.info.ExpireAt) {
		return LicenseStatus{Licensed: false}
	}

	daysRemaining := int(time.Until(m.info.ExpireAt).Hours() / 24)

	return LicenseStatus{
		Licensed:      true,
		Key:           m.info.Key,      // 完整卡密
		KeyMasked:     maskKey(m.info.Key), // 掩码卡密
		MachineID:     m.info.MachineID,
		ExpireAt:      m.info.ExpireAt,
		DaysRemaining: daysRemaining,
	}
}

// Activate 使用卡密激活
func (m *Manager) Activate(key string) error {
	// 查找卡密
	predefined := FindKey(key)
	if predefined == nil {
		return fmt.Errorf("无效的卡密")
	}

	// 获取机器码
	machineID, err := GetMachineID()
	if err != nil {
		return fmt.Errorf("获取机器码失败: %w", err)
	}

	// 检查是否已激活
	if m.info != nil && m.info.Key == key {
		// 已激活，验证机器码
		if m.info.MachineID != machineID {
			return fmt.Errorf("卡密已绑定到其他机器")
		}
		// 验证是否过期
		if time.Now().After(m.info.ExpireAt) {
			return fmt.Errorf("授权已过期")
		}
		return nil // 已激活且有效
	}

	// 检查是否已有其他卡密激活
	if m.info != nil {
		// 同一台机器可以重新激活
		if m.info.MachineID != machineID {
			return fmt.Errorf("本机已激活其他卡密")
		}
	}

	// 创建新的授权
	now := time.Now()
	m.info = &LicenseInfo{
		Key:         key,
		MachineID:   machineID,
		ActivatedAt: now,
		ExpireAt:    now.AddDate(0, 0, predefined.ExpireDays),
	}

	return m.save()
}

// maskKey 掩码卡密显示
func maskKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	// 显示前4位和后4位，中间用*代替
	return key[:4] + "****" + key[len(key)-4:]
}
