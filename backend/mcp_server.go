package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/accounts"
	"github.com/xpzouying/xiaohongshu-mcp/session"
)

type AccountArgs struct {
	AccountID int `json:"account_id,omitempty"`
}

type LoginArgs struct {
	AccountID int     `json:"account_id,omitempty"`
	Proxy     *string `json:"proxy,omitempty"`
}

type PublishContentArgs struct {
	AccountID int      `json:"account_id,omitempty"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Images    []string `json:"images"`
	Tags      []string `json:"tags,omitempty"`
}

type PublishVideoArgs struct {
	AccountID int      `json:"account_id,omitempty"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Video     string   `json:"video"`
	Tags      []string `json:"tags,omitempty"`
}

type SearchFeedsArgs struct {
	AccountID int          `json:"account_id,omitempty"`
	Keyword   string       `json:"keyword"`
	Filters   FilterOption `json:"filters,omitempty"`
}

type FilterOption struct {
	SortBy      string `json:"sort_by,omitempty"`
	NoteType    string `json:"note_type,omitempty"`
	PublishTime string `json:"publish_time,omitempty"`
	SearchScope string `json:"search_scope,omitempty"`
	Location    string `json:"location,omitempty"`
}

type FeedDetailArgs struct {
	AccountID        int    `json:"account_id,omitempty"`
	FeedID           string `json:"feed_id"`
	XsecToken        string `json:"xsec_token"`
	LoadAllComments  bool   `json:"load_all_comments,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	ClickMoreReplies bool   `json:"click_more_replies,omitempty"`
	ReplyLimit       int    `json:"reply_limit,omitempty"`
	ScrollSpeed      string `json:"scroll_speed,omitempty"`
}

type UserProfileArgs struct {
	AccountID int    `json:"account_id,omitempty"`
	UserID    string `json:"user_id"`
	XsecToken string `json:"xsec_token"`
}

type PostCommentArgs struct {
	AccountID int    `json:"account_id,omitempty"`
	FeedID    string `json:"feed_id"`
	XsecToken string `json:"xsec_token"`
	Content   string `json:"content"`
}

type ReplyCommentArgs struct {
	AccountID int    `json:"account_id,omitempty"`
	FeedID    string `json:"feed_id"`
	XsecToken string `json:"xsec_token"`
	CommentID string `json:"comment_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Content   string `json:"content"`
}

type LikeFeedArgs struct {
	AccountID int    `json:"account_id,omitempty"`
	FeedID    string `json:"feed_id"`
	XsecToken string `json:"xsec_token"`
	Unlike    bool   `json:"unlike,omitempty"`
}

type FavoriteFeedArgs struct {
	AccountID  int    `json:"account_id,omitempty"`
	FeedID     string `json:"feed_id"`
	XsecToken  string `json:"xsec_token"`
	Unfavorite bool   `json:"unfavorite,omitempty"`
}

func ensureAccountCtx(ctx context.Context, app *AppServer, accountID int) (context.Context, *accounts.Account, error) {
	id := accountID
	if id == 0 {
		id = 1
	}
	acc, err := app.accounts.Get(id)
	if err != nil && id == 1 {
		acc, err = app.accounts.Create("", "")
	}
	if err != nil {
		return ctx, nil, err
	}
	ctx = session.WithAccount(ctx, acc.Key)
	return ctx, acc, nil
}

func ensureAccountForLogin(ctx context.Context, app *AppServer, accountID int, proxy *string) (context.Context, *accounts.Account, error) {
	// If account_id is 0, create a brand new account (with proxy if provided).
	if accountID == 0 {
		proxyVal := ""
		if proxy != nil {
			proxyVal = *proxy
		}
		acc, err := app.accounts.Create(proxyVal, "")
		if err != nil {
			return ctx, nil, err
		}
		ctx = session.WithAccount(ctx, acc.Key)
		return ctx, acc, nil
	}

	// account_id provided: get or create (for id==1), and optionally update proxy.
	acc, err := app.accounts.Get(accountID)
	if err != nil && accountID == 1 {
		proxyVal := ""
		if proxy != nil {
			proxyVal = *proxy
		}
		acc, err = app.accounts.Create(proxyVal, "")
	}
	if err != nil {
		return ctx, nil, err
	}
	if proxy != nil {
		cfg := accounts.ProxyConfig{Raw: *proxy}
		if updated, err := app.accounts.ApplyProxyConfig(accountID, cfg); err == nil {
			acc = updated
		} else {
			return ctx, nil, err
		}
	}
	ctx = session.WithAccount(ctx, acc.Key)
	return ctx, acc, nil
}

