// Package router 管理后台路由。
package router

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/bootstrap"
	"github.com/kleinai/backend/internal/handler"
	"github.com/kleinai/backend/internal/middleware"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/jwtx"
)

// MountAdmin 在 root 上挂载 /admin/api/v1。
// 这里提供 AccountPool 实例，供后续 worker / openai 服务可能复用（暂存内部）。
func MountAdmin(r *gin.Engine, deps *bootstrap.Deps) *service.AccountPool {
	v1 := r.Group("/admin/api/v1")

	v1.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"pong": true, "scope": "admin"})
	})

	if deps.DB == nil {
		return nil
	}
	if deps.AES == nil {
		// 没有 AES 也禁止挂载账号池接口（凭证必须加密）
		return nil
	}

	// === repos ===
	adminRepo := repo.NewAdminRepo(deps.DB)
	userRepo := repo.NewUserRepo(deps.DB)
	accountRepo := repo.NewAccountRepo(deps.DB)
	walletRepo := repo.NewWalletRepo(deps.DB)
	generationRepo := repo.NewGenerationRepo(deps.DB)
	proxyRepo := repo.NewProxyRepo(deps.DB)
	sysCfgRepo := repo.NewSystemConfigRepo(deps.DB)
	promoRepo := repo.NewPromoRepo(deps.DB)
	dashboardRepo := repo.NewDashboardRepo(deps.DB)
	promptGalleryRepo := repo.NewPromptGalleryRepo(deps.DB)

	// === pool ===
	pool := service.NewAccountPool(accountRepo, 30*time.Second)

	// === services ===
	adminAuth := service.NewAdminAuthService(adminRepo, deps.JWT)
	adminUserSvc := service.NewAdminUserService(userRepo, walletRepo)
	accountAdmin := service.NewAccountAdminService(accountRepo, pool, deps.AES)
	billingSvc := service.NewBillingService(deps.DB, walletRepo)
	cdkSvc := service.NewCDKService(deps.DB, billingSvc)
	promoSvc := service.NewAdminPromoService(promoRepo)
	sysCfgSvc := service.NewSystemConfigService(sysCfgRepo)
	routeSvc := service.NewProviderRouteService(sysCfgSvc)
	providerRouteTestSvc := service.NewProviderRouteTestService(routeSvc, accountRepo)
	proxySvc := service.NewProxyService(proxyRepo, deps.AES)
	proxySubSvc := service.NewProxySubscriptionService(proxyRepo, deps.AES, service.NewMihomoManagerFromEnv())
	promptGallerySvc := service.NewPromptGalleryService(promptGalleryRepo)
	openaiOAuth := service.NewOpenAIOAuthService(sysCfgSvc)
	accountTest := service.NewAccountTestService(accountRepo, proxySvc, sysCfgSvc, openaiOAuth, deps.AES)
	// 把测试服务注入 AccountAdminService，使 Test/Refresh/BatchRefresh 走得通。
	accountAdmin.SetTestService(accountTest)

	// === handlers ===
	authH := handler.NewAdminAuthHandler(adminAuth, adminRepo)
	userH := handler.NewAdminUserHandler(adminUserSvc)
	accountH := handler.NewAdminAccountHandler(accountAdmin, pool)
	cdkH := handler.NewAdminCDKHandler(cdkSvc)
	billingH := handler.NewAdminBillingHandler(walletRepo)
	promoH := handler.NewAdminPromoHandler(promoSvc)
	proxyH := handler.NewAdminProxyHandler(proxySvc, accountTest, proxySubSvc)
	sysH := handler.NewAdminSystemHandler(sysCfgSvc, deps.Cfg)
	providerRouteH := handler.NewAdminProviderRouteHandler(providerRouteTestSvc)
	logH := handler.NewAdminLogHandler(generationRepo, accountRepo, deps.AES)
	dashboardH := handler.NewAdminDashboardHandler(dashboardRepo)
	promptGalleryH := handler.NewPromptGalleryHandler(promptGallerySvc)

	// auth 公开
	auth := v1.Group("/auth")
	if deps.Limiter != nil {
		auth.Use(middleware.RateLimitIP(deps.Limiter, 30))
	}
	auth.POST("/login", authH.Login)

	// 登录后接口
	authed := v1.Group("/")
	authed.Use(middleware.AuthJWT(deps.JWT, jwtx.SubjectAdmin))
	authed.Use(middleware.AdminTokenVersion(adminRepo))
	{
		superOnly := middleware.RequireAdminRole("super")
		authed.GET("/auth/me", authH.Me)
		authed.POST("/auth/password", authH.ChangePassword)
		authed.GET("/dashboard/overview", dashboardH.Overview)

		users := authed.Group("/users")
		{
			users.GET("", userH.List)
			users.POST("", userH.Create)
			users.PUT("/:id", userH.Update)
			users.POST("/:id/points", superOnly, userH.AdjustPoints)
		}

		acc := authed.Group("/accounts")
		acc.Use(superOnly)
		{
			acc.GET("", accountH.List)
			acc.POST("", accountH.Create)
			acc.POST("/import", accountH.BatchImport)
			acc.POST("/batch-delete", accountH.BatchDelete)
			acc.POST("/batch-assign-proxy", accountH.BatchAssignProxy)
			acc.POST("/purge", accountH.Purge)
			acc.POST("/batch-refresh", accountH.BatchRefresh)
			acc.POST("/batch-probe", accountH.BatchProbeQuota)
			acc.GET("/stats", accountH.PoolStats)
			acc.PUT("/:id", accountH.Update)
			acc.DELETE("/:id", accountH.Delete)
			acc.POST("/:id/test", accountH.Test)
			acc.POST("/:id/refresh", accountH.RefreshOAuth)
			acc.GET("/:id/secrets", accountH.Secrets)
		}

		// 代理管理：CRUD + 连通性测试
		proxies := authed.Group("/proxies")
		proxies.Use(superOnly)
		{
			proxies.GET("", proxyH.List)
			proxies.POST("", proxyH.Create)
			proxies.POST("/import", proxyH.BatchImport)
			proxies.GET("/subscriptions", proxyH.ListSubscriptions)
			proxies.POST("/subscriptions", proxyH.CreateSubscription)
			proxies.POST("/subscriptions/preview", proxyH.PreviewSubscription)
			proxies.POST("/subscriptions/:id/sync", proxyH.SyncSubscription)
			proxies.DELETE("/subscriptions/:id", proxyH.DeleteSubscription)
			proxies.POST("/batch-delete", proxyH.BatchDelete)
			proxies.POST("/batch-test", proxyH.BatchTest)
			proxies.PUT("/:id", proxyH.Update)
			proxies.DELETE("/:id", proxyH.Delete)
			proxies.POST("/:id/test", proxyH.Test)
		}

		// 系统配置：通用 KV（代理全局开关 / OAuth 客户端 / refresh 阈值等）
		sys := authed.Group("/system")
		sys.Use(superOnly)
		{
			sys.GET("/settings", sysH.GetSettings)
			sys.PUT("/settings", sysH.UpdateSettings)
			sys.GET("/readiness", sysH.Readiness)
			sys.GET("/cache", sysH.CacheStats)
			sys.DELETE("/cache", sysH.CleanCache)
		}

		providerRoutes := authed.Group("/provider-routes")
		providerRoutes.Use(superOnly)
		{
			providerRoutes.GET("/health", providerRouteH.Health)
			providerRoutes.POST("/test", providerRouteH.Test)
		}

		cdk := authed.Group("/cdk")
		cdk.Use(superOnly)
		{
			cdk.POST("/batches", cdkH.CreateBatch)
		}

		billing := authed.Group("/billing")
		{
			billing.GET("/wallet-logs", billingH.WalletLogs)
		}

		promo := authed.Group("/promo")
		promo.Use(superOnly)
		{
			promo.GET("/codes", promoH.List)
			promo.POST("/codes", promoH.Create)
			promo.PUT("/codes/:id", promoH.Update)
			promo.DELETE("/codes/:id", promoH.Delete)
		}

		promptGallery := authed.Group("/prompt-gallery")
		{
			promptGallery.GET("", promptGalleryH.AdminList)
			promptGallery.POST("", promptGalleryH.Create)
			promptGallery.POST("/upload-cover", promptGalleryH.UploadCover)
			promptGallery.POST("/seed-defaults", promptGalleryH.SeedDefaults)
			promptGallery.POST("/reorder", promptGalleryH.Reorder)
			promptGallery.PUT("/:id", promptGalleryH.Update)
			promptGallery.DELETE("/:id", promptGalleryH.Delete)
		}

		logs := authed.Group("/logs")
		{
			logs.GET("/generations", logH.GenerationLogs)
			logs.GET("/upstream-failures", logH.UpstreamFailures)
			logs.GET("/generations/:task_id/preview", logH.GenerationPreview)
			logs.GET("/generations/:task_id/upstream", logH.GenerationUpstreamLogs)
			logs.DELETE("/generations", logH.PurgeGenerationLogs)
		}
	}

	return pool
}
