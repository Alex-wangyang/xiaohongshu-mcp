package main

import (
	"net"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupRoutes 设置路由配置
func setupRoutes(appServer *AppServer) *gin.Engine {
	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 添加中间件
	router.Use(errorHandlingMiddleware())
	router.Use(corsMiddleware())

	// 健康检查
	router.GET("/health", healthHandler)

	// MCP 端点 - 使用官方 SDK 的 Streamable HTTP Handler
	mcpHandler := newMCPHTTPHandler(appServer)
	protected := router.Group("")
	protected.Use(authMiddleware(appServer.authToken))

	protected.Any("/mcp", gin.WrapH(mcpHandler))
	protected.Any("/mcp/*path", gin.WrapH(mcpHandler))

	// API 路由组
	api := protected.Group("/api/v1")
	{
		api.GET("/login/status", appServer.checkLoginStatusHandler)
		api.GET("/login/qrcode", appServer.getLoginQrcodeHandler)
		api.DELETE("/login/cookies", appServer.deleteCookiesHandler)
		api.POST("/publish", appServer.publishHandler)
		api.POST("/publish_video", appServer.publishVideoHandler)
		api.GET("/feeds/list", appServer.listFeedsHandler)
		api.GET("/feeds/search", appServer.searchFeedsHandler)
		api.POST("/feeds/search", appServer.searchFeedsHandler)
		api.POST("/feeds/detail", appServer.getFeedDetailHandler)
		api.POST("/user/profile", appServer.userProfileHandler)
		api.POST("/feeds/comment", appServer.postCommentHandler)
		api.POST("/feeds/comment/reply", appServer.replyCommentHandler)
		api.POST("/feeds/like", appServer.likeFeedHandler)
		api.POST("/feeds/favorite", appServer.favoriteFeedHandler)
		api.GET("/user/me", appServer.myProfileHandler)
		api.GET("/notifications/unread", appServer.getUnreadCountHandler)
		api.GET("/notifications/list", appServer.listNotificationsHandler)
		api.POST("/notifications/list", appServer.listNotificationsHandler)
		api.POST("/notifications/reply", appServer.replyNotificationHandler)
		api.POST("/notifications/like", appServer.likeNotificationHandler)
	}

	return router
}

func newMCPHTTPHandler(appServer *AppServer) http.Handler {
	getServer := func(r *http.Request) *mcp.Server {
		return appServer.mcpServer
	}
	options := &mcp.StreamableHTTPOptions{
		JSONResponse: true, // 支持 JSON 响应
		// 换取客户端可跳过 initialize 握手直接调工具。代价是服务端无法反向
		// 请求客户端（sampling / elicitation / roots），眼下一处都没用到；
		// 要用得先摘掉这行。
		Stateless: true,
	}
	defaultHandler := mcp.NewStreamableHTTPHandler(getServer, options)

	trustedAuthority := os.Getenv("MCP_TRUSTED_LOCAL_HOST")
	if trustedAuthority == "" {
		return defaultHandler
	}

	trustedOptions := *options
	trustedOptions.DisableLocalhostProtection = true
	trustedHandler := mcp.NewStreamableHTTPHandler(getServer, &trustedOptions)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isTrustedLocalHostRequest(r, trustedAuthority) {
			trustedHandler.ServeHTTP(w, r)
			return
		}
		defaultHandler.ServeHTTP(w, r)
	})
}

func isTrustedLocalHostRequest(r *http.Request, authority string) bool {
	if r == nil || authority == "" || r.Host != authority {
		return false
	}

	localAddr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || localAddr == nil {
		return false
	}

	localHost, _, err := net.SplitHostPort(localAddr.String())
	if err != nil {
		return false
	}
	localIP := net.ParseIP(localHost)
	return localIP != nil && localIP.IsLoopback()
}
