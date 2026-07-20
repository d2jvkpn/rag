package service

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/d2jvkpn/rag/backend/internal/model"
	"github.com/d2jvkpn/rag/backend/internal/repository"
)

var ErrTOTPRequired = errors.New("totp_required")
var ErrUserDisabled = errors.New("user_disabled")
var ErrCannotChangeOwnStatus = errors.New("cannot change your own status")
var ErrUsernameExists = errors.New("username already exists")
var ErrCannotResetOwnPassword = errors.New("cannot reset your own password")

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthService struct {
	store     repository.UserStore
	jwtSecret []byte
	tokenTTL  time.Duration
	blacklist TokenBlacklist
}

func NewAuthService(
	store repository.UserStore,
	jwtSecret string,
	tokenTTL time.Duration,
	bl ...TokenBlacklist,
) *AuthService {
	svc := &AuthService{
		store:     store,
		jwtSecret: []byte(jwtSecret),
		tokenTTL:  tokenTTL,
	}
	if len(bl) > 0 && bl[0] != nil {
		svc.blacklist = bl[0]
	} else {
		svc.blacklist = NewMemoryBlacklist()
	}
	return svc
}

func (s *AuthService) TokenTTL() time.Duration { return s.tokenTTL }

func (s *AuthService) Login(username, password, totpCode string) (model.User, string, error) {
	var (
		user  model.User
		err   error
		now   time.Time
		token string
	)

	user, err = s.store.FindUserByUsername(username)
	if err != nil {
		return model.User{}, "", errors.New("invalid username or password")
	}
	if err := ensureUserActive(user); err != nil {
		return model.User{}, "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, "", errors.New("invalid username or password")
	}

	if user.TOTPEnabled {
		if totpCode == "" {
			return model.User{}, "", ErrTOTPRequired
		}
		if !totp.Validate(totpCode, user.TOTPSecret) {
			return model.User{}, "", errors.New("invalid TOTP code")
		}
	}

	now = time.Now().UTC()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.store.UpdateUser(user); err != nil {
		return model.User{}, "", err
	}

	token, err = s.issueToken(user.UserID)
	if err != nil {
		return model.User{}, "", err
	}
	return user, token, nil
}

func (s *AuthService) SetupTOTP(userID, origin string) (secret, qrURL string, err error) {
	var (
		user   model.User
		issuer string
		key    *otp.Key
	)

	user, err = s.store.GetUser(userID)
	if err != nil {
		return "", "", err
	}
	issuer = "RAG"
	if origin != "" {
		issuer = "RAG@" + origin
	}
	key, err = totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: user.Username,
	})
	if err != nil {
		return "", "", err
	}
	user.TOTPSecret = key.Secret()
	user.TOTPEnabled = false
	user.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateUser(user); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func (s *AuthService) EnableTOTP(userID, code string) error {
	var (
		user model.User
		err  error
	)

	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if user.TOTPSecret == "" {
		return errors.New("TOTP not set up; call setup first")
	}
	if !totp.Validate(code, user.TOTPSecret) {
		return errors.New("invalid TOTP code")
	}
	user.TOTPEnabled = true
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) DisableTOTP(userID, code string) error {
	var (
		user model.User
		err  error
	)

	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if !user.TOTPEnabled {
		return errors.New("TOTP is not enabled")
	}
	if !totp.Validate(code, user.TOTPSecret) {
		return errors.New("invalid TOTP code")
	}
	user.TOTPEnabled = false
	user.TOTPSecret = ""
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) Logout(tokenStr string) error {
	var (
		c   *claims
		err error
	)

	if tokenStr == "" {
		return nil
	}
	c, err = s.parseTokenRaw(tokenStr)
	if err != nil {
		return nil // already expired or invalid — nothing to revoke
	}
	if c.ID == "" {
		return nil
	}
	return s.blacklist.Block(c.ID, c.ExpiresAt.Time)
}

