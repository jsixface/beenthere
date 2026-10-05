package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Dawarich user statuses (users.status): 0 inactive, 1 active, 2 trial, 3 pending_payment.
type User struct {
	ID            int64
	Email         string
	APIKey        string
	Admin         bool
	Status        int
	ActiveUntil   *time.Time
	DeletedAt     *time.Time
	EncryptedPass string
	Theme         string
	Settings      map[string]any
	PointsCount   int
	CreatedAt     time.Time
	OTPRequired   bool
	Locked        bool // Devise :lockable
}

const userCols = `id, email, api_key, coalesce(admin,false), coalesce(status,0), active_until, deleted_at,
	encrypted_password, theme, coalesce(settings,'{}'::jsonb), points_count, created_at, otp_required_for_login, locked_at IS NOT NULL`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var settings []byte
	if err := row.Scan(&u.ID, &u.Email, &u.APIKey, &u.Admin, &u.Status, &u.ActiveUntil, &u.DeletedAt,
		&u.EncryptedPass, &u.Theme, &settings, &u.PointsCount, &u.CreatedAt, &u.OTPRequired, &u.Locked); err != nil {
		return nil, notFound(err)
	}
	u.Settings = map[string]any{}
	_ = json.Unmarshal(settings, &u.Settings)
	return &u, nil
}

func (s *Store) UserByAPIKey(ctx context.Context, key string) (*User, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	return scanUser(s.Pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE api_key = $1 AND deleted_at IS NULL`, key))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.Pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.Pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL`, email))
}

// CheckPassword verifies a Devise bcrypt hash (no pepper is configured upstream).
func (u *User) CheckPassword(pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.EncryptedPass), []byte(pw)) == nil
}

// Active mirrors the API's authenticate_active_api_user! checks.
func (u *User) Active() bool {
	if u.Locked {
		return false
	}
	if u.Status == 0 || u.Status == 3 { // inactive, pending_payment
		return false
	}
	return u.ActiveUntil == nil || u.ActiveUntil.After(time.Now())
}

func (s *Store) TouchSignIn(ctx context.Context, id int64, ip string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET sign_in_count = sign_in_count + 1,
		last_sign_in_at = current_sign_in_at, last_sign_in_ip = current_sign_in_ip,
		current_sign_in_at = now(), current_sign_in_ip = $2 WHERE id = $1`, id, ip)
	return err
}

// MergeSettings shallow-merges the given keys into users.settings.
func (s *Store) MergeSettings(ctx context.Context, id int64, patch map[string]any) error {
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx,
		`UPDATE users SET settings = coalesce(settings,'{}'::jsonb) || $2::jsonb, updated_at = now() WHERE id = $1`, id, b)
	return err
}

// SettingString returns a string-ish setting with a default.
func (u *User) SettingInt(key string, def int) int {
	switch v := u.Settings[key].(type) {
	case float64:
		return int(v)
	case string:
		n := 0
		for _, c := range v {
			if c < '0' || c > '9' {
				return def
			}
			n = n*10 + int(c-'0')
		}
		if v == "" {
			return def
		}
		return n
	}
	return def
}

func (u *User) Timezone() string {
	if v, ok := u.Settings["timezone"].(string); ok && v != "" {
		return v
	}
	return "UTC"
}

// CreateUser inserts an active user with a bcrypt (cost 12, Devise-compatible) password and a fresh API key.
func (s *Store) CreateUser(ctx context.Context, email, password string, admin bool) (apiKey string, err error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	apiKey = hex.EncodeToString(buf)
	_, err = s.Pool.Exec(ctx, `INSERT INTO users (email, encrypted_password, api_key, admin, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,1,now(),now())`, email, string(hash), apiKey, admin)
	return apiKey, err
}
