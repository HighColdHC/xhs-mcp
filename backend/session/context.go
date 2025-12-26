package session

import "context"

type ctxKey string

const (
	accountKey   ctxKey = "account_id"
	proxyKey     ctxKey = "proxy"
	userAgentKey ctxKey = "user_agent"
	headlessKey  ctxKey = "headless_override"
)

// WithAccount returns a new context that carries the account ID. Empty means "default".
func WithAccount(ctx context.Context, accountID string) context.Context {
	if accountID == "" {
		accountID = "default"
	}
	return context.WithValue(ctx, accountKey, accountID)
}

// Account extracts the account ID from context, falling back to "default".
func Account(ctx context.Context) string {
	if v := ctx.Value(accountKey); v != nil {
		if id, ok := v.(string); ok && id != "" {
			return id
		}
	}
	return "default"
}

// WithProxy attaches a proxy string to the context.
func WithProxy(ctx context.Context, proxy string) context.Context {
	if proxy == "" {
		return ctx
	}
	return context.WithValue(ctx, proxyKey, proxy)
}

// Proxy extracts the proxy string from context.
func Proxy(ctx context.Context) string {
	if v := ctx.Value(proxyKey); v != nil {
		if p, ok := v.(string); ok {
			return p
		}
	}
	return ""
}

// WithUserAgent attaches a user-agent override to the context.
func WithUserAgent(ctx context.Context, ua string) context.Context {
	if ua == "" {
		return ctx
	}
	return context.WithValue(ctx, userAgentKey, ua)
}

// UserAgent extracts the user-agent override from context.
func UserAgent(ctx context.Context) string {
	if v := ctx.Value(userAgentKey); v != nil {
		if ua, ok := v.(string); ok {
			return ua
		}
	}
	return ""
}

// WithHeadless overrides headless mode in context.
func WithHeadless(ctx context.Context, headless bool) context.Context {
	return context.WithValue(ctx, headlessKey, headless)
}

// HeadlessOverride returns a bool pointer when override is set.
func HeadlessOverride(ctx context.Context) *bool {
	if v := ctx.Value(headlessKey); v != nil {
		if h, ok := v.(bool); ok {
			return &h
		}
	}
	return nil
}
