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

	db, err := database.NewPool(cfg)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	apiRepo := repository.NewAPIRepository(db)
	apiHandler := handler.NewAPIHandler(apiRepo)

	router := gin.Default()
	router.Use(cors.Default())

	router.StaticFile("/", "./index.html")
	router.StaticFile("/tester.html", "./tester.html")

	v1 := router.Group("/api/v1")
	{
		v1.GET("/apis", apiHandler.ListAPIs)
		v1.PATCH("/apis/:name/tiers/:tier", apiHandler.UpdateTier)
		v1.POST("/apis/:name/overrides", apiHandler.CreateOverride)
		v1.DELETE("/apis/:name/overrides/:wallet", apiHandler.DeleteOverride)
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
