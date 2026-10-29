package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"household-bills/backend/internal/api"
	"household-bills/backend/internal/config"
	"household-bills/backend/internal/store"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	if err := run(logger); err != nil {
		logger.Printf("服务退出: %v", err)
		os.Exit(1)
	}
}

func run(logger *log.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("配置错误: %w", err)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelStartup()

	database, err := store.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.Migrate(startupContext); err != nil {
		return err
	}
	logger.Printf("数据库迁移完成")

	bootstrap, err := database.EnsureAdmin(startupContext, cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("初始化管理员账号失败: %w", err)
	}
	switch {
	case bootstrap.Created:
		logger.Printf("管理员账号已创建: %s（家庭：%s）",
			store.BootstrapAdminUsername, store.BootstrapHouseholdName)
	case bootstrap.PasswordRotated:
		logger.Printf("管理员密码已根据 ADMIN_PASSWORD 更新，原管理员会话已失效")
	default:
		logger.Printf("管理员账号已就绪")
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.New(cfg, database, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	logger.Printf("服务已启动，监听 %s", cfg.ListenAddr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务异常: %w", err)
		}
		return nil
	case sig := <-stop:
		logger.Printf("收到信号 %s，正在关闭服务", sig)
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("关闭服务失败: %w", err)
		}
		return nil
	}
}
