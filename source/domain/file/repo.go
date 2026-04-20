package file

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"file-manager/config"
	"file-manager/metadata"
	"file-manager/storage"

	"github.com/google/uuid"
)

// FileRepo handles file-related business logic.
type FileRepo struct {
	storageManager *storage.StorageManager
	metadataStore  metadata.MetadataStore
	appConfig      *config.AppConfig
}

func NewFileRepo(sm *storage.StorageManager, ms metadata.MetadataStore, cfg *config.AppConfig) (*FileRepo, error) {
	if sm == nil || !sm.HasAdapters() {
		return nil, fmt.Errorf("file repository requires configured cloud storage")
	}
	return &FileRepo{
		storageManager: sm,
		metadataStore:  ms,
		appConfig:      cfg,
	}, nil
}

// UploadFile handles file upload, optional encryption, and multi-cloud replication.
func (s *FileRepo) UploadFile(ctx context.Context, fileHeader *multipart.FileHeader, logicalPath string, targetCloud string) (*metadata.FileMetadata, error) {
	src, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	fileBytes, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	originalSize := int64(len(fileBytes))
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fileUUID := uuid.New().String()
	fileName := filepath.Base(logicalPath)
	if fileName == "." || fileName == "/" { // Handle cases where logicalPath might be a directory
		fileName = fileHeader.Filename
	}

	fileMeta := &metadata.FileMetadata{
		ID:          fileUUID,
		LogicalPath: logicalPath,
		FileName:    fileName,
		Size:        originalSize, // Store original size
		ContentType: contentType,
		UploadedAt:  time.Now(),
		CloudCopies: make(map[string]*storage.FileInfo),
		CustomTags:  map[string]string{"original_filename": fileHeader.Filename},
	}

	// Determine target clouds for upload
	cloudsToUpload := []string{s.appConfig.StorageConfig.DefaultCloud}
	if s.appConfig.StorageConfig.ReplicateToAllClouds {
		for provider := range s.storageManager.GetAllAdapters() {
			found := slices.Contains(cloudsToUpload, provider)
			if !found {
				cloudsToUpload = append(cloudsToUpload, provider)
			}
		}
	} else if targetCloud != "" && targetCloud != s.appConfig.StorageConfig.DefaultCloud {
		cloudsToUpload = []string{targetCloud} // Override default if specific target is provided and not replicating
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(cloudsToUpload))
	fileInfoChan := make(chan struct {
		provider string
		info     *storage.FileInfo
	}, len(cloudsToUpload))

	for _, provider := range cloudsToUpload {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			adapter, err := s.storageManager.GetAdapter(p)
			if err != nil {
				errChan <- fmt.Errorf("failed to get adapter for %s: %w", p, err)
				return
			}

			// Create a new reader for each upload to avoid issues with exhausted readers
			uploadReader := bytes.NewReader(fileBytes)

			// Use a unique key for each cloud if needed, or a common one
			cloudKey := fmt.Sprintf("%s/%s", fileUUID, fileHeader.Filename) // Use UUID as prefix for cloud storage key

			log.Printf("Uploading %s to %s bucket %s with key %s", fileHeader.Filename, p, fileHeader.Filename, cloudKey) // Log bucket name

			// Pass original content type, not encrypted content type
			uploadMetadata := map[string]string{"Content-Type": contentType}

			info, uploadErr := adapter.Upload(ctx, fileHeader.Filename, cloudKey, uploadReader, int64(len(fileBytes)), uploadMetadata)
			if uploadErr != nil {
				errChan <- fmt.Errorf("failed to upload to %s: %w", p, uploadErr)
				return
			}
			fileInfoChan <- struct {
				provider string
				info     *storage.FileInfo
			}{provider: p, info: info}
		}(provider)
	}

	wg.Wait()
	close(errChan)
	close(fileInfoChan)

	for err := range errChan {
		log.Printf("Error during multi-cloud upload: %v", err)
		// Decide on error handling: fail fast, or collect all errors and return partial success
		// For now, if any upload fails, we'll return an error.
		return nil, fmt.Errorf("one or more uploads failed: %w", err)
	}

	for fi := range fileInfoChan {
		fileMeta.CloudCopies[fi.provider] = fi.info
	}

	err = s.metadataStore.CreateFileMetadata(ctx, fileMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to save file metadata: %w", err)
	}

	return fileMeta, nil
}

// GetFileMetadata retrieves detailed metadata for a file.
func (s *FileRepo) GetFileMetadata(ctx context.Context, fileID string) (*metadata.FileMetadata, error) {
	return s.metadataStore.GetFileMetadata(ctx, fileID)
}
