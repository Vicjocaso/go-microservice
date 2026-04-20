package application

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	_ "github.com/lib/pq"

	"file-manager/auth"
	"file-manager/domain/file"
	"file-manager/metadata"
	"file-manager/storage"
	"file-manager/telemetry"
)

type App struct {
	router         *echo.Echo
	db             *sql.DB
	config         *AppConfig
	storageManager *storage.StorageManager
	metadataStore  metadata.MetadataStore
}

// GetRouter returns the router for testing purposes
func (a *App) GetRouter() *echo.Echo {
	return a.router
}

func New(config *AppConfig) *App {
	app := &App{
		config: config,
	}

	storageManager, err := storage.NewStorageManager(config)
	if err != nil {
		log.Fatalf("Failed to initialize storage manager: %v", err)
	}
	app.storageManager = storageManager
	if storageManager == nil || !storageManager.HasAdapters() {
		log.Print("No cloud storage configured; file API routes (/v1/files) are disabled.")
	}

	// Initialize Metadata Store (using in-memory for demo)
	metadataStore := metadata.NewInMemoryMetadataStore()
	app.metadataStore = metadataStore

	app.loadMiddleware()
	app.loadRoutes()
	return app
}

func (a *App) Start(ctx context.Context) error {
	logger := telemetry.SLogger(ctx)

	// Listen on all interfaces (required for containers / Railway; localhost would reject proxied traffic).
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.ServerPort),
		Handler: a.router,
	}

	logger.Info("Starting application server...")

	ch := make(chan error, 1)

	// Call the main function using another thread
	go func() {
		// Handle error on startup
		err := server.ListenAndServe()
		if err != nil {
			ch <- fmt.Errorf("failed to start server: %w", err)
		}
		// Close the channel
		close(ch)
	}()

	logger.Info("Application startup complete.")
	logger.Info(fmt.Sprintf("Listening on :%d", a.config.ServerPort))

	select {
	// Select one of the channels, the one that returns first
	case err := <-ch: // This channel returns if the server dies
		return err
	case <-ctx.Done(): // This channel returns on SIGINT
		logger.Info("Shutting down gracefully...")
		if a.db != nil {
			if err := a.db.Close(); err != nil {
				errMsg := map[string]string{"Error": err.Error()}
				logger.Error("Failed to close postgres Client", errMsg)
			}
		}
		// Let's have a graceful timeout of 10 seconds
		timeout, cancel := context.WithTimeout(context.Background(), time.Second*10)
		// Run Cancel at the very end of execution
		defer cancel()
		// Shutdown server after said timeout
		return server.Shutdown(timeout)
	}
}

func (a *App) loadMiddleware() {
	router := echo.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger())
	router.Use(telemetry.Tracing())
	router.Use(middleware.Recover())
	router.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(1000)))
	router.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "Ok")
	})
	router.Use(auth.ServerAuthMiddleware())

	a.router = router
}

func (a *App) loadRoutes() {
	a.router.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "File Manager Service.")
	})

	previewHandler := file.NewPreviewHandler()
	a.router.POST("/v1/files/preview", previewHandler.PreviewTemplate)

	if a.storageManager != nil && a.storageManager.HasAdapters() {
		fileGroup := a.router.Group("/v1/files")
		a.loadFileRoutes(fileGroup)
	}
}
