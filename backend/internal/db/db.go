package db

import (
	"os"
	"path/filepath"

	"proxydeck/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func Open(path string) (*gorm.DB, error) {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&model.User{}, &model.Subscription{}, &model.ProxyNode{}, &model.SubscriptionNode{}, &model.AuditLog{}); err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_node_addr_v2 ON proxy_nodes(protocol, host, port)").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("UPDATE users SET total_requests = 0 WHERE total_requests IS NULL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("UPDATE proxy_nodes SET upload_bytes = 0 WHERE upload_bytes IS NULL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("UPDATE proxy_nodes SET download_bytes = 0 WHERE download_bytes IS NULL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("UPDATE proxy_nodes SET total_requests = 0 WHERE total_requests IS NULL").Error; err != nil {
		return nil, err
	}
	return db, nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
