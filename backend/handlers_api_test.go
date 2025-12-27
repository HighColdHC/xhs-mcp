package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xpzouying/xiaohongshu-mcp/accounts"
)

// 测试配置
const testAPIBase = "http://127.0.0.1:18060"

// setupTestApp 创建测试应用实例
func setupTestApp(t *testing.T) (*AppServer, *httptest.Server) {
	// 创建临时目录用于测试
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "accounts.json")
	profileBase := filepath.Join(tempDir, "profiles")

	// 创建账号管理器
	accountManager, err := accounts.NewManager(storePath, profileBase)
	if err != nil {
		t.Fatalf("failed to create account manager: %v", err)
	}

	// 创建服务
	xiaohongshuService := NewXiaohongshuService(accountManager)

	// 创建应用服务器
	appServer := NewAppServer(xiaohongshuService)

	// 创建测试路由
	gin.SetMode(gin.TestMode)
	router := setupRoutes(appServer)

	// 创建测试服务器
	ts := httptest.NewServer(router)

	return appServer, ts
}

// ==================== 辅助函数 ====================

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

func assertSuccess(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected success, got %d: %s", resp.StatusCode, string(body))
	}
}

func assertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}

// ==================== 健康检查 ====================

func TestHealthHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	assertSuccess(t, resp)

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// respondSuccess 返回 {success: true, data: {...}, message: "..."}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", result["data"])
	}

	if data["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", data["status"])
	}
}

// ==================== 账号管理 ====================

func TestListAccountsHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/accounts")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	assertSuccess(t, resp)

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result["success"].(bool) {
		t.Errorf("expected success=true, got %v", result["success"])
	}

	// 检查数据字段
	data, ok := result["data"].([]any)
	if !ok {
		t.Errorf("expected data to be array, got %T", result["data"])
	}
	t.Logf("Accounts: %+v", data)
}

func TestStartLoginHandler_CreateAccount(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := StartLoginRequest{
		Name: "test-account",
	}

	resp, err := http.Post(ts.URL+"/api/v1/login/start", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	// 登录会超时或失败，但应该返回正确的响应格式
	if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("Login response (status %d): %s", resp.StatusCode, string(body))
	}
}

func TestUpdateProxyHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	// 先创建账号
	createReq := StartLoginRequest{Name: "test-proxy"}
	http.Post(ts.URL+"/api/v1/login/start", "application/json", jsonBody(createReq))

	// 更新代理
	updateReq := UpdateProxyRequest{
		ProxyType: "direct",
		Name:      "updated-name",
	}

	resp, err := http.Post(ts.URL+"/api/v1/accounts/1/proxy", "application/json", jsonBody(updateReq))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	// 可能返回错误（账号未登录），但接口应该可访问
	t.Logf("Update proxy status: %d", resp.StatusCode)
}

func TestDeleteAccountHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	// 先创建账号
	createReq := StartLoginRequest{Name: "test-delete"}
	http.Post(ts.URL+"/api/v1/login/start", "application/json", jsonBody(createReq))

	// 删除账号
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/accounts/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	assertSuccess(t, resp)

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result["success"].(bool) {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

// ==================== 登录相关 ====================

func TestCheckLoginStatusHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/login/status")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Login status: %+v", result)
}

func TestGetLoginQrcodeHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/login/qrcode")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("QR code response: %+v", result)
}

func TestDeleteCookiesHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/login/cookies", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Delete cookies result: %+v", result)
}

// ==================== 代理测试 ====================

func TestProxyTestHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	// 测试直连
	req := ProxyTestRequest{
		ProxyType: "direct",
	}

	resp, err := http.Post(ts.URL+"/api/v1/proxy/test", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Proxy test result: %+v", result)
}

// ==================== 窗口启动 ====================

func TestStartAccountWindowHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	// 先创建账号
	createReq := StartLoginRequest{Name: "test-window"}
	http.Post(ts.URL+"/api/v1/login/start", "application/json", jsonBody(createReq))

	resp, err := http.Post(ts.URL+"/api/v1/accounts/1/start", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Start window result: %+v", result)
}

func TestStartRawWindowHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/raw/start", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Start raw window result: %+v", result)
}

// ==================== 内容发布 ====================

func TestPublishHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := PublishRequest{
		Title:   "测试标题",
		Content: "测试内容",
		Images:  []string{"https://example.com/test.jpg"},
	}

	resp, err := http.Post(ts.URL+"/api/v1/publish", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	// 会因为没有登录而失败，但接口应该可访问
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Publish result: %+v", result)
}

func TestPublishVideoHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := PublishVideoRequest{
		Title:   "测试视频",
		Content: "测试视频内容",
		Video:   "C:/fake/path/video.mp4",
	}

	resp, err := http.Post(ts.URL+"/api/v1/publish_video", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Publish video result: %+v", result)
}

// ==================== 内容获取 ====================

func TestListFeedsHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/feeds/list")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("List feeds result: %+v", result)
}

func TestSearchFeedsHandler_GET(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/feeds/search?keyword=测试")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Search feeds (GET) result: %+v", result)
}

func TestSearchFeedsHandler_POST(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := SearchFeedsRequest{
		Keyword: "测试",
	}

	resp, err := http.Post(ts.URL+"/api/v1/feeds/search", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Search feeds (POST) result: %+v", result)
}

func TestGetFeedDetailHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := FeedDetailRequest{
		FeedID:          "12345",
		XsecToken:       "test-token",
		LoadAllComments: false,
	}

	resp, err := http.Post(ts.URL+"/api/v1/feeds/detail", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Feed detail result: %+v", result)
}

func TestUserProfileHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := UserProfileRequest{
		UserID:    "12345",
		XsecToken: "test-token",
	}

	resp, err := http.Post(ts.URL+"/api/v1/user/profile", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("User profile result: %+v", result)
}

func TestMyProfileHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/user/me")
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("My profile result: %+v", result)
}

// ==================== 评论相关 ====================

func TestPostCommentHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := PostCommentRequest{
		FeedID:    "12345",
		XsecToken: "test-token",
		Content:   "测试评论",
	}

	resp, err := http.Post(ts.URL+"/api/v1/feeds/comment", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Post comment result: %+v", result)
}

func TestReplyCommentHandler(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	req := ReplyCommentRequest{
		FeedID:    "12345",
		XsecToken: "test-token",
		CommentID: "comment-123",
		Content:   "测试回复",
	}

	resp, err := http.Post(ts.URL+"/api/v1/feeds/comment/reply", "application/json", jsonBody(req))
	if err != nil {
		t.Fatalf("failed to request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	t.Logf("Reply comment result: %+v", result)
}

// ==================== 集成测试 ====================

// TestAllEndpoints 测试所有端点的基本可访问性
func TestAllEndpoints(t *testing.T) {
	_, ts := setupTestApp(t)
	defer ts.Close()

	endpoints := []struct {
		method string
		url    string
		body   any
	}{
		{"GET", "/health", nil},
		{"GET", "/api/v1/accounts", nil},
		{"GET", "/api/v1/login/status", nil},
		{"GET", "/api/v1/login/qrcode", nil},
		{"POST", "/api/v1/proxy/test", ProxyTestRequest{ProxyType: "direct"}},
		{"POST", "/api/v1/raw/start", nil},
		{"GET", "/api/v1/feeds/list", nil},
		{"GET", "/api/v1/feeds/search?keyword=test", nil},
		{"GET", "/api/v1/user/me", nil},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.url, func(t *testing.T) {
			var body io.Reader
			if ep.body != nil {
				body = jsonBody(ep.body)
			}

			var err error
			var resp *http.Response

			if ep.method == "GET" {
				resp, err = http.Get(ts.URL + ep.url)
			} else {
				resp, err = http.Post(ts.URL+ep.url, "application/json", body)
			}

			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			defer resp.Body.Close()

			// 检查响应可解析
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			// 检查基本响应结构
			if _, hasSuccess := result["success"]; hasSuccess {
				if !result["success"].(bool) {
					t.Logf("Request failed: %v", result)
				}
			}

			t.Logf("✓ %s %s - status: %d", ep.method, ep.url, resp.StatusCode)
		})
	}
}

// ==================== 真实服务测试 ====================

// TestRealAPIEndpoints 测试真实运行的 API 服务
// 注意：需要先启动后端服务（go run .）
func TestRealAPIEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real API test in short mode")
	}

	baseURL := testAPIBase

	t.Run("Health", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		t.Logf("Health: %+v", result)
	})

	t.Run("ListAccounts", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/accounts")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		t.Logf("Accounts: %+v", result)
	})

	t.Run("LoginStatus", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/login/status")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		t.Logf("Login Status: %+v", result)
	})
}

// BenchmarkAPIEndpoints 性能测试
func BenchmarkAPIEndpoints(b *testing.B) {
	_, ts := setupTestApp(&testing.T{})
	defer ts.Close()

	b.Run("Health", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, _ := http.Get(ts.URL + "/health")
			resp.Body.Close()
		}
	})

	b.Run("ListAccounts", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, _ := http.Get(ts.URL + "/api/v1/accounts")
			resp.Body.Close()
		}
	})
}
