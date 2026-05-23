package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/uuid"
)

const tokenExpiry = 24 * time.Hour

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthService struct {
	store     repository.Store
	jwtSecret []byte
	blacklist TokenBlacklist
}

func NewAuthService(store repository.Store, jwtSecret string, bl ...TokenBlacklist) *AuthService {
	svc := &AuthService{store: store, jwtSecret: []byte(jwtSecret)}
	if len(bl) > 0 && bl[0] != nil {
		svc.blacklist = bl[0]
	} else {
		svc.blacklist = NewMemoryBlacklist()
	}
	return svc
}

func (s *AuthService) Login(username, password string) (model.User, string, error) {
	user, err := s.store.FindUserByUsername(username)
	if err != nil {
		return model.User{}, "", errors.New("invalid username or password")
	}
	if user.PasswordHash != hashPassword(password) {
		return model.User{}, "", errors.New("invalid username or password")
	}

	now := time.Now().UTC()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.store.UpdateUser(user); err != nil {
		return model.User{}, "", err
	}

	token, err := s.issueToken(user.UserID)
	if err != nil {
		return model.User{}, "", err
	}
	return user, token, nil
}

func (s *AuthService) Logout(tokenStr string) error {
	if tokenStr == "" {
		return nil
	}
	c, err := s.parseTokenRaw(tokenStr)
	if err != nil {
		return nil // already expired or invalid — nothing to revoke
	}
	if c.ID == "" {
		return nil
	}
	return s.blacklist.Block(c.ID, c.ExpiresAt.Time)
}

func (s *AuthService) ChangePassword(userID, oldPassword, newPassword string) error {
	user, err := s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if user.PasswordHash != hashPassword(oldPassword) {
		return errors.New("incorrect current password")
	}
	user.PasswordHash = hashPassword(newPassword)
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) Me(tokenStr string) (model.User, error) {
	c, err := s.parseToken(tokenStr)
	if err != nil {
		return model.User{}, err
	}
	return s.store.GetUser(c.UserID)
}

func (s *AuthService) issueToken(userID string) (string, error) {
	now := time.Now().UTC()
	c := claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewV7(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiry)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.jwtSecret)
}

// parseTokenRaw validates signature and expiry but does NOT check the blacklist.
// Used by Logout to extract claims from a token that may have just been revoked.
func (s *AuthService) parseTokenRaw(tokenStr string) (*claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// parseToken validates the token and checks the blacklist.
func (s *AuthService) parseToken(tokenStr string) (*claims, error) {
	c, err := s.parseTokenRaw(tokenStr)
	if err != nil {
		return nil, err
	}
	if c.ID != "" {
		blocked, err := s.blacklist.IsBlocked(c.ID)
		if err != nil {
			return nil, fmt.Errorf("blacklist check: %w", err)
		}
		if blocked {
			return nil, errors.New("token has been revoked")
		}
	}
	return c, nil
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
