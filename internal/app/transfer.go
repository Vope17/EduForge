package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

type Manifest struct {
	Schema           int             `json:"schema"`
	Exported         string          `json:"exported_at"`
	Project          string          `json:"project_id"`
	Records          []domain.Record `json:"records"`
	IncludesPersonal bool            `json:"includes_personal"`
}

// BlobReferences returns actual file fields, excluding arbitrary text supplied by users or models.
func BlobReferences(r domain.Record) map[string]bool {
	out := make(map[string]bool)
	add := func(path string) {
		if path != "" {
			out[path] = true
		}
	}
	parsed := func(units []domain.Unit, images []domain.Image) {
		for _, unit := range units {
			add(unit.Preview)
		}
		for _, image := range images {
			add(image.Path)
		}
	}
	addStyle := func(style domain.Style) {
		for _, key := range []string{"logo_image", "template_file", "cover_image", "content_image"} {
			add(stringValue(style[key]))
		}
	}
	addStyles := func(styles map[string]domain.Style) {
		for _, style := range styles {
			addStyle(style)
		}
	}
	switch r.Kind {
	case "source":
		value, _ := domain.Decode[domain.Source](r.Data)
		add(value.Path)
		if value.Parsed != nil {
			parsed(value.Parsed.Units, value.Parsed.Images)
		}
	case "knowledge":
		value, _ := domain.Decode[domain.Knowledge](r.Data)
		parsed(value.Units, value.Images)
	case "template":
		value, _ := domain.Decode[domain.Template](r.Data)
		add(value.Preview)
		add(value.LogoImage)
		add(value.File)
		add(value.CoverImage)
		add(value.ContentImage)
		if ex := value.Extraction; ex != nil {
			add(ex.Source)
			for _, f := range ex.Thumbs {
				add(f)
			}
		}
		for _, h := range value.History {
			old, _ := domain.Decode[domain.Template](h.Template)
			for _, f := range []string{old.Preview, old.File, old.CoverImage, old.ContentImage} {
				add(f)
			}
		}
	case "artifact", "preview":
		value, _ := domain.Decode[domain.Artifact](r.Data)
		for _, section := range value.Sections {
			add(section.Image)
		}
		addStyles(value.Style.Resolved)
	case "request":
		value, _ := domain.Decode[domain.Request](r.Data)
		addStyles(value.Overrides)
		addStyles(value.Style.Resolved)
		for _, target := range value.Targets {
			addStyle(target.TemplateStyle)
		}
	case "task":
		value, _ := domain.Decode[domain.Task](r.Data)
		addStyles(value.Style.Resolved)
	case "project":
		value, _ := domain.Decode[domain.Project](r.Data)
		addStyles(value.Style)
	case "feedback":
		value, _ := domain.Decode[domain.Feedback](r.Data)
		addStyles(value.Changes)
	case "profile_version":
		value, _ := domain.Decode[domain.ProfileVersion](r.Data)
		addStyle(value.Common)
		addStyles(value.Outputs)
	}
	return out
}

