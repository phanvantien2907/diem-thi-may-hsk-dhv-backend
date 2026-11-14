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
	"github.com/hskvinh/backend/internal/cache"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/cccd"
	cccdhandlers "github.com/hskvinh/backend/internal/cccd/handlers"
	cccdmodels "github.com/hskvinh/backend/internal/cccd/models"
	cccdworker "github.com/hskvinh/backend/internal/cccd/worker"
	"github.com/hskvinh/backend/internal/certificate"
	"github.com/hskvinh/backend/internal/config"
	"github.com/hskvinh/backend/internal/dashboard"
	"github.com/hskvinh/backend/internal/database"
	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/location"
	"github.com/hskvinh/backend/internal/media"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/notification"
	"github.com/hskvinh/backend/internal/payment"
	"github.com/hskvinh/backend/internal/registration"
	"github.com/hskvinh/backend/internal/result"
	"github.com/hskvinh/backend/internal/worker"
	"github.com/hskvinh/backend/internal/ws"

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

	// ── Redis Cache (shared dependency) ───────────────────────────────────
	cacheSvc := cache.NewService(cache.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer cacheSvc.Close()

	// ── Audit Service (shared dependency) ─────────────────────────────────
	auditSvc := audit.NewService(db)

	// Media Service ( shared dependency)
	mediaSvc, err := media.NewService(cfg.CloudinaryURL)
	if err != nil {
		log.Fatalf("[main] media: %v", err)
	}

	// Vision Service for image validation (Google Cloud Vision with local pixel fallback)
	var visionSvc candidate.VisionService
	if cfg.GoogleCloudCredentialsPath != "" {
		visionSvc, err = candidate.NewGoogleVisionService(context.Background(), cfg.GoogleCloudCredentialsPath)
		if err != nil {
			log.Printf("[main] Google Vision initialization failed (%v); falling back to local portrait validator", err)
			visionSvc = candidate.NewLocalPortraitValidator()
		} else {
			log.Println("[main] Google Vision service initialized successfully")
		}
	} else {
		log.Println("[main] GOOGLE_CLOUD_CREDENTIALS_PATH not set; using local portrait validator (background, brightness, quality checks)")
		visionSvc = candidate.NewLocalPortraitValidator()
	}

	// ── Services ──────────────────────────────────────────────────────────
	authSvc := auth.NewService(db, auditSvc, cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	candSvc := candidate.NewService(db, auditSvc, mediaSvc, visionSvc)
	examSvc := exam.NewService(db, auditSvc)
	regSvc := registration.NewService(db, auditSvc)
	paySvc := payment.NewService(db, auditSvc, payment.PayOSConfig{
		ClientID:    cfg.PayOSClientID,
		ApiKey:      cfg.PayOSApiKey,
		ChecksumKey: cfg.PayOSChecksumKey,
	}, mediaSvc)
	resSvc := result.NewService(db, auditSvc)
	certSvc := certificate.NewService(db, auditSvc, cfg.WebhookSecret)
	notifSvc := notification.NewService(db)
	locSvc := location.NewService(db, auditSvc)

	// Attach Redis cache to approved read-heavy services
	locSvc.SetCache(cacheSvc)
	examSvc.SetCache(cacheSvc)
	resSvc.SetCache(cacheSvc)
	candSvc.SetCache(cacheSvc)
	regSvc.SetCache(cacheSvc)
	middleware.SetAuthCache(cacheSvc)

	// ── WebSocket Hub ────────────────────────────────────────────────────
	hub := ws.NewHub()
	go hub.Run()

	// ── Dashboard Service ────────────────────────────────────────────────
	dashSvc := dashboard.NewService(db, auditSvc, hub)
	dashSvc.SetCache(cacheSvc)

	// ── CCCD OCR/AI Service ──────────────────────────────────────────────
	cccdCfg := cccd.DefaultConfig()
	cccdCfg.GeminiAPIKey = cfg.GeminiAPIKey
	if cfg.GeminiModel != "" {
		cccdCfg.GeminiModel = cfg.GeminiModel
	}
	if cfg.GeminiTimeoutSec > 0 {
		cccdCfg.GeminiTimeoutSec = cfg.GeminiTimeoutSec
	}
	if cfg.CCCDMaxConcurrency > 0 {
		cccdCfg.MaxConcurrency = cfg.CCCDMaxConcurrency
	}
	var cccdExtractor cccd.IdCardExtractor
	if ext, err := cccd.NewGeminiExtractor(cccdCfg); err == nil {
		cccdExtractor = ext
		log.Printf("[main] CCCD Gemini extractor initialized (model=%s)", cccdCfg.GeminiModel)
	} else {
		log.Printf("[main] GEMINI_API_KEY not configured (%v); CCCD AI extraction awaits API key", err)
	}
	cccdSvc := cccd.NewService(cccdExtractor, cccdCfg, nil)
	cccdSvc.SetHub(hub)

	// Attach hub to services that need real-time broadcasting
	regSvc.SetHub(hub)
	regSvc.SetPaymentCanceller(paySvc)
	paySvc.SetHub(hub)
	authSvc.SetHub(hub)
	notifSvc.SetHub(hub)
	go notifSvc.StartEventListener(context.Background())
	candSvc.SetNotificationPublisher(notifSvc)
	paySvc.SetNotificationPublisher(notifSvc)
	examSvc.SetNotificationPublisher(notifSvc)
	regSvc.SetNotificationPublisher(notifSvc)

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
	dashH := dashboard.NewHandler(dashSvc)
	cccdHandler := cccd.NewHandler(cccdSvc)

	// ── Real-Time Asynchronous CCCD OCR Pipeline ─────────────────────────
	pipelineEngine := cccdworker.NewPipelineOCREngine(cccdSvc)
	cccdWorkerPool := cccdworker.NewWorkerPool(
		pipelineEngine,
		cfg.CCCDMaxConcurrency,
		512,
		cacheSvc,
		func(payload cccdmodels.OCREventPayload) {
			if payload.UserID > 0 && payload.Event != "" {
				hub.SendToUser(payload.UserID, ws.EventType(payload.Event), payload)
			}
		},
	)
	cccdUploadH := cccdhandlers.NewUploadHandler(cccdWorkerPool, "")
	cccdHandler.SetUploadHandler(cccdUploadH)

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
	dashboard.RegisterRoutes(api, dashH, cfg.JWTSecret)
	cccd.RegisterRoutes(api, cccdHandler, cfg.JWTSecret)

	// ── WebSocket Route ──────────────────────────────────────────────────
	wsH := ws.NewHandler(hub, cfg.JWTSecret)
	ws.RegisterRoutes(engine, wsH, api)

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

	// ── Background Workers ───────────────────────────────────────────────
	go notifSvc.StartEventListener(ctx)
	cccdSvc.StartWorkers(ctx)
	cccdWorkerPool.Start(ctx)

	if cacheSvc.Enabled() {
		pubsub := cacheSvc.Subscribe(ctx, cccdworker.DefaultRedisChannel)
		go hub.ListenRedisPubSub(ctx, pubsub)
	}

	seatCron := worker.NewSeatReleaseCron(regSvc, hub, 60*time.Second)
	go seatCron.Start(ctx)

	payosSyncCron := worker.NewPayOSSyncCron(dashSvc, hub, 10*time.Minute)
	go payosSyncCron.Start(ctx)

	examPublicationCron := worker.NewExamPublicationCron(examSvc, 30*time.Second)
	go examPublicationCron.Start(ctx)

	geospatialCron := worker.NewGeospatialSyncCron(locSvc, 24*time.Hour)
	go geospatialCron.Start(ctx)

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

	// Close vision service if it was initialized
	if visionSvc != nil {
		if err := visionSvc.Close(); err != nil {
			log.Printf("[main] vision service close error: %v", err)
		}
	}

	if err := srv.Shutdown(shutCtx); err != nil {
		log.Fatalf("[main] forced shutdown: %v", err)
	}
	log.Println("[main] shutdown complete")
}
