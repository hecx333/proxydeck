package model

import "time"

type User struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	UID            string     `json:"uid" gorm:"uniqueIndex;size:128;not null"`
	PasswordHash   string     `json:"-" gorm:"not null"`
	Enabled        bool       `json:"enabled" gorm:"default:true"`
	QuotaBytes     int64      `json:"quota_bytes"`
	UsedBytes      int64      `json:"used_bytes"`
	TotalRequests  int64      `json:"total_requests" gorm:"default:0"`
	MaxConcurrency int        `json:"max_concurrency"`
	ExpiredAt      *time.Time `json:"expired_at"`
	Remark         string     `json:"remark"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Subscription struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	Name                string     `json:"name" gorm:"size:128;not null"`
	Type                string     `json:"type" gorm:"size:32;not null"`
	URL                 string     `json:"url" gorm:"not null"`
	Enabled             bool       `json:"enabled" gorm:"default:true"`
	SyncIntervalSeconds int        `json:"sync_interval_seconds"`
	LastSyncAt          *time.Time `json:"last_sync_at"`
	Remark              string     `json:"remark"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ProxyNode struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	NodeKey             string     `json:"node_key" gorm:"uniqueIndex;size:255;not null"`
	Protocol            string     `json:"protocol" gorm:"size:32;not null;uniqueIndex:idx_node_addr_v2"`
	Host                string     `json:"host" gorm:"size:255;not null;uniqueIndex:idx_node_addr;uniqueIndex:idx_node_addr_v2"`
	Port                int        `json:"port" gorm:"not null;uniqueIndex:idx_node_addr;uniqueIndex:idx_node_addr_v2"`
	UpstreamUsernameEnc string     `json:"-" gorm:"column:upstream_username_enc"`
	UpstreamPasswordEnc string     `json:"-" gorm:"column:upstream_password_enc"`
	TLSEnabled          bool       `json:"tls_enabled"`
	TLSSkipVerify       bool       `json:"tls_skip_verify"`
	ServerName          string     `json:"server_name"`
	Tag                 string     `json:"tag"`
	ExpectedRegion      string     `json:"expected_region"`
	DetectedRegion      string     `json:"detected_region"`
	City                string     `json:"city"`
	Country             string     `json:"country"`
	ASN                 string     `json:"asn"`
	Org                 string     `json:"org"`
	ISP                 string     `json:"isp"`
	ExitIP              string     `json:"exit_ip"`
	LatencyMS           int64      `json:"latency_ms"`
	Healthy             bool       `json:"healthy" gorm:"default:false"`
	Disabled            bool       `json:"disabled" gorm:"default:false"`
	FailCount           int        `json:"fail_count"`
	LastCheckAt         *time.Time `json:"last_check_at"`
	SubscriptionID      uint       `json:"subscription_id"`
	RawJSON             string     `json:"raw_json" gorm:"type:text"`
	UploadBytes         int64      `json:"upload_bytes" gorm:"default:0"`
	DownloadBytes       int64      `json:"download_bytes" gorm:"default:0"`
	TotalRequests       int64      `json:"total_requests" gorm:"default:0"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SubscriptionNode struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"index"`
	NodeID         uint      `json:"node_id" gorm:"index"`
	RawTag         string    `json:"raw_tag"`
	AliasTag       string    `json:"alias_tag"`
	CreatedAt      time.Time `json:"created_at"`
}

type AuditLog struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	Operator   string    `json:"operator"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail" gorm:"type:text"`
	CreatedAt  time.Time `json:"created_at"`
}
