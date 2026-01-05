package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/accounts"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/session"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
	"golang.org/x/net/proxy"
)

// respondError 返回错误响应
func respondError(c *gin.Context, statusCode int, code, message string, details any) {
	response := ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	}

	logrus.Errorf("%s %s %s %d", c.Request.Method, c.Request.URL.Path,
		c.GetString("account"), statusCode)

	c.JSON(statusCode, response)
}

// respondSuccess 返回成功响应
func respondSuccess(c *gin.Context, data any, message string) {
	response := SuccessResponse{
		Success: true,
		Data:    data,
		Message: message,
	}

	logrus.Infof("%s %s %s %d", c.Request.Method, c.Request.URL.Path,
		c.GetString("account"), http.StatusOK)

	c.JSON(http.StatusOK, response)
}

func parseAccountID(c *gin.Context) (int, error) {
	if v := c.GetHeader("X-Account-ID"); v != "" {
		return strconv.Atoi(v)
	}
	if v := c.Query("account_id"); v != "" {
		return strconv.Atoi(v)
	}
	return 1, nil // 默认账号
}

func buildProxyConfig(raw, typ, host string, port int, user, pass string) accounts.ProxyConfig {
	cfg := accounts.ProxyConfig{
		Type: typ,
		Host: host,
		Port: port,
		User: user,
		Pass: pass,
		Raw:  raw,
	}
	if cfg.Type == "" && raw != "" {
		if u, err := url.Parse(raw); err == nil && u != nil {
			if u.Scheme != "" {
				cfg.Type = u.Scheme
			}
			if h := u.Hostname(); h != "" {
				cfg.Host = h
			}
			if p := u.Port(); p != "" {
				if n, err := strconv.Atoi(p); err == nil {
					cfg.Port = n
				}
			}
			if u.User != nil {
				cfg.User = u.User.Username()
				if p, ok := u.User.Password(); ok {
					cfg.Pass = p
				}
			}
		}
	}
	return cfg
}

func buildHTTPClient(cfg accounts.ProxyConfig) (*http.Client, error) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ResponseHeaderTimeout = 15 * time.Second
	base.ExpectContinueTimeout = 5 * time.Second
	base.TLSHandshakeTimeout = 10 * time.Second

	if cfg.Type == "" || cfg.Type == "direct" {
		base.Proxy = nil
		return &http.Client{Transport: base, Timeout: 20 * time.Second}, nil
	}

	switch cfg.Type {
	case "http", "https":
		raw := cfg.Raw
		if raw == "" && cfg.Host != "" && cfg.Port > 0 {
			if cfg.User != "" || cfg.Pass != "" {
				raw = fmt.Sprintf("%s://%s:%s@%s:%d", cfg.Type, cfg.User, cfg.Pass, cfg.Host, cfg.Port)
			} else {
				raw = fmt.Sprintf("%s://%s:%d", cfg.Type, cfg.Host, cfg.Port)
			}
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		base.Proxy = http.ProxyURL(u)
		return &http.Client{Transport: base, Timeout: 20 * time.Second}, nil
	case "socks5":
		if cfg.Host == "" || cfg.Port == 0 {
			return nil, fmt.Errorf("socks5 host/port required")
		}
		var auth *proxy.Auth
		if cfg.User != "" || cfg.Pass != "" {
			auth = &proxy.Auth{User: cfg.User, Password: cfg.Pass}
		}
		dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		base.Proxy = nil
		base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			type d interface {
				DialContext(context.Context, string, string) (net.Conn, error)
			}
			if dc, ok := dialer.(d); ok {
				return dc.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
		return &http.Client{Transport: base, Timeout: 20 * time.Second}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy type: %s", cfg.Type)
	}
}

// bindAccountContext 根据请求绑定账号上下文
func (s *AppServer) bindAccountContext(c *gin.Context) (*accounts.Account, context.Context, error) {
	id, err := parseAccountID(c)
	if err != nil {
		return nil, nil, err
	}
	acc, err := s.accounts.Get(id)
	if err != nil && id == 1 {
		acc, err = s.accounts.Create("", "")
	}
	if err != nil {
		return nil, nil, err
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)
	c.Set("account", acc.Key)
	return acc, ctx, nil
}

// startLoginHandler 创建/更新账号并生成登录二维码
func (s *AppServer) startLoginHandler(c *gin.Context) {
	logrus.Infof("start login request received")
	var req StartLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", err.Error())
		return
	}

	var acc *accounts.Account
	var err error

	var proxyVal string
	if req.Proxy != nil {
		proxyVal = *req.Proxy
	}
	pcfg := buildProxyConfig(proxyVal, req.ProxyType, req.ProxyHost, req.ProxyPort, req.ProxyUser, req.ProxyPass)

	if req.AccountID == 0 {
		acc, err = s.accounts.Create(proxyVal, req.Name)
	} else {
		acc, err = s.accounts.Get(req.AccountID)
		if err != nil && req.AccountID == 1 {
			acc, err = s.accounts.Create(proxyVal, req.Name)
		} else if err == nil && (req.Proxy != nil || req.Name != "") {
			acc, err = s.accounts.UpdateProxy(req.AccountID, proxyVal, req.Name)
		}
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_ERROR", "账号处理失败", err.Error())
		return
	}
	if _, err := s.accounts.ApplyProxyConfig(acc.ID, pcfg); err != nil {
		logrus.Warnf("apply proxy config failed: %v", err)
	}

	ctx := session.WithAccount(c.Request.Context(), acc.Key)
	logrus.Infof("begin login flow for account=%s(id=%d)", acc.Key, acc.ID)
	if err := s.xiaohongshuService.LoginAndWait(ctx, 10*time.Minute); err != nil {
		_ = s.accounts.Delete(acc.ID)
		respondError(c, http.StatusInternalServerError, "LOGIN_FAILED", "登录失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{
		"account_id": acc.ID,
		"proxy":      acc.Proxy,
	}, "登录成功")
}

