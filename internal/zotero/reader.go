package zotero

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

type Reader struct {
	db   *sql.DB
	path string
}

func OpenReader(path string) (*Reader, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" {
		return nil, ErrDatabaseNotFound
	}
	if _, err := os.Stat(clean); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrDatabaseNotFound
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(clean)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return &Reader{db: db, path: clean}, nil
}

func OpenDefaultReader(home string) (*Reader, error) {
	path, err := FindDefaultDatabase(home)
	if err != nil {
		return nil, err
	}
	return OpenReader(path)
}

func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func FindDefaultDatabase(home string) (string, error) {
	root, err := resolveHome(home)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidateDatabasePaths(root) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", ErrDatabaseNotFound
}

func candidateDatabasePaths(home string) []string {
	var out []string
	macPath := filepath.Join(home, "Zotero", "zotero.sqlite")
	out = append(out, macPath)

	base := filepath.Join(home, ".zotero", "zotero")
	if profilePath := parseDefaultProfile(base); profilePath != "" {
		out = append(out, filepath.Join(base, profilePath, "zotero.sqlite"))
	}
	if matches, err := filepath.Glob(filepath.Join(base, "*", "zotero.sqlite")); err == nil {
		slices.Sort(matches)
		out = append(out, matches...)
	}
	return uniqueCleanPaths(out)
}

func resolveHome(home string) (string, error) {
	if strings.TrimSpace(home) != "" {
		return filepath.Clean(home), nil
	}
	root, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

func parseDefaultProfile(base string) string {
	f, err := os.Open(filepath.Join(base, "profiles.ini"))
	if err != nil {
		return ""
	}
	defer f.Close()

	var path string
	isDefault := false
	flush := func() string {
		if isDefault && strings.TrimSpace(path) != "" {
			return path
		}
		return ""
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			if found := flush(); found != "" {
				return found
			}
			path = ""
			isDefault = false
		case strings.HasPrefix(line, "Path="):
			path = strings.TrimSpace(strings.TrimPrefix(line, "Path="))
		case strings.HasPrefix(line, "Default="):
			isDefault = strings.TrimSpace(strings.TrimPrefix(line, "Default=")) == "1"
		}
	}
	return flush()
}

func uniqueCleanPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func (r *Reader) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.collectionID, c.key, c.collectionName, COALESCE(parent.key, '')
		FROM collections c
		LEFT JOIN collections parent ON parent.collectionID = c.parentCollectionID
		ORDER BY lower(c.collectionName), c.collectionID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Collection
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &item.ParentKey); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Reader) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.name, COUNT(it.itemID)
		FROM tags t
		LEFT JOIN itemTags it ON it.tagID = t.tagID
		GROUP BY t.tagID, t.name
		ORDER BY lower(t.name), t.tagID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var item Tag
		if err := rows.Scan(&item.Name, &item.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Reader) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT i.itemID,
		       i.key,
		       it.typeName,
		       i.dateAdded,
		       i.dateModified,
		       COALESCE(MAX(CASE WHEN f.fieldName = 'title' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'publicationTitle' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'DOI' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'ISBN' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'abstractNote' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'date' THEN v.value END), '')
		FROM items i
		JOIN itemTypes it ON it.itemTypeID = i.itemTypeID
		LEFT JOIN itemData d ON d.itemID = i.itemID
		LEFT JOIN fields f ON f.fieldID = d.fieldID
		LEFT JOIN itemDataValues v ON v.valueID = d.valueID
		WHERE it.typeName NOT IN ('attachment', 'note', 'annotation')
		  AND NOT EXISTS (SELECT 1 FROM deletedItems di WHERE di.itemID = i.itemID)
		GROUP BY i.itemID, i.key, it.typeName, i.dateAdded, i.dateModified
		ORDER BY i.itemID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Item
	var itemIDs []int64
	for rows.Next() {
		var item Item
		if err := rows.Scan(
			&item.ID,
			&item.Key,
			&item.ItemType,
			&item.DateAdded,
			&item.DateModified,
			&item.Title,
			&item.Journal,
			&item.DOI,
			&item.ISBN,
			&item.Abstract,
			&item.Year,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
		itemIDs = append(itemIDs, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	creators, err := r.loadCreators(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	tags, err := r.loadItemTags(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	collections, err := r.loadItemCollections(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	citationKeys, err := r.ResolveCitationKeys()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for i := range out {
		out[i].Creators = creators[out[i].ID]
		out[i].Tags = tags[out[i].ID]
		out[i].Collections = collections[out[i].ID]
		out[i].CitationKey = citationKeys[out[i].Key]
	}
	return out, nil
}

func (r *Reader) ListAttachments(ctx context.Context, parentKey string) ([]Attachment, error) {
	args := []any{}
	query := `
		SELECT i.itemID,
		       i.key,
		       COALESCE(parent.key, ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'title' THEN v.value END), ''),
		       COALESCE(ia.contentType, ''),
		       COALESCE(ia.path, ''),
		       COALESCE(ia.linkMode, 0),
		       i.dateModified
		FROM itemAttachments ia
		JOIN items i ON i.itemID = ia.itemID
		LEFT JOIN items parent ON parent.itemID = ia.parentItemID
		LEFT JOIN itemData d ON d.itemID = i.itemID
		LEFT JOIN fields f ON f.fieldID = d.fieldID
		LEFT JOIN itemDataValues v ON v.valueID = d.valueID
		WHERE 1 = 1`
	if clean := strings.TrimSpace(parentKey); clean != "" {
		query += ` AND parent.key = ?`
		args = append(args, clean)
	}
	query += `
		GROUP BY i.itemID, i.key, parent.key, ia.contentType, ia.path, ia.linkMode, i.dateModified
		ORDER BY i.itemID`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Attachment
	for rows.Next() {
		var item Attachment
		if err := rows.Scan(
			&item.ID,
			&item.Key,
			&item.ParentKey,
			&item.Title,
			&item.ContentType,
			&item.Path,
			&item.LinkMode,
			&item.DateModified,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Reader) ListAnnotations(ctx context.Context, parentKey string) ([]Annotation, error) {
	args := []any{}
	query := `
		SELECT i.itemID,
		       i.key,
		       COALESCE(parent.key, ''),
		       COALESCE(a.type, ''),
		       COALESCE(a.authorName, ''),
		       COALESCE(a.text, ''),
		       COALESCE(a.comment, ''),
		       COALESCE(a.color, ''),
		       COALESCE(a.pageLabel, ''),
		       COALESCE(a.sortIndex, ''),
		       COALESCE(a.position, ''),
		       i.dateModified
		FROM itemAnnotations a
		JOIN items i ON i.itemID = a.itemID
		LEFT JOIN items parent ON parent.itemID = a.parentItemID
		WHERE 1 = 1`
	if clean := strings.TrimSpace(parentKey); clean != "" {
		query += ` AND parent.key = ?`
		args = append(args, clean)
	}
	query += ` ORDER BY i.itemID`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Annotation
	for rows.Next() {
		var item Annotation
		if err := rows.Scan(
			&item.ID,
			&item.Key,
			&item.ParentKey,
			&item.AnnotationType,
			&item.AuthorName,
			&item.Text,
			&item.Comment,
			&item.Color,
			&item.PageLabel,
			&item.SortIndex,
			&item.Position,
			&item.DateModified,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Reader) DetectBetterBibTeXExports() ([]string, error) {
	candidates := []string{
		filepath.Dir(r.path),
		filepath.Dir(filepath.Dir(r.path)),
	}
	var matches []string
	for _, dir := range uniqueCleanPaths(candidates) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.ToLower(entry.Name())
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".json" && ext != ".bib" {
				continue
			}
			if !strings.Contains(name, "bib") && !strings.Contains(name, "zotero") && !strings.Contains(name, "citation") && !strings.Contains(name, "library") {
				continue
			}
			matches = append(matches, filepath.Join(dir, entry.Name()))
		}
	}
	matches = uniqueCleanPaths(matches)
	if len(matches) == 0 {
		return nil, os.ErrNotExist
	}
	return matches, nil
}

func (r *Reader) ResolveCitationKeys(paths ...string) (map[string]string, error) {
	if len(paths) == 0 {
		detected, err := r.DetectBetterBibTeXExports()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return map[string]string{}, nil
			}
			return nil, err
		}
		paths = detected
	}
	metaByKey, err := r.listCitationMetadata(context.Background())
	if err != nil {
		return nil, err
	}
	byDOI := make(map[string]string, len(metaByKey))
	byTitle := make(map[string]string, len(metaByKey))
	for key, meta := range metaByKey {
		if meta.DOI != "" {
			byDOI[normalizeLookup(meta.DOI)] = key
		}
		if meta.Title != "" {
			byTitle[normalizeLookup(meta.Title)] = key
		}
	}

	out := make(map[string]string)
	for _, path := range uniqueCleanPaths(paths) {
		entries, err := loadCitationExport(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			key := strings.TrimSpace(entry.ItemKey)
			if key == "" && entry.DOI != "" {
				key = byDOI[normalizeLookup(entry.DOI)]
			}
			if key == "" && entry.Title != "" {
				key = byTitle[normalizeLookup(entry.Title)]
			}
			if key == "" || strings.TrimSpace(entry.CitationKey) == "" {
				continue
			}
			if _, ok := metaByKey[key]; ok {
				out[key] = entry.CitationKey
			}
		}
	}
	return out, nil
}

func (r *Reader) loadCreators(ctx context.Context, itemIDs []int64) (map[int64][]Creator, error) {
	if len(itemIDs) == 0 {
		return map[int64][]Creator{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ic.itemID,
		       ic.orderIndex,
		       COALESCE(ct.creatorType, ''),
		       COALESCE(cd.firstName, ''),
		       COALESCE(cd.lastName, ''),
		       COALESCE(cd.name, '')
		FROM itemCreators ic
		JOIN creatorTypes ct ON ct.creatorTypeID = ic.creatorTypeID
		JOIN creators c ON c.creatorID = ic.creatorID
		JOIN creatorData cd ON cd.creatorDataID = c.creatorDataID
		ORDER BY ic.itemID, ic.orderIndex`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allowed := toIDSet(itemIDs)
	out := make(map[int64][]Creator)
	for rows.Next() {
		var itemID int64
		var creator Creator
		if err := rows.Scan(&itemID, &creator.Order, &creator.Type, &creator.FirstName, &creator.LastName, &creator.Name); err != nil {
			return nil, err
		}
		if _, ok := allowed[itemID]; !ok {
			continue
		}
		out[itemID] = append(out[itemID], creator)
	}
	return out, rows.Err()
}

func (r *Reader) loadItemTags(ctx context.Context, itemIDs []int64) (map[int64][]string, error) {
	if len(itemIDs) == 0 {
		return map[int64][]string{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT it.itemID, t.name
		FROM itemTags it
		JOIN tags t ON t.tagID = it.tagID
		ORDER BY it.itemID, lower(t.name), t.tagID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allowed := toIDSet(itemIDs)
	out := make(map[int64][]string)
	for rows.Next() {
		var itemID int64
		var tag string
		if err := rows.Scan(&itemID, &tag); err != nil {
			return nil, err
		}
		if _, ok := allowed[itemID]; !ok {
			continue
		}
		out[itemID] = append(out[itemID], tag)
	}
	return out, rows.Err()
}

func (r *Reader) loadItemCollections(ctx context.Context, itemIDs []int64) (map[int64][]string, error) {
	if len(itemIDs) == 0 {
		return map[int64][]string{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ci.itemID, c.key
		FROM collectionItems ci
		JOIN collections c ON c.collectionID = ci.collectionID
		ORDER BY ci.itemID, c.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allowed := toIDSet(itemIDs)
	out := make(map[int64][]string)
	for rows.Next() {
		var itemID int64
		var key string
		if err := rows.Scan(&itemID, &key); err != nil {
			return nil, err
		}
		if _, ok := allowed[itemID]; !ok {
			continue
		}
		out[itemID] = append(out[itemID], key)
	}
	return out, rows.Err()
}

type citationMeta struct {
	Title string
	DOI   string
}

func (r *Reader) listCitationMetadata(ctx context.Context) (map[string]citationMeta, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT i.key,
		       COALESCE(MAX(CASE WHEN f.fieldName = 'title' THEN v.value END), ''),
		       COALESCE(MAX(CASE WHEN f.fieldName = 'DOI' THEN v.value END), '')
		FROM items i
		JOIN itemTypes it ON it.itemTypeID = i.itemTypeID
		LEFT JOIN itemData d ON d.itemID = i.itemID
		LEFT JOIN fields f ON f.fieldID = d.fieldID
		LEFT JOIN itemDataValues v ON v.valueID = d.valueID
		WHERE it.typeName NOT IN ('attachment', 'note', 'annotation')
		GROUP BY i.key
		ORDER BY i.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]citationMeta)
	for rows.Next() {
		var key string
		var meta citationMeta
		if err := rows.Scan(&key, &meta.Title, &meta.DOI); err != nil {
			return nil, err
		}
		out[key] = meta
	}
	return out, rows.Err()
}

func toIDSet(ids []int64) map[int64]struct{} {
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

type citationExportEntry struct {
	ItemKey     string
	CitationKey string
	DOI         string
	Title       string
}

func loadCitationExport(path string) ([]citationExportEntry, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return loadCitationJSON(path)
	case ".bib":
		return loadCitationBib(path)
	default:
		return nil, nil
	}
}

func loadCitationJSON(path string) ([]citationExportEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	var out []citationExportEntry
	collectCitationEntries(payload, &out)
	return out, nil
}

func collectCitationEntries(value any, out *[]citationExportEntry) {
	switch typed := value.(type) {
	case map[string]any:
		entry := citationExportEntry{
			ItemKey:     stringValue(firstDefined(typed, "itemKey", "item_key", "key")),
			CitationKey: stringValue(firstDefined(typed, "citationKey", "citation_key", "citekey")),
			DOI:         stringValue(firstDefined(typed, "DOI", "doi")),
			Title:       stringValue(firstDefined(typed, "title")),
		}
		if strings.TrimSpace(entry.CitationKey) != "" {
			*out = append(*out, entry)
		}
		for _, nested := range typed {
			collectCitationEntries(nested, out)
		}
	case []any:
		for _, nested := range typed {
			collectCitationEntries(nested, out)
		}
	}
}

func firstDefined(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

var (
	bibEntryRE = regexp.MustCompile(`(?ms)@[\w]+\s*\{\s*([^,\s]+)\s*,(.*?)^\s*\}`)
	bibFieldRE = regexp.MustCompile(`(?im)([A-Za-z][A-Za-z0-9_]*)\s*=\s*[\{\"]([^\"\}]+)[\}\"]`)
)

func loadCitationBib(path string) ([]citationExportEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	matches := bibEntryRE.FindAllStringSubmatch(string(data), -1)
	out := make([]citationExportEntry, 0, len(matches))
	for _, match := range matches {
		entry := citationExportEntry{CitationKey: strings.TrimSpace(match[1])}
		fields := bibFieldRE.FindAllStringSubmatch(match[2], -1)
		for _, field := range fields {
			name := strings.ToLower(strings.TrimSpace(field[1]))
			value := strings.TrimSpace(field[2])
			switch name {
			case "doi":
				entry.DOI = value
			case "title":
				entry.Title = value
			case "ids":
				for _, part := range strings.FieldsFunc(value, func(r rune) bool {
					return r == ',' || r == ';' || r == ' '
				}) {
					part = strings.TrimSpace(part)
					if len(part) >= 8 {
						entry.ItemKey = part
						break
					}
				}
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func normalizeLookup(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	return clean
}
