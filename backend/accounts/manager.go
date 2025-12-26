package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/session"
)

// Account represents an isolated account with its own proxy, fingerprint and storage paths.
type Account struct {
	ID          int                  `json:"id"`
	Key         string               `json:"key"`
	Name        string               `json:"name,omitempty"`
	Proxy       string               `json:"proxy,omitempty"`
	ProxyType   string               `json:"proxy_type,omitempty"`
	ProxyHost   string               `json:"proxy_host,omitempty"`
	ProxyPort   int                  `json:"proxy_port,omitempty"`
	ProxyUser   string               `json:"proxy_user,omitempty"`
	ProxyPass   string               `json:"proxy_pass,omitempty"`
	Fingerprint *session.Fingerprint `json:"fingerprint,omitempty"`
	CookiePath  string               `json:"cookie_path"`
	ProfilePath string               `json:"profile_path"`
	LoggedIn    bool                 `json:"logged_in"`
	LastLogin   time.Time            `json:"last_login,omitempty"`
}

// ProxyConfig structured proxy config.
type ProxyConfig struct {
	Type string
	Host string
	Port int
	User string
	Pass string
	Raw  string
}

// Manager manages account lifecycle and persistence.
type Manager struct {
	mu          sync.Mutex
	accounts    map[int]*Account
	keyIndex    map[string]*Account
	nextID      int
	storePath   string
	profileBase string
}

// NewManager creates a manager with persistence.
// storePath: JSON file path. profileBase: base dir for user data dir (per account).
func NewManager(storePath, profileBase string) (*Manager, error) {
	m := &Manager{
		accounts:    map[int]*Account{},
		keyIndex:    map[string]*Account{},
		nextID:      1,
		storePath:   storePath,
		profileBase: profileBase,
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Create creates a new account with optional proxy/name and generated fingerprint.
func (m *Manager) Create(proxy, name string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++

	key := fmt.Sprintf("acc_%d", id)
	fp := session.RandomDesktopFingerprint()
	cookiePath := cookies.GetCookiesFilePathForAccount(key)
	profilePath := filepath.Join(m.profileBase, key, "profile")

	acc := &Account{
		ID:          id,
		Key:         key,
		Name:        name,
		Proxy:       proxy,
		ProxyType:   "",
		ProxyHost:   "",
		ProxyPort:   0,
		ProxyUser:   "",
		ProxyPass:   "",
		Fingerprint: fp,
		CookiePath:  cookiePath,
		ProfilePath: profilePath,
		LoggedIn:    false,
	}

	m.accounts[id] = acc
	m.keyIndex[key] = acc
	return acc, m.saveLocked()
}

// Get returns account by id.
func (m *Manager) Get(id int) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, errors.Errorf("account %d not found", id)
	}
	return acc, nil
}

// GetByKey returns account by key.
func (m *Manager) GetByKey(key string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.keyIndex[key]
	if !ok {
		return nil, errors.Errorf("account %s not found", key)
	}
	return acc, nil
}

// List returns shallow copies of accounts.
func (m *Manager) List() []Account {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Account, 0, len(m.accounts))
	for _, acc := range m.accounts {
		copyAcc := *acc
		out = append(out, copyAcc)
	}
	return out
}

// UpdateProxy updates proxy/name for account.
func (m *Manager) UpdateProxy(id int, proxy string, name string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, errors.Errorf("account %d not found", id)
	}
	acc.Proxy = proxy
	if name != "" {
		acc.Name = name
	}
	acc.LoggedIn = false
	return acc, m.saveLocked()
}

// SetName sets name for account.
func (m *Manager) SetName(id int, name string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, errors.Errorf("account %d not found", id)
	}
	acc.Name = name
	return acc, m.saveLocked()
}

// ApplyProxyConfig sets structured proxy config (and raw if provided).
func (m *Manager) ApplyProxyConfig(id int, cfg ProxyConfig) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, errors.Errorf("account %d not found", id)
	}
	if cfg.Raw == "" {
		if cfg.Type != "" && cfg.Type != "direct" && cfg.Host != "" && cfg.Port > 0 {
			if cfg.User != "" || cfg.Pass != "" {
				cfg.Raw = fmt.Sprintf("%s://%s:%s@%s:%d", cfg.Type, cfg.User, cfg.Pass, cfg.Host, cfg.Port)
			} else {
				cfg.Raw = fmt.Sprintf("%s://%s:%d", cfg.Type, cfg.Host, cfg.Port)
			}
		}
	}
	acc.Proxy = cfg.Raw
	acc.ProxyType = cfg.Type
	acc.ProxyHost = cfg.Host
	acc.ProxyPort = cfg.Port
	acc.ProxyUser = cfg.User
	acc.ProxyPass = cfg.Pass
	acc.LoggedIn = false
	return acc, m.saveLocked()
}

// MarkLoggedIn updates logged-in status and timestamp.
func (m *Manager) MarkLoggedIn(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if acc, ok := m.keyIndex[key]; ok {
		acc.LoggedIn = true
		acc.LastLogin = time.Now()
		_ = m.saveLocked()
	}
}

// Delete removes account and its files.
func (m *Manager) Delete(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return errors.Errorf("account %d not found", id)
	}
	delete(m.accounts, id)
	delete(m.keyIndex, acc.Key)

	// Remove cookie/profile paths.
	_ = os.Remove(acc.CookiePath)
	_ = os.RemoveAll(acc.ProfilePath)
	return m.saveLocked()
}

func (m *Manager) saveLocked() error {
	if m.storePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		NextID   int        `json:"next_id"`
		Accounts []*Account `json:"accounts"`
	}{
		NextID:   m.nextID,
		Accounts: collectAccounts(m.accounts),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.storePath, data, 0o644)
}

func (m *Manager) load() error {
	if m.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var payload struct {
		NextID   int        `json:"next_id"`
		Accounts []*Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.NextID > 0 {
		m.nextID = payload.NextID
	}
	for _, acc := range payload.Accounts {
		m.accounts[acc.ID] = acc
		m.keyIndex[acc.Key] = acc
		if acc.ID >= m.nextID {
			m.nextID = acc.ID + 1
		}
	}
	return nil
}

func collectAccounts(m map[int]*Account) []*Account {
	out := make([]*Account, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
