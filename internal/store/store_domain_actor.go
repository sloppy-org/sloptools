package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
)

func (s *Store) CreateActor(name, kind string) (Actor, error) {
	return s.CreateActorWithOptions(name, kind, ActorOptions{})
}

func (s *Store) CreateActorWithOptions(name, kind string, opts ActorOptions) (Actor, error) {
	cleanName := normalizeActorName(name)
	cleanKind := normalizeActorKind(kind)
	cleanEmail := ""
	if opts.Email != nil {
		cleanEmail = normalizeActorEmail(*opts.Email)
	}
	cleanProvider := ""
	if opts.Provider != nil {
		cleanProvider = normalizeActorProvider(*opts.Provider)
	}
	cleanProviderRef := ""
	if opts.ProviderRef != nil {
		cleanProviderRef = strings.TrimSpace(*opts.ProviderRef)
	}
	metaJSON, err := normalizeOptionalJSON(opts.MetaJSON)
	if err != nil {
		return Actor{}, err
	}
	if cleanName == "" {
		return Actor{}, errors.New("actor name is required")
	}
	if cleanKind == "" {
		return Actor{}, errors.New("actor kind must be human or agent")
	}
	if cleanProvider == "" && cleanProviderRef != "" {
		return Actor{}, errors.New("actor provider is required when provider_ref is set")
	}
	res, err := s.db.Exec(
		`INSERT INTO actors (name, kind, email, provider, provider_ref, meta_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cleanName,
		cleanKind,
		nullableString(cleanEmail),
		nullableString(cleanProvider),
		nullableString(cleanProviderRef),
		metaJSON,
	)
	if err != nil {
		return Actor{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Actor{}, err
	}
	return s.GetActor(id)
}

func (s *Store) GetActor(id int64) (Actor, error) {
	return scanActor(s.db.QueryRow(
		`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) GetActorByEmail(email string) (Actor, error) {
	cleanEmail := normalizeActorEmail(email)
	if cleanEmail == "" {
		return Actor{}, errors.New("actor email is required")
	}
	return scanActor(s.db.QueryRow(
		`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE lower(email) = ?`,
		cleanEmail,
	))
}

func (s *Store) GetActorByProviderRef(provider, providerRef string) (Actor, error) {
	cleanProvider := normalizeActorProvider(provider)
	cleanProviderRef := strings.TrimSpace(providerRef)
	if cleanProvider == "" || cleanProviderRef == "" {
		return Actor{}, errors.New("actor provider and provider_ref are required")
	}
	return scanActor(s.db.QueryRow(
		`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at
		 FROM actors
		 WHERE lower(provider) = ? AND provider_ref = ?`,
		cleanProvider,
		cleanProviderRef,
	))
}

func (s *Store) ListActors() ([]Actor, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, email, provider, provider_ref, meta_json, created_at FROM actors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Actor
	for rows.Next() {
		actor, err := scanActor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) UpsertActorContact(name, email, provider, providerRef string, metaJSON *string) (Actor, error) {
	cleanName := normalizeActorName(name)
	cleanEmail := normalizeActorEmail(email)
	cleanProvider := normalizeActorProvider(provider)
	cleanProviderRef := strings.TrimSpace(providerRef)
	if cleanName == "" && cleanEmail == "" {
		return Actor{}, errors.New("actor contact name or email is required")
	}
	if cleanName == "" {
		cleanName = cleanEmail
	}
	if cleanProvider == "" && cleanProviderRef != "" {
		return Actor{}, errors.New("actor provider is required when provider_ref is set")
	}
	if cleanProvider != "" && cleanProviderRef != "" {
		actor, err := s.GetActorByProviderRef(cleanProvider, cleanProviderRef)
		switch {
		case err == nil:
			return s.updateActorContact(actor, cleanName, cleanEmail, cleanProvider, cleanProviderRef, metaJSON)
		case !errors.Is(err, sql.ErrNoRows):
			return Actor{}, err
		}
	}
	if cleanEmail != "" {
		actor, err := s.GetActorByEmail(cleanEmail)
		switch {
		case err == nil:
			return s.updateActorContact(actor, cleanName, cleanEmail, cleanProvider, cleanProviderRef, metaJSON)
		case !errors.Is(err, sql.ErrNoRows):
			return Actor{}, err
		}
	}
	return s.CreateActorWithOptions(cleanName, ActorKindHuman, ActorOptions{
		Email:       stringPointer(cleanEmail),
		Provider:    stringPointer(cleanProvider),
		ProviderRef: stringPointer(cleanProviderRef),
		MetaJSON:    metaJSON,
	})
}

func (s *Store) updateActorContact(existing Actor, name, email, provider, providerRef string, metaJSON *string) (Actor, error) {
	nextName := existing.Name
	if strings.TrimSpace(name) != "" {
		nextName = normalizeActorName(name)
	}
	nextEmail := existing.Email
	if email != "" {
		nextEmail = stringPointer(normalizeActorEmail(email))
	}
	nextProvider := existing.Provider
	if provider != "" {
		nextProvider = stringPointer(normalizeActorProvider(provider))
	}
	nextProviderRef := existing.ProviderRef
	if strings.TrimSpace(providerRef) != "" {
		nextProviderRef = stringPointer(strings.TrimSpace(providerRef))
	}
	nextMetaJSON := existing.MetaJSON
	if metaJSON != nil {
		cleanMetaJSON, err := normalizeOptionalJSON(metaJSON)
		if err != nil {
			return Actor{}, err
		}
		nextMetaJSON = nil
		if cleanMetaJSON != nil {
			nextMetaJSON = stringPointer(cleanMetaJSON.(string))
		}
	}
	if _, err := s.db.Exec(
		`UPDATE actors
		 SET name = ?, email = ?, provider = ?, provider_ref = ?, meta_json = ?
		 WHERE id = ?`,
		nextName,
		nullablePointerString(nextEmail),
		nullablePointerString(nextProvider),
		nullablePointerString(nextProviderRef),
		nullablePointerString(nextMetaJSON),
		existing.ID,
	); err != nil {
		return Actor{}, err
	}
	return s.GetActor(existing.ID)
}

func (s *Store) DeleteActor(id int64) error {
	res, err := s.db.Exec(`DELETE FROM actors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullablePointerString(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(*value)
}

func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := value
	return &out
}
