package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/mattn/go-runewidth"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/accounts"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/pkg/downloader"
	"github.com/xpzouying/xiaohongshu-mcp/session"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// XiaohongshuService 小红书业务服务
type XiaohongshuService struct {
	accounts     *accounts.Manager
	liveBrowsers []*browser.Browser
	liveByAccount map[string]*browser.Browser
	liveMu       sync.Mutex
}

// NewXiaohongshuService 创建小红书服务实例
func NewXiaohongshuService(am *accounts.Manager) *XiaohongshuService {
	return &XiaohongshuService{
		accounts:     am,
		liveBrowsers: make([]*browser.Browser, 0),
		liveByAccount: make(map[string]*browser.Browser),
	}
}

func (s *XiaohongshuService) getLiveBrowser(accountKey string) *browser.Browser {
	if accountKey == "" {
		return nil
	}
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return s.liveByAccount[accountKey]
}

func (s *XiaohongshuService) setLiveBrowser(accountKey string, b *browser.Browser) {
	if accountKey == "" || b == nil {
		return
	}
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if old := s.liveByAccount[accountKey]; old != nil && old != b {
		old.Close()
	}
	s.liveByAccount[accountKey] = b
}

func (s *XiaohongshuService) getAccountBrowser(ctx context.Context) (*browser.Browser, func(), error) {
	accountKey := session.Account(ctx)
	if live := s.getLiveBrowser(accountKey); live != nil {
		return live, func() {}, nil
	}
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, nil, err
	}
	return b, func() { b.Close() }, nil
}

// PublishRequest 发布请求
type PublishRequest struct {
	AccountID int      `json:"account_id,omitempty"`
	Title     string   `json:"title" binding:"required"`
	Content   string   `json:"content" binding:"required"`
	Images    []string `json:"images" binding:"required,min=1"`
	Tags      []string `json:"tags,omitempty"`
}

// LoginStatusResponse 登录状态响应
type LoginStatusResponse struct {
	IsLoggedIn bool   `json:"is_logged_in"`
	Username   string `json:"username,omitempty"`
}

// LoginQrcodeResponse 登录扫码二维码
type LoginQrcodeResponse struct {
	Timeout    string `json:"timeout"`
	IsLoggedIn bool   `json:"is_logged_in"`
	Img        string `json:"img,omitempty"`
}

// PublishResponse 发布响应
type PublishResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Images  int    `json:"images"`
	Status  string `json:"status"`
	PostID  string `json:"post_id,omitempty"`
}

// PublishVideoRequest 发布视频请求（仅支持本地单个视频文件）
type PublishVideoRequest struct {
	AccountID int      `json:"account_id,omitempty"`
	Title     string   `json:"title" binding:"required"`
	Content   string   `json:"content" binding:"required"`
	Video     string   `json:"video" binding:"required"`
	Tags      []string `json:"tags,omitempty"`
}

// PublishVideoResponse 发布视频响应
type PublishVideoResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Video   string `json:"video"`
	Status  string `json:"status"`
	PostID  string `json:"post_id,omitempty"`
}

// FeedsListResponse Feeds列表响应
type FeedsListResponse struct {
	Feeds []xiaohongshu.Feed `json:"feeds"`
	Count int                `json:"count"`
}

// UserProfileResponse 用户主页响应
type UserProfileResponse struct {
	UserBasicInfo xiaohongshu.UserBasicInfo      `json:"userBasicInfo"`
	Interactions  []xiaohongshu.UserInteractions `json:"interactions"`
	Feeds         []xiaohongshu.Feed             `json:"feeds"`
}

// DeleteCookies 删除 cookies 文件，用于登录重置
func (s *XiaohongshuService) DeleteCookies(ctx context.Context) error {
	cookiePath := cookies.GetCookiesFilePathForAccount(session.Account(ctx))
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	return cookieLoader.DeleteCookies()
}

// CheckLoginStatus 检查登录状态
func (s *XiaohongshuService) CheckLoginStatus(ctx context.Context) (*LoginStatusResponse, error) {
	b, closeBrowser, err := s.getAccountBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer closeBrowser()

	page := b.NewPage()
	defer func() { _ = page.Close() }()

	loginAction := xiaohongshu.NewLogin(page)

	isLoggedIn, err := loginAction.CheckLoginStatus(ctx)
	if err != nil {
		return nil, err
	}
	if isLoggedIn {
		if err := s.saveCookies(ctx, page); err != nil {
			logrus.Warnf("failed to save cookies after login status ok: %v", err)
		}
	}

	response := &LoginStatusResponse{
		IsLoggedIn: isLoggedIn,
		Username:   configs.Username,
	}

	return response, nil
}