func (s *AuthService) ChangePassword(userID, oldPassword, newPassword string) error {
	var (
		user model.User
		err  error
		hash []byte
	)

	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash), []byte(oldPassword),
	); err != nil {
		return errors.New("incorrect current password")
	}
	hash, err = bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) Me(tokenStr string) (model.User, error) {
	var (
		c    *claims
		err  error
		user model.User
	)

	c, err = s.parseToken(tokenStr)
	if err != nil {
		return model.User{}, err
	}
	user, err = s.store.GetUser(c.UserID)
	if err != nil {
		return model.User{}, err
	}
	if err := ensureUserActive(user); err != nil {
		return model.User{}, err
	}
	return user, nil
}

// Permissions returns the effective permission list for a user.
// Both nil and empty slice mean no permissions (zero-default model).
func (s *AuthService) Permissions(user model.User) []string {
	if len(user.Permissions) == 0 {
		return []string{}
	}
	return append([]string(nil), user.Permissions...)
}

func (s *AuthService) HasPermission(user model.User, permission string) bool {
	return slices.Contains(user.Permissions, permission)
}

func (s *AuthService) ListUsers() []model.User {
	return s.store.ListUsers()
}

func (s *AuthService) SetUserStatus(actor model.User, userID, status string) error {
	var (
		user model.User
		err  error
	)

	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if user.UserID == actor.UserID {
		return ErrCannotChangeOwnStatus
	}
	if user.Status == status {
		return nil
	}
	user.Status = status
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) CreateUser(username, password string, permissions []string) (model.User, error) {
	var (
		hash []byte
		err  error
		now  time.Time
		user model.User
	)

	if _, err := s.store.FindUserByUsername(username); err == nil {
		return model.User{}, ErrUsernameExists
	} else if !errors.Is(err, repository.ErrNotFound) {
		return model.User{}, err
	}
	hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, err
	}
	now = time.Now().UTC()
	if permissions == nil {
		permissions = []string{}
	}
	user = model.User{
		UserID:       uuid.Must(uuid.NewV7()).String(),
		Username:     username,
		PasswordHash: string(hash),
		Status:       "active",
		Permissions:  permissions,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateUser(user); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func (s *AuthService) SetUserPermissions(userID string, permissions []string) error {
	var (
		user model.User
		err  error
	)

	for _, p := range permissions {
		if !slices.Contains(repository.AllPermissions, p) {
			return fmt.Errorf("unknown permission: %s", p)
		}
	}
	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if permissions == nil {
		permissions = []string{}
	}
	user.Permissions = permissions
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) SetUserPassword(actor model.User, userID, newPassword string) error {
	var (
		user model.User
		err  error
		hash []byte
	)

	if userID == actor.UserID {
		return ErrCannotResetOwnPassword
	}
	user, err = s.store.GetUser(userID)
	if err != nil {
		return err
	}
	hash, err = bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now().UTC()
	return s.store.UpdateUser(user)
}

func (s *AuthService) issueToken(userID string) (string, error) {
	var (
		now time.Time
		c   claims
	)

	now = time.Now().UTC()
	c = claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.Must(uuid.NewV7()).String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.jwtSecret)
}

// parseTokenRaw validates signature and expiry but does NOT check the blacklist.
// Used by Logout to extract claims from a token that may have just been revoked.
func (s *AuthService) parseTokenRaw(tokenStr string) (*claims, error) {
	var (
		t   *jwt.Token
		err error
		c   *claims
		ok  bool
	)

	t, err = jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok = t.Claims.(*claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// parseToken validates the token and checks the blacklist.
func (s *AuthService) parseToken(tokenStr string) (*claims, error) {
	var (
		c   *claims
		err error
	)

	c, err = s.parseTokenRaw(tokenStr)
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

func ensureUserActive(user model.User) error {
	if user.Status == "" || user.Status == "active" {
		return nil
	}
	if user.Status == "disabled" {
		return ErrUserDisabled
	}
	return fmt.Errorf("user status %q is not allowed", user.Status)
}
