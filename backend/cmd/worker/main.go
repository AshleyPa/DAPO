// Command worker 异步任务消费者（asynq）。
//
// 监听队列：critical / default / low
// 详见 docs/02-后端规范.md §8。
package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/kleinai/backend/internal/bootstrap"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/logger"
)

const serviceName = "worker"

// Task type 常量（与发布端保持一致）。
const (
	TaskGenImage    = "gen:image"
	TaskGenVideo    = "gen:video"
	TaskPoolHealth  = "pool:health"
	TaskBillSettle  = "bill:settle"
	TaskEmailSend   = "email:send"
	TaskWebhookSend = "webhook:notify"
)

func main() {
	deps, err := bootstrap.Init(serviceName)
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	if deps.Cfg.Redis.Addr == "" {
		logger.L().Fatal("worker requires redis")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if deps.DB != nil {
		sysCfgSvc := service.NewSystemConfigService(repo.NewSystemConfigRepo(deps.DB))
		walletRepo := repo.NewWalletRepo(deps.DB)
		proxyRepo := repo.NewProxyRepo(deps.DB)
		proxySvc := service.NewProxyService(proxyRepo, deps.AES)
		service.NewGrokCFRefreshService(sysCfgSvc, proxySvc).Start(ctx)
		service.NewProxySubscriptionService(proxyRepo, deps.AES, service.NewMihomoManagerFromEnv()).StartAutoSync(ctx)
		inviteSvc := service.NewInviteRewardService(deps.DB, walletRepo, sysCfgSvc)
		rechargeSvc := service.NewRechargeService(deps.DB, repo.NewRechargeRepo(deps.DB), sysCfgSvc, inviteSvc)
		rechargeSvc.StartAlipayReconcileLoop(ctx,
			envDuration("KLEIN_ALIPAY_RECONCILE_INTERVAL", 2*time.Minute),
			envInt("KLEIN_ALIPAY_RECONCILE_LIMIT", 100),
		)
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     deps.Cfg.Redis.Addr,
			Password: deps.Cfg.Redis.Password,
			DB:       deps.Cfg.Redis.DB,
		},
		asynq.Config{
			Concurrency: 16,
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			Logger:          &asynqZap{l: logger.L()},
			ShutdownTimeout: deps.Cfg.Server.ShutdownTimeout,
			HealthCheckFunc: func(err error) {
				if err != nil {
					logger.L().Warn("asynq health", zap.Error(err))
				}
			},
		},
	)

	mux := asynq.NewServeMux()

	// TODO Sprint 5+: 注册具体任务 handler
	mux.HandleFunc(TaskPoolHealth, func(ctx context.Context, t *asynq.Task) error {
		logger.FromCtx(ctx).Info("pool health tick", zap.String("task", t.Type()))
		return nil
	})

	go func() {
		if err := srv.Run(mux); err != nil {
			logger.L().Fatal("asynq run", zap.Error(err))
		}
	}()

	logger.L().Info("worker started", zap.String("redis", deps.Cfg.Redis.Addr))

	<-ctx.Done()

	srv.Shutdown()
	logger.L().Info("worker shutdown done")
}

// asynqZap 把 asynq 日志转 zap。
type asynqZap struct{ l *zap.Logger }

func (a *asynqZap) Debug(args ...any) { a.l.Sugar().Debug(args...) }
func (a *asynqZap) Info(args ...any)  { a.l.Sugar().Info(args...) }
func (a *asynqZap) Warn(args ...any)  { a.l.Sugar().Warn(args...) }
func (a *asynqZap) Error(args ...any) { a.l.Sugar().Error(args...) }
func (a *asynqZap) Fatal(args ...any) { a.l.Sugar().Fatal(args...) }

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}
