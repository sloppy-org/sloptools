package store

import "testing"

func TestStoreAuthHelpersHandleMissingAndDeletedSessions(t *testing.T) {
	s := newTestStore(t)

	if s.VerifyAdminPassword("anything") {
		t.Fatal("VerifyAdminPassword() = true, want false before password is set")
	}
	if s.HasAuthSession("") {
		t.Fatal("HasAuthSession(\"\") = true, want false")
	}
	if s.HasAuthSession("missing") {
		t.Fatal("HasAuthSession(missing) = true, want false")
	}
	if err := s.AddAuthSession("tok-edge"); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	if err := s.DeleteAuthSession("tok-edge"); err != nil {
		t.Fatalf("DeleteAuthSession() error: %v", err)
	}
	if s.HasAuthSession("tok-edge") {
		t.Fatal("HasAuthSession(tok-edge) = true, want false after delete")
	}
}

func TestStoreUpdateHostNoopAndUnknownFieldsKeepExistingRecord(t *testing.T) {
	s := newTestStore(t)

	original, err := s.AddHost(HostConfig{
		Name:       "alpha",
		Hostname:   "alpha.local",
		Port:       2202,
		Username:   "u1",
		KeyPath:    "/tmp/key1",
		ProjectDir: "/tmp/p1",
	})
	if err != nil {
		t.Fatalf("AddHost() error: %v", err)
	}

	unchanged, err := s.UpdateHost(original.ID, map[string]interface{}{})
	if err != nil {
		t.Fatalf("UpdateHost(empty) error: %v", err)
	}
	if unchanged != original {
		t.Fatalf("UpdateHost(empty) = %+v, want %+v", unchanged, original)
	}

	ignored, err := s.UpdateHost(original.ID, map[string]interface{}{"unsupported": "value"})
	if err != nil {
		t.Fatalf("UpdateHost(unsupported) error: %v", err)
	}
	if ignored != original {
		t.Fatalf("UpdateHost(unsupported) = %+v, want %+v", ignored, original)
	}
}
