// passion-index main entry point.
//
// Startup order:
//  1. hopebox/env loads .env (dev only)
//  2. hopebox/log init
//  3. hopebox/dao connects to PG + AutoMigrate
//  4. internal/orm init (DI) + data dir setup
//  5. gin HTTP server (REST, see internal/httpapi):
//     - /healthz
//     - documents / nodes / folders routes + image redirects
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	"github.com/yichozy/hopebox/log"

	"github.com/yichozy/passion-index/internal/handler"
	"github.com/yichozy/passion-index/internal/orm"
)

func main() {
	// Step 1: load .env (dev only); prod uses container-injected env.
	if os.Getenv("ENV") != "prod" {
		env.LoadEnvVariable()
	}

	// Step 2: log
	log.Init(log.DefaultConfig())
	defer log.Sync()

	ctx := context.Background()
	log.Info(ctx, "passion-index starting up...")

	// Step 3: Postgres
	dao.InitPgDbConn()
	db := dao.GetDB()
	if db == nil {
		log.Error(ctx, "failed to connect to postgres — check POSTGRES_* env")
		os.Exit(1)
	}
	orm.DoAutoMigrate()

	// Step 4: gin + REST routes
	r := gin.New()
	r.Use(gin.Recovery())
	r.MaxMultipartMemory = 100 << 20 // upload cap, matches the old GraphQL transport

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	})
	handler.Register(r)

	port := os.Getenv("PASSION_INDEX_PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Info(ctx, "listening on :"+port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf(ctx, "listen: %v", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info(ctx, "shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf(ctx, "server shutdown: %v", err)
	}
	log.Info(ctx, "passion-index stopped")
}