// InitMCPServer 初始化 MCP Server
func InitMCPServer(appServer *AppServer) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "xiaohongshu-mcp",
			Version: "2.0.0",
		},
		nil,
	)

	registerTools(server, appServer)

	logrus.Info("MCP Server initialized with official SDK")

	return server
}

func withPanicRecovery[T any](
	toolName string,
	handler func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error),
) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {

	return func(ctx context.Context, req *mcp.CallToolRequest, args T) (result *mcp.CallToolResult, resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithFields(logrus.Fields{
					"tool":  toolName,
					"panic": r,
				}).Error("Tool handler panicked")

				logrus.Errorf("Stack trace:\n%s", debug.Stack())

				result = &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("工具 %s 执行时发生内部错误: %v\n\n请查看服务端日志获取详细信息", toolName, r),
						},
					},
					IsError: true,
				}
				resp = nil
				err = nil
			}
		}()

		return handler(ctx, req, args)
	}
}

func registerTools(server *mcp.Server, appServer *AppServer) {
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "list_accounts",
			Description: "列出已创建的账号及其登录状态、代理、指纹",
		},
		withPanicRecovery("list_accounts", func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			accounts := appServer.accounts.List()
			data, _ := json.MarshalIndent(accounts, "", "  ")
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
			}, nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "check_login_status",
			Description: "检查小红书登录状态",
		},
		withPanicRecovery("check_login_status", func(ctx context.Context, req *mcp.CallToolRequest, args AccountArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			result := appServer.handleCheckLoginStatus(ctx)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "get_login_qrcode",
			Description: "获取登录二维码（返回 Base64 图片和超时时间）",
		},
		withPanicRecovery("get_login_qrcode", func(ctx context.Context, req *mcp.CallToolRequest, args LoginArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountForLogin(ctx, appServer, args.AccountID, args.Proxy)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			ctx = session.WithHeadless(ctx, false)
			result := appServer.handleGetLoginQrcode(ctx)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "delete_cookies",
			Description: "删除 cookies 文件，重置登录状态。删除后需要重新登录",
		},
		withPanicRecovery("delete_cookies", func(ctx context.Context, req *mcp.CallToolRequest, args AccountArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			result := appServer.handleDeleteCookies(ctx)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "publish_content",
			Description: "发布小红书图文内容",
		},
		withPanicRecovery("publish_content", func(ctx context.Context, req *mcp.CallToolRequest, args PublishContentArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"images":  convertStringsToInterfaces(args.Images),
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handlePublishContent(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "save_draft_content",
			Description: "保存小红书图文草稿（点击“暂时离开”）",
		},
		withPanicRecovery("save_draft_content", func(ctx context.Context, req *mcp.CallToolRequest, args PublishContentArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"images":  convertStringsToInterfaces(args.Images),
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handleSaveDraftContent(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "schedule_publish_content",
			Description: "定时发布小红书图文内容（自动选择当前时间+3天）",
		},
		withPanicRecovery("schedule_publish_content", func(ctx context.Context, req *mcp.CallToolRequest, args PublishContentArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"images":  convertStringsToInterfaces(args.Images),
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handlePublishContentScheduled(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "list_feeds",
			Description: "获取首页 Feeds 列表",
		},
		withPanicRecovery("list_feeds", func(ctx context.Context, req *mcp.CallToolRequest, args AccountArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			result := appServer.handleListFeeds(ctx)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "search_feeds",
			Description: "搜索小红书内容（需要已登录）",
		},
		withPanicRecovery("search_feeds", func(ctx context.Context, req *mcp.CallToolRequest, args SearchFeedsArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			result := appServer.handleSearchFeeds(ctx, args)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "get_feed_detail",
			Description: "获取小红书笔记详情，返回内容、图片、作者信息、互动数据以及评论列表",
		},
		withPanicRecovery("get_feed_detail", func(ctx context.Context, req *mcp.CallToolRequest, args FeedDetailArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"feed_id":           args.FeedID,
				"xsec_token":        args.XsecToken,
				"load_all_comments": args.LoadAllComments,
			}

			if args.LoadAllComments {
				argsMap["click_more_replies"] = args.ClickMoreReplies
				limit := args.Limit
				if limit <= 0 {
					limit = 20
				}
				argsMap["max_comment_items"] = limit

				replyLimit := args.ReplyLimit
				if replyLimit <= 0 {
					replyLimit = 10
				}
				argsMap["max_replies_threshold"] = replyLimit

				if args.ScrollSpeed != "" {
					argsMap["scroll_speed"] = args.ScrollSpeed
				}
			}

			result := appServer.handleGetFeedDetail(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "user_profile",
			Description: "获取指定小红书用户主页，返回用户信息及笔记内容",
		},
		withPanicRecovery("user_profile", func(ctx context.Context, req *mcp.CallToolRequest, args UserProfileArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"user_id":    args.UserID,
				"xsec_token": args.XsecToken,
			}
			result := appServer.handleUserProfile(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "post_comment_to_feed",
			Description: "发表评论到小红书笔记",
		},
		withPanicRecovery("post_comment_to_feed", func(ctx context.Context, req *mcp.CallToolRequest, args PostCommentArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"feed_id":    args.FeedID,
				"xsec_token": args.XsecToken,
				"content":    args.Content,
			}
			result := appServer.handlePostComment(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "reply_comment_in_feed",
			Description: "回复小红书笔记下的指定评论",
		},
		withPanicRecovery("reply_comment_in_feed", func(ctx context.Context, req *mcp.CallToolRequest, args ReplyCommentArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			if args.CommentID == "" && args.UserID == "" {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "缺少 comment_id 或 user_id"}},
				}, nil, nil
			}

			argsMap := map[string]interface{}{
				"feed_id":    args.FeedID,
				"xsec_token": args.XsecToken,
				"comment_id": args.CommentID,
				"user_id":    args.UserID,
				"content":    args.Content,
			}
			result := appServer.handleReplyComment(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "publish_with_video",
			Description: "发布小红书视频内容（仅支持本地单个视频文件）",
		},
		withPanicRecovery("publish_with_video", func(ctx context.Context, req *mcp.CallToolRequest, args PublishVideoArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"video":   args.Video,
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handlePublishVideo(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "save_draft_video",
			Description: "保存小红书视频草稿（点击“暂时离开”）",
		},
		withPanicRecovery("save_draft_video", func(ctx context.Context, req *mcp.CallToolRequest, args PublishVideoArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"video":   args.Video,
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handleSaveDraftVideo(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "schedule_publish_video",
			Description: "定时发布小红书视频内容（自动选择当前时间+3天）",
		},
		withPanicRecovery("schedule_publish_video", func(ctx context.Context, req *mcp.CallToolRequest, args PublishVideoArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"title":   args.Title,
				"content": args.Content,
				"video":   args.Video,
				"tags":    convertStringsToInterfaces(args.Tags),
			}
			result := appServer.handlePublishVideoScheduled(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "like_feed",
			Description: "为指定笔记点赞或取消点赞",
		},
		withPanicRecovery("like_feed", func(ctx context.Context, req *mcp.CallToolRequest, args LikeFeedArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"feed_id":    args.FeedID,
				"xsec_token": args.XsecToken,
				"unlike":     args.Unlike,
			}
			result := appServer.handleLikeFeed(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "favorite_feed",
			Description: "收藏指定笔记或取消收藏",
		},
		withPanicRecovery("favorite_feed", func(ctx context.Context, req *mcp.CallToolRequest, args FavoriteFeedArgs) (*mcp.CallToolResult, any, error) {
			ctx, _, err := ensureAccountCtx(ctx, appServer, args.AccountID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			argsMap := map[string]interface{}{
				"feed_id":    args.FeedID,
				"xsec_token": args.XsecToken,
				"unfavorite": args.Unfavorite,
			}
			result := appServer.handleFavoriteFeed(ctx, argsMap)
			return convertToMCPResult(result), nil, nil
		}),
	)

	logrus.Infof("Registered %d MCP tools", 18)
}

// convertToMCPResult 将自定义的 MCPToolResult 转换为官方 SDK 的格式
func convertToMCPResult(result *MCPToolResult) *mcp.CallToolResult {
	var contents []mcp.Content
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			contents = append(contents, &mcp.TextContent{Text: c.Text})
		case "image":
			imageData, err := base64.StdEncoding.DecodeString(c.Data)
			if err != nil {
				logrus.WithError(err).Error("Failed to decode base64 image data")
				contents = append(contents, &mcp.TextContent{
					Text: "图片数据解码失败: " + err.Error(),
				})
			} else {
				contents = append(contents, &mcp.ImageContent{
					Data:     imageData,
					MIMEType: c.MimeType,
				})
			}
		}
	}

	return &mcp.CallToolResult{
		Content: contents,
		IsError: result.IsError,
	}
}

// convertStringsToInterfaces 辅助函数：将 []string 转换为 []interface{}
func convertStringsToInterfaces(strs []string) []interface{} {
	result := make([]interface{}, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}