// GetLoginQrcode 获取登录的扫码二维码
func (s *XiaohongshuService) GetLoginQrcode(ctx context.Context) (*LoginQrcodeResponse, error) {
	logrus.Info("GetLoginQrcode: resolve account")
	if _, err := s.resolveAccount(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	logrus.Info("GetLoginQrcode: creating browser")
	b, err := s.newBrowser(ctx)
	if err != nil {
		logrus.Errorf("GetLoginQrcode: newBrowser failed: %v", err)
		return nil, err
	}

	logrus.Info("GetLoginQrcode: creating page")
	page := b.NewPage()

	deferFunc := func() {
		_ = page.Close()
		b.Close()
	}

	loginAction := xiaohongshu.NewLogin(page)
	logrus.Info("GetLoginQrcode: fetching QR code")
	img, loggedIn, err := loginAction.FetchQrcodeImage(ctx)
	if err != nil || loggedIn {
		defer deferFunc()
	}
	if err != nil {
		logrus.Errorf("GetLoginQrcode: FetchQrcodeImage error: %v", err)
		return nil, err
	}

	timeout := 4 * time.Minute

	if !loggedIn {
		go func() {
			ctxWithAccount := session.WithAccount(context.Background(), session.Account(ctx))
			ctxTimeout, cancel := context.WithTimeout(ctxWithAccount, timeout)
			defer cancel()
			defer deferFunc()

			if loginAction.WaitForLogin(ctxTimeout) {
				if er := s.saveCookies(ctxWithAccount, page); er != nil {
					logrus.Errorf("failed to save cookies: %v", er)
				}
			}
		}()
	}

	return &LoginQrcodeResponse{
		Timeout: func() string {
			if loggedIn {
				return "0s"
			}
			return timeout.String()
		}(),
		Img:        img,
		IsLoggedIn: loggedIn,
	}, nil
}

// LoginAndWait 启动可视化登录并等待扫码完成，完成后保存 cookies 并关闭浏览器。
func (s *XiaohongshuService) LoginAndWait(ctx context.Context, timeout time.Duration) error {
	ctx = session.WithHeadless(ctx, false)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	page := b.NewPage()
	defer func() {
		_ = page.Close()
		b.Close()
	}()

	if err := rod.Try(func() {
		_ = page.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	}); err != nil {
		return err
	}

	loginAction := xiaohongshu.NewLogin(page)
	if !loginAction.WaitForLogin(ctx) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("login cancelled")
	}

	return s.saveCookies(ctx, page)
}

// StartVisibleWindow 启动可视化窗口供人工操作
func (s *XiaohongshuService) StartVisibleWindow(ctx context.Context) error {
	accountKey := session.Account(ctx)
	bg := session.WithAccount(context.Background(), accountKey)
	bg = session.WithHeadless(bg, false)

	b, err := s.newBrowser(bg)
	if err != nil {
		return err
	}
	page := b.NewPage()

	// 保存引用防止被回收提前关闭
	s.setLiveBrowser(accountKey, b)
	s.liveMu.Lock()
	s.liveBrowsers = append(s.liveBrowsers, b)
	s.liveMu.Unlock()

	go func() {
		if err := page.Navigate("https://www.xiaohongshu.com/explore"); err != nil {
			logrus.Warnf("visible window navigate failed: %v", err)
			return
		}
		_ = page.WaitLoad()

		loginAction := xiaohongshu.NewLogin(page)
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if loginAction.WaitForLogin(ctxTimeout) {
			ctxWithAccount := session.WithAccount(context.Background(), accountKey)
			if err := s.saveCookies(ctxWithAccount, page); err != nil {
				logrus.Warnf("save cookies after manual login failed: %v", err)
			}
		}
	}()

	logrus.Infof("started visible browser window for account %s", accountKey)
	return nil
}