// listAccountsHandler 列出账号信息
func (s *AppServer) listAccountsHandler(c *gin.Context) {
	respondSuccess(c, s.accounts.List(), "获取账号列表成功")
}

// updateProxyHandler 更新账号代理并重新发起登录
func (s *AppServer) updateProxyHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ACCOUNT_ID", "账号ID无效", err.Error())
		return
	}
	var req UpdateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", err.Error())
		return
	}

	pcfg := buildProxyConfig(req.Proxy, req.ProxyType, req.ProxyHost, req.ProxyPort, req.ProxyUser, req.ProxyPass)
	acc, err := s.accounts.UpdateProxy(id, req.Proxy, req.Name)
	if err == nil {
		acc, err = s.accounts.ApplyProxyConfig(id, pcfg)
	}
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}

	ctx := session.WithAccount(c.Request.Context(), acc.Key)
	ctx = session.WithHeadless(ctx, false)
	result, err := s.xiaohongshuService.GetLoginQrcode(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "STATUS_CHECK_FAILED", "获取登录二维码失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{"account_id": acc.ID, "proxy": acc.Proxy, "data": result}, "更新代理并生成二维码成功")
}

// testProxyHandler 测试代理连通性并返回外网IP
func (s *AppServer) testProxyHandler(c *gin.Context) {
	var req ProxyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", err.Error())
		return
	}
	cfg := buildProxyConfig(req.Proxy, req.ProxyType, req.ProxyHost, req.ProxyPort, req.ProxyUser, req.ProxyPass)
	client, err := buildHTTPClient(cfg)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_PROXY", "构建代理失败", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	reqHTTP, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=text", nil)
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROXY_TEST_FAILED", "代理连通失败", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respondError(c, http.StatusBadRequest, "PROXY_TEST_FAILED", "代理返回异常状态", resp.Status)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	ip := string(body)
	respondSuccess(c, gin.H{"ip": ip}, "代理检测成功")
}

// deleteAccountHandler 删除账号及数据
func (s *AppServer) deleteAccountHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ACCOUNT_ID", "账号ID无效", err.Error())
		return
	}
	if err := s.accounts.Delete(id); err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	respondSuccess(c, gin.H{"account_id": id}, "账号已删除")
}

// startAccountWindowHandler 启动可视化浏览器窗口
func (s *AppServer) startAccountWindowHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ACCOUNT_ID", "账号ID无效", err.Error())
		return
	}
	acc, err := s.accounts.Get(id)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)
	ctx = session.WithHeadless(ctx, false)

	if err := s.xiaohongshuService.StartVisibleWindow(ctx); err != nil {
		respondError(c, http.StatusInternalServerError, "START_FAILED", "启动窗口失败", err.Error())
		return
	}
	respondSuccess(c, gin.H{"account_id": acc.ID}, "账号窗口已启动")
}

