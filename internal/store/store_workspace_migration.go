package store

func (s *Store) migrateWorkspaceSphereSupport() error {
	return nil
}

func (s *Store) migrateWorkspaceConfigSupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	type columnDef struct {
		name string
		sql  string
	}
	defs := []columnDef{
		{name: "mcp_url", sql: `ALTER TABLE workspaces ADD COLUMN mcp_url TEXT NOT NULL DEFAULT ''`},
		{name: "canvas_session_id", sql: `ALTER TABLE workspaces ADD COLUMN canvas_session_id TEXT NOT NULL DEFAULT ''`},
		{name: "chat_model", sql: `ALTER TABLE workspaces ADD COLUMN chat_model TEXT NOT NULL DEFAULT ''`},
		{name: "chat_model_reasoning_effort", sql: `ALTER TABLE workspaces ADD COLUMN chat_model_reasoning_effort TEXT NOT NULL DEFAULT ''`},
		{name: "companion_config_json", sql: `ALTER TABLE workspaces ADD COLUMN companion_config_json TEXT NOT NULL DEFAULT '{}'`},
	}
	for _, def := range defs {
		if tableColumns["workspaces"][def.name] {
			continue
		}
		if _, err := s.db.Exec(def.sql); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`UPDATE workspaces SET companion_config_json = '{}' WHERE trim(companion_config_json) = ''`)
	return err
}

func (s *Store) migrateWorkspaceDailySupport() error {
	tableColumns, err := s.tableColumnSet("workspaces")
	if err != nil {
		return err
	}
	type columnDef struct {
		name string
		sql  string
	}
	defs := []columnDef{
		{name: "is_daily", sql: `ALTER TABLE workspaces ADD COLUMN is_daily INTEGER NOT NULL DEFAULT 0`},
		{name: "daily_date", sql: `ALTER TABLE workspaces ADD COLUMN daily_date TEXT`},
	}
	for _, def := range defs {
		if tableColumns["workspaces"][def.name] {
			continue
		}
		if _, err := s.db.Exec(def.sql); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_daily_date ON workspaces(daily_date) WHERE daily_date IS NOT NULL AND is_daily <> 0`)
	return err
}
