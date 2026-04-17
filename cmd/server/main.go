package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"github.com/shafikshaon/ratelimit/internal/config"
	"github.com/shafikshaon/ratelimit/internal/database"
	"github.com/shafikshaon/ratelimit/internal/handler"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/service"
	"go.uber.org/zap"
)

func main() {
	logger.Init(os.Getenv("GIN_MODE") != "release")
	defer logger.Sync()

	log := logger.L
	cfg := config.Load()

	// ── Storage connections ───────────────────────────────────────────────────

	db, err := database.NewPool(cfg)
	if err != nil {
		log.Fatal("connect to postgres", zap.Error(err))
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		log.Fatal("run migrations", zap.Error(err))
	}

	redisClient, err := database.NewRedisClient(cfg)
	if err != nil {
		log.Fatal("connect to redis", zap.Error(err))
	}
	defer redisClient.Close()

	scyllaSession, err := database.NewScyllaSession(cfg)
	if err != nil {
		log.Fatal("connect to scylla", zap.Error(err))
	}
	defer scyllaSession.Close()

	if err := database.InitScyllaSchema(scyllaSession, cfg.ScyllaKeyspace); err != nil {
		log.Fatal("init scylla schema", zap.Error(err))
	}

	// ── Service layer ─────────────────────────────────────────────────────────

	pgSvc := service.NewPostgresService(db)
	redisSvc := service.NewRedisService(redisClient)
	scyllaSvc := service.NewScyllaService(scyllaSession, cfg.ScyllaKeyspace)
	apiSvc := service.NewAPIService(pgSvc, redisSvc, scyllaSvc)

	// Warm Redis cache at startup so the first requests don't hit PostgreSQL.
	if err := warmCache(context.Background(), apiSvc); err != nil {
		log.Warn("tier cache warm-up failed", zap.Error(err))
	}

	// ── HTTP layer ────────────────────────────────────────────────────────────

	apiHandler := handler.NewAPIHandler(apiSvc)

	router := gin.Default()
	router.Use(cors.Default())

	router.StaticFile("/", "./index.html")
	router.StaticFile("/tester.html", "./tester.html")

	v1 := router.Group("/api/v1")
	{
		v1.GET("/apis", apiHandler.ListAPIs)
		v1.GET("/apis/:name", apiHandler.GetAPI)
		v1.PATCH("/apis/:name/tiers/:tier", apiHandler.UpdateTier)
		v1.GET("/apis/:name/config/:wallet", apiHandler.GetWalletConfig)
		v1.POST("/apis/:name/check", apiHandler.CheckRequest)
		v1.GET("/apis/:name/usage", apiHandler.GetUsage)
		v1.GET("/apis/:name/overrides", apiHandler.ListOverrides)
		v1.POST("/apis/:name/overrides", apiHandler.CreateOverride)
		v1.DELETE("/apis/:name/overrides/:wallet", apiHandler.DeleteOverride)
	}

	srv := &http.Server{Addr: ":" + cfg.ServerPort, Handler: router}

	go func() {
		log.Info("server listening", zap.String("addr", ":"+cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("listen", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced shutdown", zap.Error(err))
	}
	log.Info("server stopped")
}