// StartRawVisibleWindow 启动最小化可视浏览器（不注入指纹、不加载账号数据）
func (s *XiaohongshuService) StartRawVisibleWindow(ctx context.Context) error {
	cfg := browser.Config{
		Headless: false,
		BinPath:  configs.GetBinPath(),
		Trace:    false,
	}
	b, err := browser.New(cfg)
	if err != nil {
		return err
	}
	page := b.NewPage()
	_ = page.MustNavigate("about:blank").WaitLoad()

	s.liveMu.Lock()
	s.liveBrowsers = append(s.liveBrowsers, b)
	s.liveMu.Unlock()

	logrus.Info("started raw visible browser window")
	return nil
}

// PublishContent 发布内容
func (s *XiaohongshuService) PublishContent(ctx context.Context, req *PublishRequest) (*PublishResponse, error) {
	// 验证标题长度
	// 小红书限制：最大40个单位长度
	// 中文/日文/韩文占2个单位，英文/数字占1个单位
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	// 处理图片：下载URL图片或使用本地路径
	imagePaths, err := s.processImages(req.Images)
	if err != nil {
		return nil, err
	}

	// 构建发布内容
	content := xiaohongshu.PublishImageContent{
		Title:      req.Title,
		Content:    req.Content,
		Tags:       req.Tags,
		ImagePaths: imagePaths,
	}

	// 执行发布
	if err := s.publishContent(ctx, content); err != nil {
		logrus.Errorf("发布内容失败: title=%s %v", content.Title, err)
		return nil, err
	}

	response := &PublishResponse{
		Title:   req.Title,
		Content: req.Content,
		Images:  len(imagePaths),
		Status:  "发布完成",
	}

	return response, nil
}

// SaveDraftContent 保存图文草稿（流程一致，最后点击“暂时离开”）
func (s *XiaohongshuService) SaveDraftContent(ctx context.Context, req *PublishRequest) (*PublishResponse, error) {
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	imagePaths, err := s.processImages(req.Images)
	if err != nil {
		return nil, err
	}

	content := xiaohongshu.PublishImageContent{
		Title:      req.Title,
		Content:    req.Content,
		Tags:       req.Tags,
		ImagePaths: imagePaths,
	}

	if err := s.saveDraftContent(ctx, content); err != nil {
		logrus.Errorf("保存草稿失败: title=%s %v", content.Title, err)
		return nil, err
	}

	return &PublishResponse{
		Title:   req.Title,
		Content: req.Content,
		Images:  len(imagePaths),
		Status:  "草稿已保存",
	}, nil
}

// processImages 处理图片列表，支持URL下载和本地路径
func (s *XiaohongshuService) processImages(images []string) ([]string, error) {
	processor := downloader.NewImageProcessor()
	return processor.ProcessImages(images)
}

// publishContent 执行内容发布
func (s *XiaohongshuService) publishContent(ctx context.Context, content xiaohongshu.PublishImageContent) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishImageAction(page)
	if err != nil {
		return err
	}

	// 执行发布
	return action.Publish(ctx, content)
}

func (s *XiaohongshuService) saveDraftContent(ctx context.Context, content xiaohongshu.PublishImageContent) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishImageAction(page)
	if err != nil {
		return err
	}

	return action.SaveDraft(ctx, content)
}

// PublishContentScheduled 定时发布图文（默认当前时间+3天，精确到分钟）
func (s *XiaohongshuService) PublishContentScheduled(ctx context.Context, req *PublishRequest) (*PublishResponse, error) {
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	imagePaths, err := s.processImages(req.Images)
	if err != nil {
		return nil, err
	}

	content := xiaohongshu.PublishImageContent{
		Title:      req.Title,
		Content:    req.Content,
		Tags:       req.Tags,
		ImagePaths: imagePaths,
	}

	when := time.Now().Add(72 * time.Hour).Truncate(time.Minute)
	if err := s.publishContentScheduled(ctx, content, when); err != nil {
		logrus.Errorf("定时发布失败: title=%s %v", content.Title, err)
		return nil, err
	}

	return &PublishResponse{
		Title:   req.Title,
		Content: req.Content,
		Images:  len(imagePaths),
		Status:  "定时发布已设置",
		PostID:  when.Format("2006-01-02 15:04"),
	}, nil
}

