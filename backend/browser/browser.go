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
}

// New launches a new rod browser with the provided configuration.
func New(cfg Config) (*Browser, error) {
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
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

	l := launcher.New().Context(ctx).
		Headless(cfg.Headless).
		Leakless(false).
		Set(flags.NoSandbox).
		Set(flags.Flag("no-first-run")).
		Set(flags.Flag("no-default-browser-check")).
		// 禁用 Chromium 的无关日志
		Set(flags.Flag("disable-logging")).
		Set(flags.Flag("disable-gpu-computing")).
		Set(flags.Flag("disable-software-rasterizer")).
		Set(flags.Flag("log-level"), "3").
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

	logrus.Infof("browser launch: headless=%t bin=%q userData=%q proxy=%q", cfg.Headless, cfg.BinPath, cfg.UserDataDir, proxyForChrome)
	logrus.Infof("browser launch args: %s", strings.Join(l.FormatArgs(), " "))
	logrus.Info("browser launch: starting Chromium process")

	controlURL, err := l.Launch()
	if err != nil {
		logrus.Errorf("browser launch failed: %v", err)
		return nil, err
	}
	logrus.Infof("browser launch: control url=%s", controlURL)

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

	return &Browser{
		browser:  rb,
		launcher: l,
		fp:       cfg.Fingerprint,
		bridge:   bridgeStop,
		cleanup:  cfg.UserDataDir == "",
	}, nil
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
	lockFiles := []string{"SingletonLock", "SingletonCookie", "SingletonSocket", "DevToolsActivePort"}
	for _, name := range lockFiles {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logrus.Debugf("cleanup user data lock failed: %s %v", path, err)
		}
	}
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
  // Skip Intl.DateTimeFormat modification due to stealth.js conflicts
  // if (typeof Intl !== 'undefined' && Intl.DateTimeFormat && Intl.DateTimeFormat.prototype) {
  //   const orig = Intl.DateTimeFormat.prototype.resolvedOptions;
  //   Intl.DateTimeFormat.prototype.resolvedOptions = function(...args) {
  //     let o = {};
  //     try {
  //       if (orig && typeof orig === 'function' && typeof orig.apply === 'function') {
  //         o = orig.apply(this, args) || {};
  //       }
  //     } catch (e) {
  //       // 如果原方法调用失败，使用空对象
  //     }
  //     return Object.assign({}, o, { timeZone: tz });
  //   };
  // }
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

