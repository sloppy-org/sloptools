package store

import "testing"

func contextIDByNameForTest(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	var contextID int64
	if err := s.db.QueryRow(`SELECT id FROM contexts WHERE lower(name) = lower(?)`, name).Scan(&contextID); err != nil {
		t.Fatalf("context lookup %q: %v", name, err)
	}
	return contextID
}
