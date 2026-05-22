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
)

const tokenExpiry = 24 * time.Hour

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthService struct {
	store     repository.Store
	jwtSecret []byte
}

func NewAuthService(store repository.Store, jwtSecret string) *AuthService {
	return &AuthService{store: store, jwtSecret: []byte(jwtSecret)}
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

func (s *AuthService) Logout(_ string) error {
	return nil
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
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiry)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.jwtSecret)
}

func (s *AuthService) parseToken(tokenStr string) (*claims, error) {
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

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