func (s *XiaohongshuService) publishContentScheduled(ctx context.Context, content xiaohongshu.PublishImageContent, when time.Time) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishImageAction(page)
	if err != nil {
		return err
	}

	return action.PublishScheduled(ctx, content, when)
}

// PublishVideo 发布视频（本地文件）
func (s *XiaohongshuService) PublishVideo(ctx context.Context, req *PublishVideoRequest) (*PublishVideoResponse, error) {
	// 标题长度校验
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	// 本地视频文件校验
	if req.Video == "" {
		return nil, fmt.Errorf("必须提供本地视频文件")
	}
	if _, err := os.Stat(req.Video); err != nil {
		return nil, fmt.Errorf("视频文件不存在或不可访问: %v", err)
	}

	// 构建发布内容
	content := xiaohongshu.PublishVideoContent{
		Title:     req.Title,
		Content:   req.Content,
		Tags:      req.Tags,
		VideoPath: req.Video,
	}

	// 执行发布
	if err := s.publishVideo(ctx, content); err != nil {
		return nil, err
	}

	resp := &PublishVideoResponse{
		Title:   req.Title,
		Content: req.Content,
		Video:   req.Video,
		Status:  "发布完成",
	}
	return resp, nil
}

// SaveDraftVideo 保存视频草稿（流程一致，最后点击“暂时离开”）
func (s *XiaohongshuService) SaveDraftVideo(ctx context.Context, req *PublishVideoRequest) (*PublishVideoResponse, error) {
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	if req.Video == "" {
		return nil, fmt.Errorf("必须提供本地视频文件")
	}
	if _, err := os.Stat(req.Video); err != nil {
		return nil, fmt.Errorf("视频文件不存在或不可访问: %v", err)
	}

	content := xiaohongshu.PublishVideoContent{
		Title:     req.Title,
		Content:   req.Content,
		Tags:      req.Tags,
		VideoPath: req.Video,
	}

	if err := s.saveDraftVideo(ctx, content); err != nil {
		return nil, err
	}

	resp := &PublishVideoResponse{
		Title:   req.Title,
		Content: req.Content,
		Video:   req.Video,
		Status:  "草稿已保存",
	}
	return resp, nil
}

// publishVideo 执行视频发布
func (s *XiaohongshuService) publishVideo(ctx context.Context, content xiaohongshu.PublishVideoContent) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishVideoAction(page)
	if err != nil {
		return err
	}

	return action.PublishVideo(ctx, content)
}

func (s *XiaohongshuService) saveDraftVideo(ctx context.Context, content xiaohongshu.PublishVideoContent) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishVideoAction(page)
	if err != nil {
		return err
	}

	return action.SaveDraftVideo(ctx, content)
}

// renderIPAndFingerprint 在当前 page 展示 IP 与指纹信息
func renderIPAndFingerprint(page *rod.Page, fp *session.Fingerprint) error {
	if fp == nil {
		return nil
	}
	html := fmt.Sprintf(`<html><body>
<pre id="info" style="font-family:monospace;padding:12px;white-space:pre-wrap;">加载中...</pre>
<script>
const fp = %s;
(async () => {
  const info = document.getElementById('info');
  try {
    const res = await fetch('https://api.ipify.org?format=json');
    const data = await res.json();
    info.textContent = 'IP: ' + data.ip + '\\nFingerprint: ' + JSON.stringify(fp, null, 2);
  } catch (e) {
    info.textContent = 'IP 获取失败: ' + e;
  }
})();
</script>
</body></html>`, toJSON(fp))
	return page.SetDocumentContent(html)
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// PublishVideoScheduled 定时发布视频（默认当前时间+3天）
func (s *XiaohongshuService) PublishVideoScheduled(ctx context.Context, req *PublishVideoRequest) (*PublishVideoResponse, error) {
	if titleWidth := runewidth.StringWidth(req.Title); titleWidth > 40 {
		return nil, fmt.Errorf("标题长度超过限制")
	}

	if req.Video == "" {
		return nil, fmt.Errorf("必须提供本地视频文件")
	}
	if _, err := os.Stat(req.Video); err != nil {
		return nil, fmt.Errorf("视频文件不存在或不可访问: %v", err)
	}

	content := xiaohongshu.PublishVideoContent{
		Title:     req.Title,
		Content:   req.Content,
		Tags:      req.Tags,
		VideoPath: req.Video,
	}

	when := time.Now().Add(72 * time.Hour).Truncate(time.Minute)
	if err := s.publishVideoScheduled(ctx, content, when); err != nil {
		return nil, err
	}

	resp := &PublishVideoResponse{
		Title:   req.Title,
		Content: req.Content,
		Video:   req.Video,
		Status:  "定时发布已设置",
		PostID:  when.Format("2006-01-02 15:04"),
	}
	return resp, nil
}

func (s *XiaohongshuService) publishVideoScheduled(ctx context.Context, content xiaohongshu.PublishVideoContent, when time.Time) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action, err := xiaohongshu.NewPublishVideoAction(page)
	if err != nil {
		return err
	}

	return action.PublishVideoScheduled(ctx, content, when)
}

