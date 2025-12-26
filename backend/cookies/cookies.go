package cookies

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

type Cookier interface {
	LoadCookies() ([]byte, error)
	SaveCookies(data []byte) error
	DeleteCookies() error
}

type localCookie struct {
	path string
}

func NewLoadCookie(path string) Cookier {
	if path == "" {
		panic("path is required")
	}

	return &localCookie{
		path: path,
	}
}

// LoadCookies 从文件中加载 cookies。
func (c *localCookie) LoadCookies() ([]byte, error) {

	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cookies from tmp file")
	}

	return data, nil
}

// SaveCookies 保存 cookies 到文件中。
func (c *localCookie) SaveCookies(data []byte) error {
	// Ensure the directory exists before writing.
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return errors.Wrap(err, "failed to create cookies directory")
	}
	return os.WriteFile(c.path, data, 0644)
}

// DeleteCookies 删除 cookies 文件。
func (c *localCookie) DeleteCookies() error {
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		// 文件不存在，返回 nil（认为已经删除）
		return nil
	}
	return os.Remove(c.path)
}

// GetCookiesFilePath 获取 cookies 文件路径（不再兼容旧路径）。
func GetCookiesFilePath() string {
	path := os.Getenv("COOKIES_PATH") // 判断环境变量
	if path == "" {
		path = "cookies.json" // fallback，本地调试时用当前目录
	}

	return path
}

// GetCookiesFilePathForAccount returns the cookie file path for a specific account.
func GetCookiesFilePathForAccount(accountID string) string {
	baseDir := os.Getenv("COOKIES_BASE_DIR")
	if baseDir == "" {
		baseDir = "accounts"
	}

	if accountID == "" || accountID == "default" {
		accountID = "default"
	}
	return filepath.Join(baseDir, accountID, "cookies.json")
}
