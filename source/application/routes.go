package application

import (
	"log"

	"file-manager/domain/file"

	"github.com/labstack/echo/v4"
)

func (a *App) loadFileRoutes(g *echo.Group) {
	fileRepo, err := file.NewFileRepo(a.storageManager, a.metadataStore, a.config)
	if err != nil {
		log.Fatalf("Failed to create file repository: %v", err)
	}

	fileHandler := file.NewFileHandler(fileRepo)

	g.POST("/render-template", fileHandler.Insert)

}
