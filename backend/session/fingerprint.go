package session

import (
	"fmt"
	"math/rand"
	"time"
)

// Fingerprint represents a deterministic browser fingerprint.
type Fingerprint struct {
	UserAgent           string  `json:"user_agent"`
	AcceptLanguage      string  `json:"accept_language"`
	Platform            string  `json:"platform"`
	Timezone            string  `json:"timezone"`
	ScreenWidth         int     `json:"screen_width"`
	ScreenHeight        int     `json:"screen_height"`
	DeviceScale         float64 `json:"device_scale"`
	HardwareConcurrency int     `json:"hardware_concurrency"`
	DeviceMemory        int     `json:"device_memory"`
	WebglVendor         string  `json:"webgl_vendor"`
	WebglRenderer       string  `json:"webgl_renderer"`
}

var (
	fpRng           = rand.New(rand.NewSource(time.Now().UnixNano()))
	winUserAgents   = []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36"}
	macUserAgents   = []string{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36"}
	screenOptions   = []struct{ W, H int; D float64 }{{1920, 1080, 1.25}, {1536, 864, 1.0}, {1366, 768, 1.0}}
	webglOptions    = []struct{ Vendor, Renderer string }{
		{"Intel Inc.", "Intel(R) UHD Graphics"},
		{"NVIDIA Corporation", "NVIDIA GeForce GTX 1650/PCIe/SSE2"},
		{"Intel Inc.", "Intel(R) Iris(R) Plus Graphics 640"},
	}
	hwcOptions      = []int{4, 6, 8}
	deviceMemOptions = []int{8, 16}
)

// RandomDesktopFingerprint generates a China-desktop-like fingerprint (Win/Mac).
func RandomDesktopFingerprint() *Fingerprint {
	chromeVersion := randomChromeVersion()

	isWin := fpRng.Intn(2) == 0
	var uaTemplate string
	var platform string
	if isWin {
		uaTemplate = winUserAgents[fpRng.Intn(len(winUserAgents))]
		platform = "Win32"
	} else {
		uaTemplate = macUserAgents[fpRng.Intn(len(macUserAgents))]
		platform = "MacIntel"
	}

	screen := screenOptions[fpRng.Intn(len(screenOptions))]
	webgl := webglOptions[fpRng.Intn(len(webglOptions))]
	hwc := hwcOptions[fpRng.Intn(len(hwcOptions))]
	mem := deviceMemOptions[fpRng.Intn(len(deviceMemOptions))]

	return &Fingerprint{
		UserAgent:           fmt.Sprintf(uaTemplate, chromeVersion),
		AcceptLanguage:      "zh-CN,zh;q=0.9,en;q=0.6",
		Platform:            platform,
		Timezone:            "Asia/Shanghai",
		ScreenWidth:         screen.W,
		ScreenHeight:        screen.H,
		DeviceScale:         screen.D,
		HardwareConcurrency: hwc,
		DeviceMemory:        mem,
		WebglVendor:         webgl.Vendor,
		WebglRenderer:       webgl.Renderer,
	}
}

func randomChromeVersion() string {
	major := 124 + fpRng.Intn(3) // 124-126
	build := 0
	patch := fpRng.Intn(200)
	return fmt.Sprintf("%d.0.%d.%d", major, build, patch)
}
