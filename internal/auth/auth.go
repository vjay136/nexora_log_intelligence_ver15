// Package auth implements JWT-based authentication with admin-only user creation.
// Users are stored in a flat JSON file (no external DB dependency for the agent).
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Role constants. Authorization is permission-based; role levels are not used
// to let parser authors or reviewers inherit each other's write capabilities.
const (
	RoleViewer       = "viewer"
	RoleParserAuthor = "parser_author"
	RoleReviewer     = "reviewer"
	RoleOps          = "ops"
	RoleAdmin        = "admin"
)

var validRoles = map[string]bool{
	RoleViewer: true, RoleParserAuthor: true, RoleReviewer: true,
	RoleOps: true, RoleAdmin: true,
}

func IsValidRole(role string) bool { return validRoles[role] }
func Roles() []string {
	return []string{RoleViewer, RoleParserAuthor, RoleReviewer, RoleOps, RoleAdmin}
}

// User is a team member account.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"password_hash"` // bcrypt-equivalent: HMAC-SHA256 + salt
	Salt         string    `json:"salt"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	Active       bool      `json:"active"`
	LastLogin    time.Time `json:"last_login,omitempty"`
}

// userStore is the on-disk format.
type userStore struct {
	Users     []*User `json:"users"`
	JWTSecret string  `json:"jwt_secret"`
}

// Store manages users and JWT secrets.
type Store struct {
	mu        sync.RWMutex
	path      string
	users     map[string]*User // keyed by username
	jwtSecret []byte
}

// NewStore loads an existing store or bootstraps one from the
// NLI_BOOTSTRAP_ADMIN_PASSWORD environment variable.
func NewStore(path string) (*Store, error) {
	return NewStoreWithBootstrap(path, os.Getenv("NLI_BOOTSTRAP_ADMIN_PASSWORD"))
}

// NewStoreWithBootstrap is used by the controller and tests to supply the
// one-time administrator password without persisting it in configuration.
func NewStoreWithBootstrap(path, bootstrapPassword string) (*Store, error) {
	s := &Store{path: path, users: make(map[string]*User)}
	if err := s.load(bootstrapPassword); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load(bootstrapPassword string) error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if len(bootstrapPassword) < 12 {
			return fmt.Errorf("NLI_BOOTSTRAP_ADMIN_PASSWORD must contain at least 12 characters on first start")
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		s.jwtSecret = secret

		admin, err := newUser("admin", "admin@nli.local", RoleAdmin, bootstrapPassword, "system")
		if err != nil {
			return err
		}
		s.users["admin"] = admin
		return s.save()
	}
	if err != nil {
		return err
	}
	var store userStore
	if err := json.Unmarshal(data, &store); err != nil {
		return err
	}
	secretBytes, err := hex.DecodeString(store.JWTSecret)
	if err != nil {
		return err
	}
	s.jwtSecret = secretBytes
	for _, u := range store.Users {
		s.users[u.Username] = u
	}
	return nil
}

func (s *Store) save() error {
	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	store := userStore{
		Users:     users,
		JWTSecret: hex.EncodeToString(s.jwtSecret),
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newUser(username, email, role, password, createdBy string) (*User, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	saltHex := hex.EncodeToString(salt)
	hash := hashPassword(password, saltHex)
	id := make([]byte, 8)
	rand.Read(id)
	return &User{
		ID:           hex.EncodeToString(id),
		Username:     username,
		Email:        email,
		Role:         role,
		PasswordHash: hash,
		Salt:         saltHex,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		Active:       true,
	}, nil
}

const passwordIterations = 210000

func hashPassword(password, salt string) string {
	key := pbkdf2SHA256([]byte(password), []byte(salt), passwordIterations, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s", passwordIterations, hex.EncodeToString(key))
}

func verifyPassword(password, salt, encoded string) (valid bool, legacy bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) == 3 && parts[0] == "pbkdf2-sha256" {
		iterations, err := strconv.Atoi(parts[1])
		expected, decodeErr := hex.DecodeString(parts[2])
		if err != nil || decodeErr != nil || iterations < 100000 || iterations > 2000000 {
			return false, false
		}
		actual := pbkdf2SHA256([]byte(password), []byte(salt), iterations, len(expected))
		return hmac.Equal(actual, expected), false
	}
	// Compatibility with v0.4's one-round HMAC. A successful login upgrades it.
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(password))
	legacyHash := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(legacyHash), []byte(encoded)), true
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for index := range t {
				t[index] ^= u[index]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}

// CreateUser creates a new user. Only admins may call this.
func (s *Store) CreateUser(callerRole, username, email, role, password string) (*User, error) {
	if callerRole != RoleAdmin {
		return nil, fmt.Errorf("only admins can create users")
	}
	if username == "" || email == "" {
		return nil, fmt.Errorf("username, email and password are required")
	}
	if len(password) < 12 {
		return nil, fmt.Errorf("password must contain at least 12 characters")
	}
	if !IsValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; exists {
		return nil, fmt.Errorf("username already exists")
	}

	u, err := newUser(username, email, role, password, callerRole)
	if err != nil {
		return nil, err
	}
	s.users[username] = u
	return u, s.save()
}

// Authenticate verifies credentials and returns a JWT token.
func (s *Store) Authenticate(username, password string) (string, *User, error) {
	s.mu.RLock()
	u, ok := s.users[username]
	s.mu.RUnlock()

	if !ok || !u.Active {
		return "", nil, fmt.Errorf("invalid credentials")
	}
	valid, legacy := verifyPassword(password, u.Salt, u.PasswordHash)
	if !valid {
		return "", nil, fmt.Errorf("invalid credentials")
	}

	token, err := s.issueToken(u)
	if err != nil {
		return "", nil, err
	}

	s.mu.Lock()
	if legacy {
		u.PasswordHash = hashPassword(password, u.Salt)
	}
	u.LastLogin = time.Now().UTC()
	saveErr := s.save()
	s.mu.Unlock()
	if saveErr != nil {
		return "", nil, saveErr
	}

	return token, u, nil
}

// ValidateToken verifies a JWT and returns the username and role.
func (s *Store) ValidateToken(token string) (username, role string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid token format")
	}

	// Verify signature in constant time.
	sig := hmacSign(s.jwtSecret, parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(sig), []byte(parts[2])) {
		return "", "", fmt.Errorf("invalid token signature")
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("invalid token payload")
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", err
	}

	expiry, ok := claims["exp"].(float64)
	if !ok || time.Now().Unix() >= int64(expiry) {
		return "", "", fmt.Errorf("token expired or missing expiry")
	}

	username, _ = claims["sub"].(string)
	if username == "" {
		return "", "", fmt.Errorf("token subject missing")
	}
	s.mu.RLock()
	user, exists := s.users[username]
	if exists {
		role = user.Role
	}
	active := exists && user.Active
	s.mu.RUnlock()
	if !active {
		return "", "", fmt.Errorf("account inactive")
	}
	return username, role, nil
}

// ListUsers returns all users (without password hashes).
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		cp := *u
		cp.PasswordHash = ""
		cp.Salt = ""
		out = append(out, &cp)
	}
	return out
}

// DeactivateUser disables a user account (admin only).
func (s *Store) DeactivateUser(callerRole, username string) error {
	if callerRole != RoleAdmin {
		return fmt.Errorf("only admins can deactivate users")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.Active = false
	return s.save()
}

// issueToken generates a minimal JWT (header.payload.sig using HMAC-SHA256).
func (s *Store) issueToken(u *User) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]interface{}{
		"sub":  u.Username,
		"role": u.Role,
		"uid":  u.ID,
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	sig := hmacSign(s.jwtSecret, header+"."+encodedPayload)
	return header + "." + encodedPayload + "." + sig, nil
}

func hmacSign(secret []byte, data string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ResetPassword rotates one existing account's password for an offline,
// host-administrator recovery flow. Callers must enforce OS-level authorization.
func (s *Store) ResetPassword(username, password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must contain at least 12 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	user.Salt = hex.EncodeToString(salt)
	user.PasswordHash = hashPassword(password, user.Salt)
	user.Active = true
	return s.save()
}
