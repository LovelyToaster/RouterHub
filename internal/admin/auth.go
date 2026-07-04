package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

type contextKey string

const adminUserContextKey contextKey = "admin_user"

// SetupStatus returns whether the system has been initialized (has admin users).
func SetupStatus(db *sql.DB) (bool, error) {
	count, err := storage.CountAdminUsers(db)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// InitSetup creates the initial admin user.
func InitSetup(db *sql.DB, username, password string) error {
	// Check if already setup
	exists, err := SetupStatus(db)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("already initialized")
	}

	now := storage.Now()
	user := &storage.AdminUser{
		ID:        uuid.New().String(),
		Username:  username,
		Password:  hashPassword(password),
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return storage.CreateAdminUser(db, user)
}

// Login authenticates a user and returns a session token.
func Login(db *sql.DB, username, password string) (*storage.AdminSession, error) {
	user, err := storage.GetAdminUserByUsername(db, username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if !user.Enabled {
		return nil, fmt.Errorf("user disabled")
	}

	if user.Password != hashPassword(password) {
		return nil, fmt.Errorf("invalid credentials")
	}

	now := storage.Now()
	token, err := generateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	session := &storage.AdminSession{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		CreatedAt: now,
	}

	if err := storage.CreateAdminSession(db, session); err != nil {
		return nil, err
	}

	// Update last login
	_ = storage.UpdateAdminUserLogin(db, user.ID, now)

	return session, nil
}

// Logout invalidates a session.
func Logout(db *sql.DB, token string) error {
	return storage.DeleteAdminSession(db, token)
}

// ValidateSession checks if a session token is valid and returns the associated user.
func ValidateSession(db *sql.DB, token string) (*storage.AdminUser, error) {
	session, err := storage.GetAdminSessionByToken(db, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	// Check expiry
	expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		// Clean up expired session
		_ = storage.DeleteAdminSession(db, token)
		return nil, nil
	}

	// Get user by ID
	var u storage.AdminUser
	err = db.QueryRow(`SELECT id, username, password, enabled, created_at, updated_at, last_login_at, timezone FROM admin_users WHERE id = ?`, session.UserID).
		Scan(&u.ID, &u.Username, &u.Password, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Timezone)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if !u.Enabled {
		return nil, nil
	}

	// Touch session
	now := storage.Now()
	_ = storage.TouchAdminSession(db, session.ID, now)

	return &u, nil
}

// AdminAuthMiddleware validates admin session tokens.
func AdminAuthMiddleware(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractSessionToken(r)
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			user, err := ValidateSession(db, token)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
				return
			}
			if user == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
				return
			}

			ctx := context.WithValue(r.Context(), adminUserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAdminUserFromContext retrieves the admin user from context.
func GetAdminUserFromContext(ctx context.Context) *storage.AdminUser {
	user, _ := ctx.Value(adminUserContextKey).(*storage.AdminUser)
	return user
}

func extractSessionToken(r *http.Request) string {
	// Try Authorization: Bearer <token>
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback: ?token=... (used by EventSource which cannot set headers)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}

func hashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// JSON response helpers
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
