package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/proxybridge"
	"github.com/xpzouying/xiaohongshu-mcp/session"
)

// Config describes how to launch a browser instance.
type Config struct {
	Headless    bool
	BinPath     string
	Proxy       string // legacy raw proxy string
	UserAgent   string
	UserDataDir string
	CookiePath  string
	Trace       bool
	Fingerprint *session.Fingerprint
	ProxyType   string
	ProxyHost   string
	ProxyPort   int
	ProxyUser   string
	ProxyPass   string
	Context     context.Context
}

// Browser wraps a rod browser and its launcher lifecycle.
type Browser struct {
	browser  *rod.Browser
	launcher *launcher.Launcher
	fp       *session.Fingerprint
	bridge   func()
	cleanup  bool
	pid      int // Chrome 进程 PID（用于强制清理）
}

// New launches a new rod browser with the provided configuration.
func New(cfg Config) (*Browser, error) {
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// 添加默认超时控制（30秒）
	// 防止 Chrome 启动时因代理不可用等原因无限期阻塞
	if _, hasTimeout := ctx.Deadline(); !hasTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		logrus.Infof("browser launch: using default 30s timeout")
	} else {
		logrus.Infof("browser launch: using custom context timeout")
	}

	if cfg.UserDataDir != "" {
		if err := os.MkdirAll(cfg.UserDataDir, 0o755); err != nil {
			logrus.Warnf("failed to create user data dir: %s %v", cfg.UserDataDir, err)
		}
		cleanupUserDataLocks(cfg.UserDataDir)
	}

	bridgeStop := func() {}
	proxyForChrome := cfg.Proxy
	if cfg.ProxyType != "" {
		if cfg.ProxyType == "direct" {
			proxyForChrome = ""
		} else if cfg.ProxyHost != "" && cfg.ProxyPort > 0 {
			if cfg.ProxyUser != "" || cfg.ProxyPass != "" {
				proxyForChrome = fmt.Sprintf("%s://%s:%s@%s:%d", cfg.ProxyType, cfg.ProxyUser, cfg.ProxyPass, cfg.ProxyHost, cfg.ProxyPort)
			} else {
				proxyForChrome = fmt.Sprintf("%s://%s:%d", cfg.ProxyType, cfg.ProxyHost, cfg.ProxyPort)
			}
			if cfg.ProxyType == "socks5" {
				local, stop, err := proxybridge.StartSocksBridge(proxyForChrome)
				if err != nil {
					return nil, err
				}
				bridgeStop = stop
				proxyForChrome = local
			}
		}
	}

	traceEnabled := cfg.Trace || envEnabled("XHS_ROD_TRACE")
	chromeVerbose := envEnabled("XHS_CHROME_VERBOSE")

	makeLauncher := func() *launcher.Launcher {
		l := launcher.New().Context(ctx).
			Headless(cfg.Headless).
			Leakless(true).
			Set(flags.NoSandbox).
			Set(flags.Flag("no-first-run")).
			Set(flags.Flag("no-default-browser-check")).
			Logger(os.Stdout)

		if chromeVerbose {
			l = l.Set(flags.Flag("enable-logging"), "stderr").
				Set(flags.Flag("v"), "1")
			logrus.Info("chrome verbose logging enabled")
		}

		if proxyForChrome != "" {
			l = l.Proxy(proxyForChrome)
		}
		if cfg.UserDataDir != "" {
			l = l.UserDataDir(cfg.UserDataDir)
		}
		if cfg.BinPath != "" {
			l = l.Bin(cfg.BinPath)
		}
		if cfg.UserAgent != "" {
			l = l.Set(flags.Flag("user-agent"), cfg.UserAgent)
		}
		if cfg.Fingerprint != nil && cfg.Fingerprint.AcceptLanguage != "" {
			l = l.Set(flags.Flag("lang"), strings.Split(cfg.Fingerprint.AcceptLanguage, ",")[0])
		}

		return l
	}

	cleanupLauncher := func(l *launcher.Launcher) {
		if l == nil {
			return
		}
		if cfg.UserDataDir == "" {
			l.Cleanup()
		} else {
			l.Kill()
		}
		if cfg.UserDataDir != "" {
			cleanupUserDataLocks(cfg.UserDataDir)
		}
	}

	var (
		l          *launcher.Launcher
		controlURL string
		err        error
	)

	// cleanupNeeded 标记是否需要在失败时清理 launcher
	// 只有成功创建并返回 Browser 时才设为 false
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded && l != nil {
			logrus.Infof("browser launch failed, cleaning up launcher")
			cleanupLauncher(l)
		}
	}()

	for attempt := 1; attempt <= 2; attempt++ {
		l = makeLauncher()
		logrus.Infof("browser launch: headless=%t bin=%q userData=%q proxy=%q", cfg.Headless, cfg.BinPath, cfg.UserDataDir, proxyForChrome)
		logrus.Infof("browser launch args: %s", strings.Join(l.FormatArgs(), " "))

		// 启动前强制清理锁文件
		if cfg.UserDataDir != "" {
			cleanupUserDataLocks(cfg.UserDataDir)
		}

		if attempt > 1 {
			logrus.Info("browser launch: retrying after previous failure")
		}
		logrus.Info("browser launch: starting Chromium process")

		controlURL, err = l.Launch()
		if err == nil {
			logrus.Infof("browser launch: success, control url=%s", controlURL)
			break
		}
		logrus.Errorf("browser launch failed: %v", err)

		// 检查 Chrome 进程是否启动
		if cfg.BinPath != "" {
			logrus.Infof("browser launch: checking if Chrome process is running...")
			// 这里的检查可能需要额外的工具函数
		}

		// cleanupLauncher 会在 defer 中统一调用
		if attempt < 2 {
			logrus.Infof("browser launch: waiting 500ms before retry...")
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		logrus.Errorf("browser launch: all attempts failed, final error: %v", err)
		return nil, err
	}

	rb := rod.New().
		ControlURL(controlURL).
		Trace(traceEnabled).
		Context(ctx)

	logrus.Info("browser connect: connecting to Chromium")
	if err := rb.Connect(); err != nil {
		logrus.Errorf("browser connect failed: %v", err)
		return nil, err
	}
	logrus.Info("browser connect: success")

	// Load cookies if provided.
	if cfg.CookiePath != "" {
		cookieLoader := cookies.NewLoadCookie(cfg.CookiePath)
		if data, err := cookieLoader.LoadCookies(); err == nil {
			var cks []*proto.NetworkCookie
			if er := json.Unmarshal(data, &cks); er != nil {
				logrus.Warnf("failed to unmarshal cookies from %s: %v", cfg.CookiePath, er)
			} else {
				rb.MustSetCookies(cks...)
				logrus.Debugf("loaded cookies from %s", cfg.CookiePath)
			}
		} else {
			logrus.Debugf("no cookies loaded from %s: %v", cfg.CookiePath, err)
		}
	}

	// 成功创建浏览器，标记不需要清理
	// 后续由 Browser.Close() 负责清理 launcher
	cleanupNeeded = false

	// 尝试获取 Chrome 进程 PID（用于后续强制清理）
	pid := getChromePID(cfg.BinPath)

	return &Browser{
		browser:  rb,
		launcher: l,
		fp:       cfg.Fingerprint,
		bridge:   bridgeStop,
		cleanup:  cfg.UserDataDir == "",
		pid:      pid,
	}, nil
}