// ListFeeds 获取Feeds列表
func (s *XiaohongshuService) ListFeeds(ctx context.Context) (*FeedsListResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 创建 Feeds 列表 action
	action := xiaohongshu.NewFeedsListAction(page)

	// 获取 Feeds 列表
	feeds, err := action.GetFeedsList(ctx)
	if err != nil {
		logrus.Errorf("获取 Feeds 列表失败: %v", err)
		return nil, err
	}

	response := &FeedsListResponse{
		Feeds: feeds,
		Count: len(feeds),
	}

	return response, nil
}

func (s *XiaohongshuService) SearchFeeds(ctx context.Context, keyword string, filters ...xiaohongshu.FilterOption) (*FeedsListResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewSearchAction(page)

	feeds, err := action.Search(ctx, keyword, filters...)
	if err != nil {
		return nil, err
	}

	response := &FeedsListResponse{
		Feeds: feeds,
		Count: len(feeds),
	}

	return response, nil
}

// GetFeedDetail 获取Feed详情
func (s *XiaohongshuService) GetFeedDetail(ctx context.Context, feedID, xsecToken string, loadAllComments bool) (*FeedDetailResponse, error) {
	return s.GetFeedDetailWithConfig(ctx, feedID, xsecToken, loadAllComments, xiaohongshu.DefaultCommentLoadConfig())
}

// GetFeedDetailWithConfig 使用配置获取Feed详情
func (s *XiaohongshuService) GetFeedDetailWithConfig(ctx context.Context, feedID, xsecToken string, loadAllComments bool, config xiaohongshu.CommentLoadConfig) (*FeedDetailResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	// 创建 Feed 详情 action
	action := xiaohongshu.NewFeedDetailAction(page)

	// 获取 Feed 详情
	result, err := action.GetFeedDetailWithConfig(ctx, feedID, xsecToken, loadAllComments, config)
	if err != nil {
		return nil, err
	}

	response := &FeedDetailResponse{
		FeedID: feedID,
		Data:   result,
	}

	return response, nil
}

// UserProfile 获取用户信息
func (s *XiaohongshuService) UserProfile(ctx context.Context, userID, xsecToken string) (*UserProfileResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewUserProfileAction(page)

	result, err := action.UserProfile(ctx, userID, xsecToken)
	if err != nil {
		return nil, err
	}
	response := &UserProfileResponse{
		UserBasicInfo: result.UserBasicInfo,
		Interactions:  result.Interactions,
		Feeds:         result.Feeds,
	}

	return response, nil

}

// PostCommentToFeed 发表评论到Feed
func (s *XiaohongshuService) PostCommentToFeed(ctx context.Context, feedID, xsecToken, content string) (*PostCommentResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewCommentFeedAction(page)

	if err := action.PostComment(ctx, feedID, xsecToken, content); err != nil {
		return nil, err
	}

	return &PostCommentResponse{FeedID: feedID, Success: true, Message: "评论发表成功"}, nil
}

// LikeFeed 点赞笔记
func (s *XiaohongshuService) LikeFeed(ctx context.Context, feedID, xsecToken string) (*ActionResult, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLikeAction(page)
	if err := action.Like(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "点赞成功或已点赞"}, nil
}

// UnlikeFeed 取消点赞笔记
func (s *XiaohongshuService) UnlikeFeed(ctx context.Context, feedID, xsecToken string) (*ActionResult, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLikeAction(page)
	if err := action.Unlike(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "取消点赞成功或未点赞"}, nil
}

