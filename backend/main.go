package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/accounts"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

func resolveDefaultChromePath() string {
	if runtime.GOOS != "windows" {
		return ""
	}

	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, "AppData", "Local", "Google", "Chrome", "Application", "chrome.exe"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func main() {
	// 日志级别：默认 info，可用环境变量 LOG_LEVEL=debug 切换
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = os.Getenv("LOGLEVEL")
	}
	if levelStr != "" {
		if lvl, err := logrus.ParseLevel(levelStr); err == nil {
			logrus.SetLevel(lvl)
		}
	}

	var (
		headless bool
		binPath  string // 浏览器二进制文件路径
		port     string
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&port, "port", ":18060", "端口")
	flag.Parse()

	if len(binPath) == 0 {
		binPath = os.Getenv("ROD_BROWSER_BIN")
	}
	if len(binPath) == 0 {
		if guessed := resolveDefaultChromePath(); guessed != "" {
			binPath = guessed
			logrus.Infof("检测到本机 Chrome: %s", binPath)
		}
	}
	if len(binPath) == 0 {
		logrus.Warn("未指定 Chrome 路径，将使用 Rod 默认下载的浏览器")
	}

	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)

	storePath := os.Getenv("ACCOUNTS_STORE")
	if storePath == "" {
		storePath = "accounts.json"
	}
	profileBase := os.Getenv("USER_DATA_BASE_DIR")
	if profileBase == "" {
		profileBase = "accounts"
	}

	// 初始化授权管理器（使用 profileBase 作为数据目录）
	initLicenseManager(profileBase)

	accountManager, err := accounts.NewManager(storePath, profileBase)
	if err != nil {
		logrus.Fatalf("failed to init account manager: %v", err)
	}

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService(accountManager)

	// 创建并启动应用服务器
	appServer := NewAppServer(xiaohongshuService)
	if err := appServer.Start(port); err != nil {
		logrus.Fatalf("failed to run server: %v", err)
	}
}