// startRawWindowHandler 启动最小化可视浏览器（不加载账号数据）
func (s *AppServer) startRawWindowHandler(c *gin.Context) {
	if err := s.xiaohongshuService.StartRawVisibleWindow(context.Background()); err != nil {
		respondError(c, http.StatusInternalServerError, "START_FAILED", "启动浏览器失败", err.Error())
		return
	}
	respondSuccess(c, gin.H{"status": "ok"}, "已启动原生浏览器窗口")
}

// checkLoginStatusHandler 检查登录状态
func (s *AppServer) checkLoginStatusHandler(c *gin.Context) {
	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	status, err := s.xiaohongshuService.CheckLoginStatus(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "STATUS_CHECK_FAILED",
			"检查登录状态失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{"account_id": acc.ID, "status": status}, "检查登录状态成功")
}

// getLoginQrcodeHandler 获取登录二维码
func (s *AppServer) getLoginQrcodeHandler(c *gin.Context) {
	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	result, err := s.xiaohongshuService.GetLoginQrcode(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "STATUS_CHECK_FAILED",
			"获取登录二维码失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{"account_id": acc.ID, "data": result}, "获取登录二维码成功")
}

// deleteCookiesHandler 删除 cookies，重置登录状态
func (s *AppServer) deleteCookiesHandler(c *gin.Context) {
	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	err = s.xiaohongshuService.DeleteCookies(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DELETE_COOKIES_FAILED",
			"删除 cookies 失败", err.Error())
		return
	}

	cookiePath := cookies.GetCookiesFilePathForAccount(acc.Key)
	respondSuccess(c, map[string]interface{}{
		"account_id":  acc.ID,
		"cookie_path": cookiePath,
		"message":     "Cookies 已成功删除，登录状态已重置。下次操作时需要重新登录。",
	}, "删除 cookies 成功")
}

// publishHandler 发布内容
func (s *AppServer) publishHandler(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}

	req.AccountID = acc.ID

	// 执行发布
	result, err := s.xiaohongshuService.PublishContent(ctx, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PUBLISH_FAILED",
			"发布失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{"account_id": acc.ID, "result": result}, "发布成功")
}

// publishVideoHandler 发布视频内容
func (s *AppServer) publishVideoHandler(c *gin.Context) {
	var req PublishVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	req.AccountID = acc.ID

	// 执行视频发布
	result, err := s.xiaohongshuService.PublishVideo(ctx, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PUBLISH_VIDEO_FAILED",
			"视频发布失败", err.Error())
		return
	}

	respondSuccess(c, gin.H{"account_id": acc.ID, "result": result}, "视频发布成功")
}

// listFeedsHandler 获取Feeds列表
func (s *AppServer) listFeedsHandler(c *gin.Context) {
	_, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	// 获取 Feeds 列表
	result, err := s.xiaohongshuService.ListFeeds(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "LIST_FEEDS_FAILED",
			"获取Feeds列表失败", err.Error())
		return
	}

	respondSuccess(c, result, "获取Feeds列表成功")
}

// searchFeedsHandler 搜索Feeds
func (s *AppServer) searchFeedsHandler(c *gin.Context) {
	var keyword string
	var filters xiaohongshu.FilterOption
	var accountID int

	switch c.Request.Method {
	case http.MethodPost:
		// 对于POST请求，从JSON中获取keyword
		var searchReq SearchFeedsRequest
		if err := c.ShouldBindJSON(&searchReq); err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
				"请求参数错误", err.Error())
			return
		}
		keyword = searchReq.Keyword
		filters = searchReq.Filters
		accountID = searchReq.AccountID
	default:
		keyword = c.Query("keyword")
		id, _ := parseAccountID(c)
		accountID = id
	}

	if accountID == 0 {
		accountID = 1
	}

	if keyword == "" {
		respondError(c, http.StatusBadRequest, "MISSING_KEYWORD",
			"缺少关键字参数", "keyword parameter is required")
		return
	}

	acc, err := s.accounts.Get(accountID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)

	// 搜索 Feeds
	result, err := s.xiaohongshuService.SearchFeeds(ctx, keyword, filters)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "SEARCH_FEEDS_FAILED",
			"搜索Feeds失败", err.Error())
		return
	}

	respondSuccess(c, result, "搜索Feeds成功")
}

