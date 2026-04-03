package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

const pbkdfIter = 600000

func hashPassword(password, salt string) string {
	data := []byte(password + ":" + salt)
	sum := sha256.Sum256(data)
	for i := 0; i < pbkdfIter/10000; i++ {
		next := sha256.Sum256(sum[:])
		sum = next
	}
	return hex.EncodeToString(sum[:])
}

func (s *Store) HasAdminPassword() bool {
	var c int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&c)
	return c > 0
}

func (s *Store) SetAdminPassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	salt := randomHex(16)
	h := hashPassword(password, salt)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM admin`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM auth_sessions`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO admin (id,pw_hash,pw_salt) VALUES (1,?,?)`, h, salt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) VerifyAdminPassword(password string) bool {
	var h, salt string
	if err := s.db.QueryRow(`SELECT pw_hash,pw_salt FROM admin WHERE id=1`).Scan(&h, &salt); err != nil {
		return false
	}
	cand := hashPassword(password, salt)
	return hmac.Equal([]byte(cand), []byte(h))
}

func (s *Store) AddAuthSession(token string) error {
	if token == "" {
		return errors.New("empty token")
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO auth_sessions (token,created_at) VALUES (?,?)`, token, time.Now().Unix())
	return err
}

func (s *Store) HasAuthSession(token string) bool {
	if token == "" {
		return false
	}
	var one int
	if err := s.db.QueryRow(`SELECT 1 FROM auth_sessions WHERE token=?`, token).Scan(&one); err != nil {
		return false
	}
	return true
}

func (s *Store) DeleteAuthSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE token=?`, token)
	return err
}
