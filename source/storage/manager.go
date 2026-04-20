package storage

import (
	"file-manager/config"
	"fmt"
)

// StorageManager manages different cloud storage adapters.
type StorageManager struct {
	adapters     map[string]Storage
	defaultCloud string
}

// NewStorageManager initializes and returns a new StorageManager.
func NewStorageManager(cfg *config.AppConfig) (*StorageManager, error) {
	adapters := make(map[string]Storage)

	// Initialize AWS S3 Adapter
	if cfg.StorageConfig.AWSAccessKeyID != "" && cfg.StorageConfig.AWSSecretAccessKey != "" {
		awsAdapter, err := NewAWSS3Adapter(cfg.StorageConfig.AWSRegion, cfg.StorageConfig.AWSAccessKeyID, cfg.StorageConfig.AWSSecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize AWS S3 adapter: %w", err)
		}
		adapters["aws"] = awsAdapter
	}

	// Initialize Google Cloud Storage Adapter
	fmt.Println("GCPProjectID", cfg.StorageConfig.GCPProjectID)
	if cfg.StorageConfig.GCPProjectID != "" {
		gcsAdapter, err := NewGCSAdapter(cfg.StorageConfig.GCPProjectID, cfg.StorageConfig.GCPCredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Google Cloud Storage adapter: %w", err)
		}
		adapters["gcp"] = gcsAdapter
	}

	if len(adapters) == 0 {
		return nil, nil
	}

	if _, ok := adapters[cfg.StorageConfig.DefaultCloud]; !ok {
		return nil, fmt.Errorf("default cloud '%s' is not configured or initialized", cfg.StorageConfig.DefaultCloud)
	}

	return &StorageManager{
		adapters:     adapters,
		defaultCloud: cfg.StorageConfig.DefaultCloud,
	}, nil
}

// HasAdapters reports whether any cloud storage backend is configured.
func (sm *StorageManager) HasAdapters() bool {
	return sm != nil && len(sm.adapters) > 0
}

// GetAdapter returns the Storage adapter for the given cloud provider.
// If provider is empty, returns the default cloud adapter.
func (sm *StorageManager) GetAdapter(provider string) (Storage, error) {
	if provider == "" {
		provider = sm.defaultCloud
	}
	adapter, ok := sm.adapters[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported cloud provider: %s", provider)
	}
	return adapter, nil
}

// GetAllAdapters returns all initialized storage adapters.
func (sm *StorageManager) GetAllAdapters() map[string]Storage {
	return sm.adapters
}
