package main

import (
	"context"
	"log"
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
	"github.com/shafikshaon/ratelimit/internal/repository"
)

func main() {
	cfg := config.Load()

	// PostgreSQL
	db, err := database.NewPool(cfg)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	// Redis
	redisClient, err := database.NewRedisClient(cfg)
	if err != nil {
		log.Fatalf("connect to redis: %v", err)
	}
	defer redisClient.Close()

	// ScyllaDB
	scyllaSession, err := database.NewScyllaSession(cfg)
	if err != nil {
		log.Fatalf("connect to scylla: %v", err)
	}
	defer scyllaSession.Close()

	if err := database.InitScyllaSchema(scyllaSession, cfg.ScyllaKeyspace); err != nil {
		log.Fatalf("init scylla schema: %v", err)
	}

	// Repositories — overrideRepo must be created first; tierRepo depends on it for MGET fallback
	apiRepo := repository.NewAPIRepository(db)
	overrideRepo := repository.NewOverrideRepository(scyllaSession, cfg.ScyllaKeyspace, redisClient)
	tierRepo := repository.NewTierRepository(db, redisClient, overrideRepo)

	// Warm Redis cache for all APIs at startup
	if err := warmTierCache(context.Background(), apiRepo, tierRepo); err != nil {
		log.Printf("warn: tier cache warm-up failed: %v", err)
	}

	apiHandler := handler.NewAPIHandler(apiRepo, tierRepo, overrideRepo)

	router := gin.Default()
	router.Use(cors.Default())

	router.StaticFile("/", "./index.html")
	router.StaticFile("/tester.html", "./tester.html")

	v1 := router.Group("/api/v1")
	{
		v1.GET("/apis", apiHandler.ListAPIs)
		v1.GET("/apis/:name", apiHandler.GetAPI)
		v1.PATCH("/apis/:name/tiers/:tier", apiHandler.UpdateTier)
		v1.GET("/apis/:name/overrides", apiHandler.ListOverrides)
		v1.POST("/apis/:name/overrides", apiHandler.CreateOverride)
		v1.DELETE("/apis/:name/overrides/:wallet", apiHandler.DeleteOverride)
		v1.GET("/apis/:name/config/:wallet", apiHandler.GetWalletConfig)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: router,
	}

	go func() {
		log.Printf("server listening on :%s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced shutdown: %v", err)
	}
	log.Println("server stopped")
}