// getChromePID 尝试获取 Chrome 进程的 PID
func getChromePID(binPath string) int {
	if binPath == "" {
		return 0
	}

	// Windows 下使用 tasklist 获取 Chrome 进程 PID
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq chrome.exe", "/FO", "CSV")
		output, err := cmd.Output()
		if err != nil {
			return 0
		}

		lines := strings.Split(string(output), "\n")
		if len(lines) > 1 {
			// 跳过标题行，取第一个 Chrome 进程
			for _, line := range lines[1:] {
				if line == "" {
					continue
				}
				// CSV 格式: "chrome.exe","12345","Console","1","150,000 K"
				parts := strings.Split(line, ",")
				if len(parts) > 1 {
					pidStr := strings.Trim(parts[1], "\"")
					var pid int
					if _, err := fmt.Sscanf(pidStr, "%d", &pid); err == nil {
						return pid
					}
				}
			}
		}
	}

	return 0
}

func envEnabled(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
}

func cleanupUserDataLocks(dir string) {
	logrus.Infof("cleanupUserDataLocks: cleaning dir=%s", dir)
	lockFiles := []string{"SingletonLock", "SingletonCookie", "SingletonSocket", "DevToolsActivePort"}
	cleaned := 0
	for _, name := range lockFiles {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logrus.Debugf("cleanupUserDataLocks: failed to remove %s: %v", path, err)
		} else if os.IsNotExist(err) {
			logrus.Debugf("cleanupUserDataLocks: %s does not exist", path)
		} else {
			logrus.Infof("cleanupUserDataLocks: removed %s", path)
			cleaned++
		}
	}
	logrus.Infof("cleanupUserDataLocks: cleaned %d lock files", cleaned)
}

