package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/audit"
	"github.com/hskvinh/backend/internal/auth"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/certificate"
	"github.com/hskvinh/backend/internal/config"
	"github.com/hskvinh/backend/internal/database"
	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/location"
	"github.com/hskvinh/backend/internal/media"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/notification"
	"github.com/hskvinh/backend/internal/payment"
	"github.com/hskvinh/backend/internal/registration"
	"github.com/hskvinh/backend/internal/result"

	_ "github.com/hskvinh/backend/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title Vinh University HSK Computer-based Test Registration System API
// @version 1.0
// @description This is the backend API for Vinh University HSK Computer-based Test Registration System.
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	// ── Config ────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[main] config: %v", err)
	}

	// ── Database ──────────────────────────────────────────────────────────
	db, err := database.Connect(database.Options{
		DSN:             cfg.DatabaseURL,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
	})
	if err != nil {
		log.Fatalf("[main] database: %v", err)
	}
	defer database.Close(db)

	// ── Audit Service (shared dependency) ─────────────────────────────────
	auditSvc := audit.NewService(db)

	// Media Service ( shared dependency)
	mediaSvc, err := media.NewService(cfg.CloudinaryURL)
	if err != nil {
		log.Fatalf("[main] media: %v", err)
	}

	// ── Services ──────────────────────────────────────────────────────────
	authSvc := auth.NewService(db, auditSvc, cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	candSvc := candidate.NewService(db, auditSvc, mediaSvc)
	examSvc := exam.NewService(db, auditSvc)
	regSvc := registration.NewService(db, auditSvc)
	paySvc := payment.NewService(db, auditSvc, regSvc)
	resSvc := result.NewService(db, auditSvc)
	certSvc := certificate.NewService(db, auditSvc, cfg.WebhookSecret)
	notifSvc := notification.NewService(db)
	locSvc := location.NewService(db, auditSvc)

	// ── Handlers ──────────────────────────────────────────────────────────
	auditH := audit.NewHandler(auditSvc)
	authH := auth.NewHandler(authSvc)
	candH := candidate.NewHandler(candSvc)
	examH := exam.NewHandler(examSvc)
	regH := registration.NewHandler(regSvc)
	payH := payment.NewHandler(paySvc)
	resH := result.NewHandler(resSvc)
	certH := certificate.NewHandler(certSvc)
	notifH := notification.NewHandler(notifSvc)
	locH := location.NewHandler(locSvc)

	// ── Middleware ────────────────────────────────────────────────────────
	limiter := middleware.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	engine.Use(middleware.CORS(cfg.CORSAllowedOrigins), middleware.Logger(), gin.Recovery(), limiter.RateLimit())

	// Redirect Root to Swagger UI
	engine.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})

	// ── Health ────────────────────────────────────────────────────────────
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"status": "ok"},
			"error":   nil,
			"meta":    nil,
		})
	})

	// ── Swagger API Docs ──────────────────────────────────────────────────
	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ── Feature Routes ────────────────────────────────────────────────────
	api := engine.Group("/api/v1")

	audit.RegisterRoutes(api, auditH, cfg.JWTSecret)

	authLimiter := middleware.NewRateLimiter(3, 5) // 3 RPS / 5 burst for auth
	auth.RegisterRoutes(api, authH, cfg.JWTSecret, authLimiter)

	candidate.RegisterRoutes(api, candH, cfg.JWTSecret)
	exam.RegisterRoutes(api, examH, cfg.JWTSecret)
	registration.RegisterRoutes(api, regH, cfg.JWTSecret)
	payment.RegisterRoutes(api, payH, cfg.JWTSecret)
	result.RegisterRoutes(api, resH, cfg.JWTSecret)
	certificate.RegisterRoutes(api, certH, cfg.JWTSecret)
	notification.RegisterRoutes(api, notifH, cfg.JWTSecret)
	location.RegisterRoutes(api, locH, cfg.JWTSecret)

	// ── HTTP Server ───────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Graceful Shutdown ─────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[main] listening on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] server error: %v", err)
		}
	}()

	<-ctx.Done()
	stop()

	log.Println("[main] graceful shutdown (30s timeout)…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Fatalf("[main] forced shutdown: %v", err)
	}
	log.Println("[main] shutdown complete")
}
