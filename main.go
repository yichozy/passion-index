// passion-index main entry point.
//
// 启动顺序:
//  1. hopebox/env 加载 .env(dev 时)
//  2. hopebox/log 初始化
//  3. hopebox/dao 连 PG + AutoMigrate documents 表
//  4. internal/orm 初始化(OOP 注入) + data dir 准备
//  5. gin HTTP server:
//     - POST /query  (GraphQL, gqlgen)
//     - GET /        (GraphiQL playground, dev)
//     - GET /healthz
//
// Phase 3+ 加 tree_service;Phase 6 接 pipeline worker;
// Phase 7 加 REST 图片下载(/documents/:docId/images/:name)。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-gonic/gin"

	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	"github.com/yichozy/hopebox/log"

	"github.com/yichozy/passion-index/graph"
	"github.com/yichozy/passion-index/internal/orm"
)

func main() {
	// Step 1: 加载 .env(dev 时);prod 用容器注入的 env。
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

	// Step 5: gin + GraphQL + healthz
	r := gin.New()
	r.Use(gin.Recovery())

	// GraphQL endpoint (gqlgen). Wrap net/http handler for gin.
	gqlRes := &graph.Resolver{}
	gqlSrv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: gqlRes}))
	// Multipart form support for `uploadDocument(file: Upload!)` — cap 100MB.
	gqlSrv.AddTransport(transport.MultipartForm{MaxUploadSize: 100 * 1024 * 1024})
	r.POST("/query", gin.WrapH(gqlSrv))

	// GraphiQL playground (dev tool, served at root).
	r.GET("/", gin.WrapH(playground.Handler("passion-index", "/query")))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	})

	// REST: single figure image retrieval (graphql figure.data is intentionally
	// not populated — images live in OSS and are fetched on demand here).
	r.GET("/documents/:docID/images/:name", imageHandler)

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

	// 优雅关闭
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

// imageHandler redirects to a 24h-signed OSS URL for the requested figure.
// The client (browser / curl / LLM provider) fetches bytes directly from OSS;
// passion-index never proxies image bytes.
func imageHandler(c *gin.Context) {
	docID := c.Param("docID")
	name := c.Param("name")

	// Reject path-traversal attempts; MinerU image names are plain basenames.
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image name"})
		return
	}

	oss, err := aliyun.NewOss()
	if err != nil {
		log.Errorf(c.Request.Context(), "image: oss init: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "oss init failed"})
		return
	}

	object_key := fmt.Sprintf("passion-index/%s/images/%s", docID, name)
	url, err := oss.GetObjectURL(c.Request.Context(), object_key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		return
	}
	c.Redirect(http.StatusFound, url)
}
