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
)

func main() {
	logger.Init(os.Getenv("GIN_MODE") != "release")
	defer logger.Sync()

	cfg := config.Load()

	// ── Storage connections ───────────────────────────────────────────────────

	db, err := database.NewPool(cfg)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		panic(err)
	}

	redisClient, err := database.NewRedisClient(cfg)
	if err != nil {
		panic(err)
	}
	defer redisClient.Close()

	scyllaSession, err := database.NewScyllaSession(cfg)
	if err != nil {
		panic(err)
	}
	defer scyllaSession.Close()

	if err := database.InitScyllaSchema(scyllaSession, cfg.ScyllaKeyspace); err != nil {
		panic(err)
	}

	// ── Service layer ─────────────────────────────────────────────────────────

	pgSvc := service.NewPostgresService(db)
	redisSvc := service.NewRedisService(redisClient)
	scyllaSvc := service.NewScyllaService(scyllaSession, cfg.ScyllaKeyspace)
	apiSvc := service.NewAPIService(pgSvc, redisSvc, scyllaSvc)

	// Warm Redis cache at startup so the first requests don't hit PostgreSQL.
	wctx, wcancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = warmCache(wctx, apiSvc)
	wcancel()

	// ── HTTP layer ────────────────────────────────────────────────────────────

	apiHandler := handler.NewAPIHandler(apiSvc)

	corsConfig := cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(cors.New(corsConfig))
	router.Use(requestTimeoutMiddleware(10 * time.Second))
	router.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
		c.Next()
	})

	router.GET("/health", healthHandler(db, redisClient))
	router.GET("/ready", healthHandler(db, redisClient))

	router.StaticFile("/", "./index.html")
	router.StaticFile("/index.html", "./index.html")
	router.StaticFile("/tester.html", "./tester.html")

	v1 := router.Group("/api/v1")
	v1.Use(queryCountLoggerMiddleware())
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
		v1.GET("/redis/export", apiHandler.ExportRedis)
	}

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
