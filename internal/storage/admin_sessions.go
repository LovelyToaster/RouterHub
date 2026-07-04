package storage

import (
	"database/sql"
	"fmt"
)

func CreateAdminSession(db *sql.DB, s *AdminSession) error {
	_, err := db.Exec(
		`INSERT INTO admin_sessions (id, user_id, token, expires_at, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.Token, s.ExpiresAt, s.CreatedAt, s.LastSeenAt,
	)
	if err != nil {
		return fmt.Errorf("create admin session: %w", err)
	}
	return nil
}

func GetAdminSessionByToken(db *sql.DB, token string) (*AdminSession, error) {
	var s AdminSession
	err := db.QueryRow(`SELECT id, user_id, token, expires_at, created_at, last_seen_at FROM admin_sessions WHERE token = ?`, token).
		Scan(&s.ID, &s.UserID, &s.Token, &s.ExpiresAt, &s.CreatedAt, &s.LastSeenAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get admin session: %w", err)
	}
	return &s, nil
}

func TouchAdminSession(db *sql.DB, id string, lastSeenAt string) error {
	_, err := db.Exec(`UPDATE admin_sessions SET last_seen_at = ? WHERE id = ?`, lastSeenAt, id)
	if err != nil {
		return fmt.Errorf("touch admin session: %w", err)
	}
	return nil
}

func DeleteAdminSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM admin_sessions WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("delete admin session: %w", err)
	}
	return nil
}
