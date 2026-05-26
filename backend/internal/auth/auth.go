package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func HashPassword(password string) (string, error) {
	data, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(data), err
}

func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(secret string) (*Cipher, error) {
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	encrypted := c.aead.Seal(nonce, nonce, []byte(raw), nil)
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func (c *Cipher) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	size := c.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("invalid ciphertext")
	}
	plain, err := c.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

type AdminSessionManager struct {
	store *redisstore.Store
	ttl   time.Duration
}

func NewAdminSessionManager(store *redisstore.Store, ttl time.Duration) *AdminSessionManager {
	return &AdminSessionManager{store: store, ttl: ttl}
}

func (m *AdminSessionManager) Create(ctx context.Context, username string) (string, error) {
	token := uuid.NewString()
	return token, m.store.Client.Set(ctx, "admin:sess:"+token, username, m.ttl).Err()
}

func (m *AdminSessionManager) Get(ctx context.Context, token string) (string, error) {
	return m.store.Client.Get(ctx, "admin:sess:"+token).Result()
}

func (m *AdminSessionManager) Delete(ctx context.Context, token string) error {
	return m.store.Client.Del(ctx, "admin:sess:"+token).Err()
}

func EnsureDefaultAdmin(db *gorm.DB, username, password string) error {
	var count int64
	if err := db.Model(&model.User{}).Where("uid = ?", model.AdminUIDPrefix+username).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return db.Create(&model.User{
		UID:          model.AdminUIDPrefix + username,
		PasswordHash: hash,
		Enabled:      true,
		Remark:       fmt.Sprintf("bootstrap admin %s", username),
	}).Error
}