// FavoriteFeed 收藏笔记
func (s *XiaohongshuService) FavoriteFeed(ctx context.Context, feedID, xsecToken string) (*ActionResult, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewFavoriteAction(page)
	if err := action.Favorite(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "收藏成功或已收藏"}, nil
}

// UnfavoriteFeed 取消收藏笔记
func (s *XiaohongshuService) UnfavoriteFeed(ctx context.Context, feedID, xsecToken string) (*ActionResult, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewFavoriteAction(page)
	if err := action.Unfavorite(ctx, feedID, xsecToken); err != nil {
		return nil, err
	}
	return &ActionResult{FeedID: feedID, Success: true, Message: "取消收藏成功或未收藏"}, nil
}

// ReplyCommentToFeed 回复指定评论
func (s *XiaohongshuService) ReplyCommentToFeed(ctx context.Context, feedID, xsecToken, commentID, userID, content string) (*ReplyCommentResponse, error) {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewCommentFeedAction(page)

	if err := action.ReplyToComment(ctx, feedID, xsecToken, commentID, userID, content); err != nil {
		return nil, err
	}

	return &ReplyCommentResponse{
		FeedID:          feedID,
		TargetCommentID: commentID,
		TargetUserID:    userID,
		Success:         true,
		Message:         "评论回复成功",
	}, nil
}

func (s *XiaohongshuService) newBrowser(ctx context.Context) (*browser.Browser, error) {
	acc, err := s.resolveAccount(ctx)
	if err != nil {
		return nil, err
	}

	cfg := browser.Config{
		Context: func() context.Context {
			if ctx != nil {
				return ctx
			}
			return context.Background()
		}(),
		Headless: func() bool {
			if h := session.HeadlessOverride(ctx); h != nil {
				return *h
			}
			return configs.IsHeadless()
		}(),
		BinPath:     configs.GetBinPath(),
		Proxy:       acc.Proxy,
		ProxyType:   acc.ProxyType,
		ProxyHost:   acc.ProxyHost,
		ProxyPort:   acc.ProxyPort,
		ProxyUser:   acc.ProxyUser,
		ProxyPass:   acc.ProxyPass,
		UserAgent:   acc.Fingerprint.UserAgent,
		CookiePath:  acc.CookiePath,
		UserDataDir: acc.ProfilePath,
		Fingerprint: acc.Fingerprint,
	}

	return browser.New(cfg)
}

func (s *XiaohongshuService) resolveAccount(ctx context.Context) (*accounts.Account, error) {
	key := session.Account(ctx)
	if key == "" || key == "default" {
		// default to account 1
		acc, err := s.accounts.Get(1)
		if err == nil {
			return acc, nil
		}
		return s.accounts.Create("", "")
	}
	acc, err := s.accounts.GetByKey(key)
	if err == nil {
		return acc, nil
	}
	// if not found, try parse acc_<id>
	return s.accounts.Create("", "")
}

func (s *XiaohongshuService) saveCookies(ctx context.Context, page *rod.Page) error {
	cks, err := page.Browser().GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cks)
	if err != nil {
		return err
	}

	cookiePath := cookies.GetCookiesFilePathForAccount(session.Account(ctx))
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	if err := cookieLoader.SaveCookies(data); err != nil {
		return err
	}
	s.accounts.MarkLoggedIn(session.Account(ctx))
	return nil
}

// withBrowserPage 执行需要浏览器页面的操作的通用函数
func (s *XiaohongshuService) withBrowserPage(ctx context.Context, fn func(*rod.Page) error) error {
	b, err := s.newBrowser(ctx)
	if err != nil {
		return err
	}
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	return fn(page)
}

// GetMyProfile 获取当前登录用户的个人信息
func (s *XiaohongshuService) GetMyProfile(ctx context.Context) (*UserProfileResponse, error) {
	var result *xiaohongshu.UserProfileResponse
	var err error

	err = s.withBrowserPage(ctx, func(page *rod.Page) error {
		action := xiaohongshu.NewUserProfileAction(page)
		result, err = action.GetMyProfileViaSidebar(ctx)
		return err
	})

	if err != nil {
		return nil, err
	}

	response := &UserProfileResponse{
		UserBasicInfo: result.UserBasicInfo,
		Interactions:  result.Interactions,
		Feeds:         result.Feeds,
	}

	return response, nil
}
