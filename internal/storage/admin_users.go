package storage

import (
	"database/sql"
	"fmt"
)

const adminUserColumns = `id, username, password, enabled, created_at, updated_at, last_login_at, timezone`

func ListAdminUsers(db *sql.DB) ([]AdminUser, error) {
	rows, err := db.Query(`SELECT ` + adminUserColumns + ` FROM admin_users ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Timezone); err != nil {
			return nil, fmt.Errorf("scan admin user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func GetAdminUserByUsername(db *sql.DB, username string) (*AdminUser, error) {
	var u AdminUser
	err := db.QueryRow(`SELECT `+adminUserColumns+` FROM admin_users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Password, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Timezone)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get admin user: %w", err)
	}
	return &u, nil
}

func GetAdminUserByID(db *sql.DB, id string) (*AdminUser, error) {
	var u AdminUser
	err := db.QueryRow(`SELECT `+adminUserColumns+` FROM admin_users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Password, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Timezone)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get admin user: %w", err)
	}
	return &u, nil
}

func CreateAdminUser(db *sql.DB, u *AdminUser) error {
	_, err := db.Exec(
		`INSERT INTO admin_users (id, username, password, enabled, created_at, updated_at, last_login_at, timezone) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Password, boolToInt(u.Enabled), u.CreatedAt, u.UpdatedAt, u.LastLoginAt, u.Timezone,
	)
	if err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}
	return nil
}

func UpdateAdminUserLogin(db *sql.DB, id string, lastLoginAt string) error {
	_, err := db.Exec(`UPDATE admin_users SET last_login_at = ?, updated_at = ? WHERE id = ?`, lastLoginAt, lastLoginAt, id)
	if err != nil {
		return fmt.Errorf("update admin user login: %w", err)
	}
	return nil
}

// UpdateAdminUserTimezone stores the user's timezone (IANA name, empty = server local).
func UpdateAdminUserTimezone(db *sql.DB, id string, tz string, updatedAt string) error {
	_, err := db.Exec(`UPDATE admin_users SET timezone = ?, updated_at = ? WHERE id = ?`, tz, updatedAt, id)
	if err != nil {
		return fmt.Errorf("update admin user timezone: %w", err)
	}
	return nil
}

func CountAdminUsers(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admin users: %w", err)
	}
	return count, nil
}
