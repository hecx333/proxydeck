package quota

import (
	"context"
	"errors"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"gorm.io/gorm"
)

var (
	ErrUserDisabled        = errors.New("user disabled")
	ErrUserExpired         = errors.New("user expired")
	ErrQuotaExceeded       = errors.New("quota exceeded")
	ErrConcurrencyExceeded = errors.New("concurrency exceeded")
)

type Service struct {
	db    *gorm.DB
	store *redisstore.Store
}

func NewService(db *gorm.DB, store *redisstore.Store) *Service {
	return &Service{db: db, store: store}
}

func (s *Service) Authenticate(ctx context.Context, uid, password string) (*model.User, error) {
	_ = ctx
	var user model.User
	if err := s.db.Where("uid = ?", uid).First(&user).Error; err != nil {
		return nil, err
	}
	if !user.Enabled {
		return nil, ErrUserDisabled
	}
	if user.ExpiredAt != nil && time.Now().After(*user.ExpiredAt) {
		return nil, ErrUserExpired
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		return nil, err
	}
	pendingUsage := s.store.UserPendingUsage(ctx, uid)
	if user.QuotaBytes > 0 && user.UsedBytes+pendingUsage >= user.QuotaBytes {
		return nil, ErrQuotaExceeded
	}
	return &user, nil
}

func (s *Service) Acquire(ctx context.Context, user *model.User) error {
	current, err := s.store.Client.Incr(ctx, redisstore.ConnKey(user.UID)).Result()
	if err != nil {
		return err
	}
	if user.MaxConcurrency > 0 && current > int64(user.MaxConcurrency) {
		_ = s.store.Client.Decr(ctx, redisstore.ConnKey(user.UID)).Err()
		return ErrConcurrencyExceeded
	}
	return nil
}

func (s *Service) Release(ctx context.Context, uid string) {
	current, err := s.store.Client.Get(ctx, redisstore.ConnKey(uid)).Int64()
	if err != nil || current <= 1 {
		_ = s.store.Client.Del(ctx, redisstore.ConnKey(uid)).Err()
		return
	}
	_ = s.store.Client.Decr(ctx, redisstore.ConnKey(uid)).Err()
}

func (s *Service) CurrentConcurrency(ctx context.Context, uid string) int64 {
	value, err := s.store.Client.Get(ctx, redisstore.ConnKey(uid)).Int64()
	if err != nil {
		return 0
	}
	return value
}

func (s *Service) CleanupUserRuntime(ctx context.Context, uid string) error {
	keys := []string{
		redisstore.ConnKey(uid),
		redisstore.UsageKey(uid, "upload"),
		redisstore.UsageKey(uid, "download"),
		redisstore.RequestsKey(uid),
	}
	stickyKeys, err := s.store.Client.Keys(ctx, "sticky:"+uid+":*").Result()
	if err != nil {
		return err
	}
	keys = append(keys, stickyKeys...)
	return s.store.Client.Del(ctx, keys...).Err()
}
