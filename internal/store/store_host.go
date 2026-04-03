package store

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

func (s *Store) AddHost(h HostConfig) (HostConfig, error) {
	if h.Name == "" || h.Hostname == "" || h.Username == "" {
		return HostConfig{}, errors.New("name, hostname, username required")
	}
	if h.Port <= 0 {
		h.Port = 22
	}
	res, err := s.db.Exec(`INSERT INTO hosts (name,hostname,port,username,key_path,project_dir) VALUES (?,?,?,?,?,?)`, h.Name, h.Hostname, h.Port, h.Username, h.KeyPath, h.ProjectDir)
	if err != nil {
		return HostConfig{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetHost(int(id))
}

func (s *Store) GetHost(id int) (HostConfig, error) {
	var h HostConfig
	err := s.db.QueryRow(`SELECT id,name,hostname,port,username,key_path,project_dir FROM hosts WHERE id=?`, id).
		Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.Username, &h.KeyPath, &h.ProjectDir)
	if err != nil {
		return HostConfig{}, err
	}
	return h, nil
}

func (s *Store) ListHosts() ([]HostConfig, error) {
	rows, err := s.db.Query(`SELECT id,name,hostname,port,username,key_path,project_dir FROM hosts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostConfig{}
	for rows.Next() {
		var h HostConfig
		if err := rows.Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.Username, &h.KeyPath, &h.ProjectDir); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) UpdateHost(id int, updates map[string]interface{}) (HostConfig, error) {
	if len(updates) == 0 {
		return s.GetHost(id)
	}
	parts := []string{}
	args := []interface{}{}
	for _, key := range []string{"name", "hostname", "port", "username", "key_path", "project_dir"} {
		if v, ok := updates[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=?", key))
			args = append(args, v)
		}
	}
	if len(parts) == 0 {
		return s.GetHost(id)
	}
	args = append(args, id)
	_, err := s.db.Exec(`UPDATE hosts SET `+stringsJoin(parts, ",")+` WHERE id=?`, args...)
	if err != nil {
		return HostConfig{}, err
	}
	return s.GetHost(id)
}

func (s *Store) DeleteHost(id int) error {
	_, err := s.db.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

func (s *Store) AddRemoteSession(sessionID string, hostID int) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO remote_sessions (session_id,host_id,created_at) VALUES (?,?,?)`, sessionID, hostID, time.Now().Unix())
	return err
}

func (s *Store) DeleteRemoteSession(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM remote_sessions WHERE session_id=?`, sessionID)
	return err
}

func (s *Store) ListRemoteSessions() ([][2]interface{}, error) {
	rows, err := s.db.Query(`SELECT session_id,host_id FROM remote_sessions ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][2]interface{}{}
	for rows.Next() {
		var sid string
		var hid int
		if err := rows.Scan(&sid, &hid); err != nil {
			return nil, err
		}
		out = append(out, [2]interface{}{sid, hid})
	}
	return out, nil
}
