package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	// 基础上下文设置 - 不在这里设置超时，让每次重试独立控制
	// 避免外层超时削减内层重试的超时时间
	logrus.Infof("browser launch: using attempt-level timeout control")

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

	// 创建带特定 context 的 launcher 的辅助函数
	makeLauncherWithContext := func(launchCtx context.Context) *launcher.Launcher {
		// 🔥 修复 Windows 启动卡死问题：
		// Rod 在 headless=false 时会自动添加 --no-startup-window
		// 这会导致 Chrome 在 Windows 上启动卡死。
		// 解决方案：使用 headless=true 但添加参数强制显示窗口。
		// 🔥 修复 Leakless 辅助进程被杀软拦截问题：关闭 Leakless 模式
		l := launcher.New().Context(launchCtx).
			Leakless(false).  // Windows 下 Leakless 辅助进程可能被杀软拦截，导致 Chrome 永远无法启动
			Set(flags.NoSandbox).
			Set(flags.Flag("no-first-run")).
			Set(flags.Flag("no-default-browser-check")).
			Logger(os.Stdout)

		// 设置 headless 模式（直接使用用户配置，不再尝试绕过 --no-startup-window）
		l = l.Headless(cfg.Headless)
		if !cfg.Headless {
			logrus.Info("browser launch: headless=false mode (visible window)")
		}

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
		logrus.Infof("browser launch: ===== Attempt %d START =====", attempt)

		// ✅ 为每次重试创建独立的 context（独立30秒，不受外层影响）
		// 使用独立的background context避免外层超时削减
		attemptCtx, attemptCancel := context.WithTimeout(context.Background(), 30*time.Second)
		logrus.Infof("browser launch: created independent attempt context with 30s timeout")

		// 使用新的 context 创建 launcher
		l = makeLauncherWithContext(attemptCtx)
		logrus.Infof("browser launch: created launcher")
		logrus.Infof("browser launch: headless=%t bin=%q userData=%q proxy=%q (attempt %d)", cfg.Headless, cfg.BinPath, cfg.UserDataDir, proxyForChrome, attempt)
		logrus.Infof("browser launch args: %s", strings.Join(l.FormatArgs(), " "))

		// 启动前强制清理锁文件
		if cfg.UserDataDir != "" {
			logrus.Infof("browser launch: cleaning locks before launch")
			cleanupUserDataLocks(cfg.UserDataDir)
			logrus.Infof("browser launch: lock cleanup completed")
		}

		if attempt > 1 {
			logrus.Info("browser launch: retrying after previous failure")
		}
		logrus.Infof("browser launch: about to call l.Launch()...")
		logrus.Infof("browser launch: ===== Attempt %d EXECUTING LAUNCH =====", attempt)

		startTime := time.Now()
		controlURL, err = l.Launch()
		duration := time.Since(startTime)

		logrus.Infof("browser launch: ===== Attempt %d LAUNCH RETURNED (duration: %v) =====", attempt, duration)

		// ✅ 立即取消 context
		attemptCancel()

		// 检查结果
		if err != nil {
			logrus.Errorf("browser launch failed (attempt %d): %v", attempt, err)
			logrus.Errorf("browser launch: error type: %T", err)
			// 记录 context 错误详情
			if attemptCtx.Err() != nil {
				logrus.Errorf("browser launch: context error on attempt %d: %v", attempt, attemptCtx.Err())
			}
			// 检查是否是超时错误
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
				logrus.Errorf("browser launch: timeout error detected")
			}
		} else {
			logrus.Infof("browser launch: SUCCESS! control url=%s", controlURL)
			logrus.Infof("browser launch: ===== Attempt %d COMPLETED SUCCESSFULLY =====", attempt)
			break
		}

		// 等待后重试
		if attempt < 2 {
			logrus.Infof("browser launch: waiting 500ms before retry...")
			time.Sleep(500 * time.Millisecond)
		}
		logrus.Infof("browser launch: ===== Attempt %d END =====", attempt)
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

	// 🔥 修复：不再获取和保存 PID，避免误杀用户的 Chrome 浏览器
	// b.launcher.Kill() 已经能正确清理

	return &Browser{
		browser:  rb,
		launcher: l,
		fp:       cfg.Fingerprint,
		bridge:   bridgeStop,
		cleanup:  cfg.UserDataDir == "",
		pid:      0,  // 不再使用
	}, nil
}

// 🔥 删除 getChromePID 函数 - 不再使用，会误杀用户的 Chrome 浏览器

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

	// 🔥 修复：删除强制清理代码
	// b.launcher.Kill() 已经能正确清理 Chrome 进程
	// 旧的 getChromePID() 会误杀用户自己的 Chrome 浏览器
	// 不再需要额外的强制清理
}

// 🔥 删除 forceKillChrome 函数 - 不再使用，会误杀用户的 Chrome 浏览器

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