// getFeedDetailHandler 获取Feed详情
func (s *AppServer) getFeedDetailHandler(c *gin.Context) {
	var req FeedDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	if req.AccountID == 0 {
		req.AccountID = 1
	}
	acc, err := s.accounts.Get(req.AccountID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)

	var result *FeedDetailResponse
	var er error

	if req.CommentConfig != nil {
		// 使用配置参数
		config := xiaohongshu.CommentLoadConfig{
			ClickMoreReplies:    req.CommentConfig.ClickMoreReplies,
			MaxRepliesThreshold: req.CommentConfig.MaxRepliesThreshold,
			MaxCommentItems:     req.CommentConfig.MaxCommentItems,
			ScrollSpeed:         req.CommentConfig.ScrollSpeed,
		}
		result, er = s.xiaohongshuService.GetFeedDetailWithConfig(ctx, req.FeedID, req.XsecToken, req.LoadAllComments, config)
	} else {
		// 使用默认配置
		result, er = s.xiaohongshuService.GetFeedDetail(ctx, req.FeedID, req.XsecToken, req.LoadAllComments)
	}

	if er != nil {
		respondError(c, http.StatusInternalServerError, "GET_FEED_DETAIL_FAILED",
			"获取Feed详情失败", er.Error())
		return
	}

	respondSuccess(c, result, "获取Feed详情成功")
}

// userProfileHandler 用户主页
func (s *AppServer) userProfileHandler(c *gin.Context) {
	var req UserProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	if req.AccountID == 0 {
		req.AccountID = 1
	}
	acc, err := s.accounts.Get(req.AccountID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)

	// 获取用户信息
	result, err := s.xiaohongshuService.UserProfile(ctx, req.UserID, req.XsecToken)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "GET_USER_PROFILE_FAILED",
			"获取用户主页失败", err.Error())
		return
	}

	respondSuccess(c, map[string]any{"data": result}, "获取用户主页成功")
}

// postCommentHandler 发表评论到Feed
func (s *AppServer) postCommentHandler(c *gin.Context) {
	var req PostCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	if req.AccountID == 0 {
		req.AccountID = 1
	}
	acc, err := s.accounts.Get(req.AccountID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)

	// 发表评论
	result, err := s.xiaohongshuService.PostCommentToFeed(ctx, req.FeedID, req.XsecToken, req.Content)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "POST_COMMENT_FAILED",
			"发表评论失败", err.Error())
		return
	}

	respondSuccess(c, result, result.Message)
}

// replyCommentHandler 回复指定评论
func (s *AppServer) replyCommentHandler(c *gin.Context) {
	var req ReplyCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_REQUEST",
			"请求参数错误", err.Error())
		return
	}

	if req.AccountID == 0 {
		req.AccountID = 1
	}
	acc, err := s.accounts.Get(req.AccountID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}
	ctx := session.WithAccount(c.Request.Context(), acc.Key)

	result, err := s.xiaohongshuService.ReplyCommentToFeed(ctx, req.FeedID, req.XsecToken, req.CommentID, req.UserID, req.Content)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "REPLY_COMMENT_FAILED",
			"回复评论失败", err.Error())
		return
	}

	respondSuccess(c, result, result.Message)
}

// healthHandler 健康检查
func healthHandler(c *gin.Context) {
	respondSuccess(c, map[string]any{
		"status":    "healthy",
		"service":   "xiaohongshu-mcp",
		"account":   "ai-report",
		"timestamp": "now",
	}, "服务正常")
}

// myProfileHandler 我的信息
func (s *AppServer) myProfileHandler(c *gin.Context) {
	acc, ctx, err := s.bindAccountContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "ACCOUNT_NOT_FOUND", "账号不存在", err.Error())
		return
	}

	// 获取当前登录用户信息
	result, err := s.xiaohongshuService.GetMyProfile(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "GET_MY_PROFILE_FAILED",
			"获取我的主页失败", err.Error())
		return
	}

	respondSuccess(c, map[string]any{"account_id": acc.ID, "data": result}, "获取我的主页成功")
}

// handleHealthCheck 健康检查端点
func (s *AppServer) handleHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"status":    "healthy",
			"service":   "xiaohongshu-mcp",
			"timestamp": time.Now().Unix(),
		},
		"message": "服务正常",
	})
}