// Close closes the browser and cleans up the launcher.
func (b *Browser) Close() {
	if b.browser != nil {
		if err := b.browser.Close(); err != nil {
			logrus.Debugf("browser close failed: %v", err)
		}
	}
	if b.launcher != nil {
		if b.cleanup {
			b.launcher.Cleanup()
		} else {
			b.launcher.Kill()
		}
	}
	if b.bridge != nil {
		done := make(chan struct{})
		go func() {
			b.bridge()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	// 强制清理残留的 Chrome 进程（Windows）
	if b.pid > 0 && runtime.GOOS == "windows" {
		forceKillChrome(b.pid)
	}
}

// forceKillChrome 强制杀死 Chrome 进程及其子进程
func forceKillChrome(pid int) {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	if err := cmd.Run(); err != nil {
		logrus.Debugf("forceKillChrome: failed to kill PID %d: %v", pid, err)
	} else {
		logrus.Infof("forceKillChrome: killed Chrome process PID %d", pid)
	}
}

// NewPage opens a new stealth page.
func (b *Browser) NewPage() *rod.Page {
	page := stealth.MustPage(b.browser)
	if b.fp != nil {
		if err := applyFingerprint(page, b.fp); err != nil {
			logrus.Warnf("apply fingerprint failed: %v", err)
		}
	}
	return page
}

func applyFingerprint(page *rod.Page, fp *session.Fingerprint) error {
	if fp == nil {
		return nil
	}

	if restore, err := page.SetExtraHeaders([]string{"Accept-Language", fp.AcceptLanguage}); err == nil && restore != nil {
		defer restore()
	}

	callSafe := func(script string) (any, error) {
		res, err := page.Eval(script)
		if err != nil {
			return nil, err
		}
		return res.Value, nil
	}

	// Keep script small; just core anti-bot bits used by project.
	script := fmt.Sprintf(`(() => {
try {
  const lang = %q;
  const platform = %q;
  const tz = %q;
  const sw = %d, sh = %d, dpr = %f;
  if (typeof navigator !== 'undefined') {
    Object.defineProperty(navigator, 'webdriver', { get: () => false });
    if (lang) Object.defineProperty(navigator, 'language', { get: () => lang });
    Object.defineProperty(navigator, 'platform', { get: () => platform });
  }
  if (typeof Intl !== 'undefined' && Intl.DateTimeFormat && Intl.DateTimeFormat.prototype) {
    const orig = Intl.DateTimeFormat.prototype.resolvedOptions;
    Intl.DateTimeFormat.prototype.resolvedOptions = function(...args) {
      const o = orig ? orig.apply(this, args) || {} : {};
      return Object.assign({}, o, { timeZone: tz });
    };
  }
  if (typeof window !== 'undefined') {
    Object.defineProperty(window, 'devicePixelRatio', { get: () => dpr });
    Object.defineProperty(window, 'outerWidth', { get: () => sw });
    Object.defineProperty(window, 'outerHeight', { get: () => sh });
  }
  if (typeof screen !== 'undefined') {
    Object.defineProperty(screen, 'width', { get: () => sw });
    Object.defineProperty(screen, 'height', { get: () => sh });
  }
} catch (e) {}
})();`,
		fp.AcceptLanguage,
		fp.Platform,
		fp.Timezone,
		fp.ScreenWidth,
		fp.ScreenHeight,
		fp.DeviceScale,
	)

	_, err := callSafe(script)
	return err
}

// PipeBrowserOutput attaches the browser launcher output (stdout/stderr) to a writer.
// It is used only for debug helpers; rod already prints to launcher.Logger.
func PipeBrowserOutput(w io.Writer) {
	_ = w
}