func (a *App) OwnedBlobs(ctx context.Context, owner string) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, kind := range []string{"source", "knowledge", "run", "template", "artifact", "preview"} {
		records, err := a.Store.List(ctx, owner, store.Filter{Kind: kind})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			for name := range BlobReferences(record) {
				out[name] = true
			}
		}
	}
	return out, nil
}
func sanitizeExport(r domain.Record) domain.Record {
	var obj map[string]any
	_ = json.Unmarshal(r.Data, &obj)
	if r.Kind == "plan" || r.Kind == "run" {
		if host, ok := obj["host"].(map[string]any); ok {
			host["ssh_alias"] = ""
		}
		if r.Kind == "plan" {
			obj["confirmed"] = false
			obj["confirmed_hash"] = ""
		}
		if r.Kind == "run" {
			if obj["progress"] != "finished" && obj["progress"] != "manual" {
				obj["progress"] = "needs_environment"
				obj["reason"] = "environment_reconnection_required_after_import"
			}
		}
	}
	if r.Kind == "task" {
		if obj["status"] == "running" || obj["status"] == "queued" {
			obj["status"] = "paused"
		}
	}
	if r.Kind == "request" {
		obj["status"] = "paused"
	}
	r.Owner = ""
	r.Data = domain.Encode(obj)
	return r
}
func (a *App) Export(ctx context.Context, owner, project string, personal bool) ([]byte, error) {
	p, err := a.Store.Get(ctx, owner, project, "project")
	if err != nil {
		return nil, err
	}
	records, err := a.Store.List(ctx, owner, store.Filter{Project: project})
	if err != nil {
		return nil, err
	}
	records = append([]domain.Record{p}, records...)
	if personal {
		for _, kind := range []string{"profile", "profile_version", "reading", "preference", "template"} {
			items, err := a.Store.List(ctx, owner, store.Filter{Kind: kind})
			if err != nil {
				return nil, err
			}
			records = append(records, items...)
		}
	}
	manifest := Manifest{Schema: 1, Exported: domain.Now(), Project: project, Records: []domain.Record{}, IncludesPersonal: personal}
	refs := map[string]bool{}
	for _, r := range records {
		if r.Kind == "provider" || r.Kind == "attempt" {
			continue
		}
		r = sanitizeExport(r)
		manifest.Records = append(manifest.Records, r)
		for name := range BlobReferences(r) {
			refs[name] = true
		}
	}
	owned, err := a.OwnedBlobs(ctx, owner)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	w, err := archive.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(domain.Encode(manifest)); err != nil {
		return nil, err
	}
	names := []string{}
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !owned[name] {
			return nil, errors.New("archive_file_not_owned")
		}
		file, err := a.Store.Path(name)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		w, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(b); err != nil {
			return nil, err
		}
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func remap(value any, ids map[string]string) any {
	switch v := value.(type) {
	case string:
		if id, ok := ids[v]; ok {
			return id
		}
		for old, id := range ids {
			if strings.HasPrefix(v, old+":") {
				return id + strings.TrimPrefix(v, old)
			}
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = remap(item, ids)
		}
		return v
	case map[string]any:
		for key, item := range v {
			v[key] = remap(item, ids)
		}
		return v
	}
	return value
}
func (a *App) Import(ctx context.Context, owner string, b []byte, personal bool) (domain.Record, error) {
	archive, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return domain.Record{}, errors.New("invalid_project_archive")
	}
	if len(archive.File) > 10000 {
		return domain.Record{}, errors.New("archive_limits_exceeded")
	}
	var expanded uint64
	entries := map[string][]byte{}
	for _, file := range archive.File {
		expanded += file.UncompressedSize64
		if expanded > 256<<20 {
			return domain.Record{}, errors.New("archive_limits_exceeded")
		}
		if file.Name != "manifest.json" && !strings.HasPrefix(file.Name, "blobs/") {
			return domain.Record{}, errors.New("unsupported_archive_entry")
		}
		if path.Clean(file.Name) != file.Name || strings.Contains(file.Name, "\\") {
			return domain.Record{}, errors.New("unsafe_archive_path")
		}
		if _, exists := entries[file.Name]; exists {
			return domain.Record{}, errors.New("duplicate_archive_entry")
		}
		r, err := file.Open()
		if err != nil {
			return domain.Record{}, err
		}
		data, err := io.ReadAll(io.LimitReader(r, 256<<20))
		_ = r.Close()
		if err != nil {
			return domain.Record{}, err
		}
		entries[file.Name] = data
	}
	var manifest Manifest
	if json.Unmarshal(entries["manifest.json"], &manifest) != nil || manifest.Schema != 1 {
		return domain.Record{}, errors.New("unsupported_project_schema")
	}
	ids := map[string]string{}
	records := []domain.Record{}
	allowed := map[string]bool{"project": true, "paper": true, "source": true, "knowledge": true, "feedback": true, "request": true, "preview": true, "task": true, "step": true, "artifact": true, "plan": true, "run": true, "comparison": true, "event": true, "plan_chat": true, "folder": true}
	if personal {
		for _, kind := range []string{"profile", "profile_version", "reading", "preference", "template"} {
			allowed[kind] = true
		}
	}
	for _, r := range manifest.Records {
		if !allowed[r.Kind] {
			continue
		}
		if err := store.ValidateID(r.ID); err != nil {
			return domain.Record{}, err
		}
		if ids[r.ID] != "" {
			return domain.Record{}, errors.New("duplicate_record_id")
		}
		ids[r.ID] = domain.ID()
		records = append(records, r)
	}
	project := ids[manifest.Project]
	if project == "" {
		return domain.Record{}, errors.New("project_record_missing")
	}
	refs := map[string]bool{}
	for _, r := range records {
		var value any
		if json.Unmarshal(r.Data, &value) != nil {
			return domain.Record{}, errors.New("invalid_record_json")
		}
		for name := range BlobReferences(r) {
			refs[name] = true
		}
	}
	for ref := range refs {
		data, ok := entries[ref]
		if !ok {
			return domain.Record{}, errors.New("archive_blob_missing")
		}
		base := path.Base(ref)
		parts := strings.SplitN(base, ".", 2)
		sum := sha256.Sum256(data)
		if parts[0] != hex.EncodeToString(sum[:]) {
			return domain.Record{}, errors.New("archive_checksum_mismatch")
		}
		rel, _, err := a.Store.Blob(data, path.Ext(base))
		if err != nil || rel != ref {
			return domain.Record{}, errors.New("archive_blob_invalid")
		}
	}
	// Validate and insert all records in a transaction; failed imports leave no partial project.
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Record{}, err
	}
	defer tx.Rollback()
	pending := records
	inserted := map[string]bool{}
	for len(pending) > 0 {
		next := []domain.Record{}
		progress := false
		for _, r := range pending {
			if r.Parent != "" && !inserted[r.Parent] {
				if ids[r.Parent] == "" {
					return domain.Record{}, errors.New("archive_parent_missing")
				}
				next = append(next, r)
				continue
			}
			if r.Project != "" && !inserted[r.Project] {
				next = append(next, r)
				continue
			}
			if r.Paper != "" && !inserted[r.Paper] {
				next = append(next, r)
				continue
			}
			var data any
			_ = json.Unmarshal(sanitizeExport(r).Data, &data)
			data = remap(data, ids)
			if obj, ok := data.(map[string]any); ok {
				if r.Kind == "profile" {
					obj["enabled"] = false
					obj["name"] = stringValue(obj["name"]) + " (imported)"
				}
				if r.Kind == "preference" {
					obj["imported_candidate"] = true
				}
				if r.Kind == "request" {
					obj["providers"] = []any{}
				}
			}
			stamp := domain.Now()
			_, err = tx.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?,?,?,?,?,?,?)", ids[r.ID], owner, r.Kind, nullValue(ids[r.Parent]), nullValue(ids[r.Project]), nullValue(ids[r.Paper]), max(1, r.Version), string(domain.Encode(data)), stamp, stamp)
			if err != nil {
				return domain.Record{}, err
			}
			inserted[r.ID] = true
			progress = true
		}
		if !progress {
			return domain.Record{}, errors.New("archive_dependency_cycle")
		}
		pending = next
	}
	if err = tx.Commit(); err != nil {
		return domain.Record{}, err
	}
	return a.Store.Get(ctx, owner, project, "project")
}
func nullValue(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
