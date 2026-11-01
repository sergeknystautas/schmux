package flighthistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/fileutil"
)

// ManifestSchemaVersion is the version stamped on every archive manifest.
const ManifestSchemaVersion = 1

// FileVersion records one observed content of an archived file. Source names
// where the content was read from, when that is a file outside the archive.
type FileVersion struct {
	SHA256     string    `json:"sha256"`
	Bytes      int64     `json:"bytes"`
	ObservedAt time.Time `json:"observed_at"`
	Source     string    `json:"source,omitempty"`
}

// TranscriptInput names one conversation log to archive. Role and SessionID
// build the destination file name and must be confined identifiers.
type TranscriptInput struct {
	Role      string
	SessionID string
	Source    string
}

// MissingSource is evidence the archive expected but could not read.
type MissingSource struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
	Error      string    `json:"error,omitempty"`
}

// Manifest describes everything archived for one flight. Flight and git
// versions are keyed by content digest; transcript versions by destination
// file name, oldest first.
type Manifest struct {
	SchemaVersion       int                      `json:"schema_version"`
	SchmuxVersion       string                   `json:"schmux_version"`
	UpdatedAt           time.Time                `json:"updated_at"`
	Final               bool                     `json:"final,omitempty"`
	FinalSnapshotSHA256 string                   `json:"final_snapshot_sha256,omitempty"`
	FlightVersions      map[string]FileVersion   `json:"flight_versions"`
	GitVersions         map[string]FileVersion   `json:"git_state_versions"`
	Transcripts         map[string][]FileVersion `json:"transcripts"`
	MissingSources      []MissingSource          `json:"missing_sources,omitempty"`
}

// ArchiveOptions configures one ArchiveFlight call. Final also writes
// final-flight.json and marks the manifest final. Missing lists evidence the
// caller expected but could not collect, such as a workspace that is gone; each
// is recorded once in the manifest.
type ArchiveOptions struct {
	SchmuxVersion string
	Final         bool
	Now           func() time.Time
	Missing       []MissingSource
}

// archiveMu serializes manifest read-modify-write across callers.
var archiveMu sync.Mutex

// ArchiveFlight copies a flight's evidence into its archive directory. It is
// idempotent: identical content is never rewritten, changed content replaces
// the current file and appends a manifest version, and a transcript that does
// not exist is recorded as missing rather than archived as empty. gitStateJSON
// may be nil when there is no workspace to inspect.
func ArchiveFlight(flightID string, flightJSON []byte, transcripts []TranscriptInput, gitStateJSON []byte, opts ArchiveOptions) error {
	dir, err := FlightDir(flightID)
	if err != nil {
		return err
	}
	type planned struct {
		TranscriptInput
		dest string
	}
	plan := make([]planned, 0, len(transcripts))
	for _, in := range transcripts {
		if err := ValidateID(in.Role); err != nil {
			return fmt.Errorf("transcript role %w", err)
		}
		if err := ValidateID(in.SessionID); err != nil {
			return fmt.Errorf("transcript session %w", err)
		}
		plan = append(plan, planned{in, in.Role + "-" + in.SessionID + ".jsonl"})
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	archiveMu.Lock()
	defer archiveMu.Unlock()

	if err := os.MkdirAll(filepath.Join(dir, "transcripts"), 0o700); err != nil {
		return fmt.Errorf("create archive for flight %s: %w", flightID, err)
	}
	manifest, err := loadManifest(dir)
	if err != nil {
		return err
	}
	original, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	observed := now().UTC()

	if err := archiveFile(filepath.Join(dir, "flight.json"), flightJSON, observed, manifest.FlightVersions); err != nil {
		return err
	}
	if opts.Final {
		if err := writeIfChanged(filepath.Join(dir, "final-flight.json"), flightJSON); err != nil {
			return err
		}
		manifest.Final = true
		manifest.FinalSnapshotSHA256 = digest(flightJSON)
	}
	if gitStateJSON != nil {
		if err := archiveFile(filepath.Join(dir, "git-state.json"), gitStateJSON, observed, manifest.GitVersions); err != nil {
			return err
		}
	}
	for _, p := range plan {
		id := p.Role + "-" + p.SessionID
		data, err := os.ReadFile(p.Source)
		if errors.Is(err, os.ErrNotExist) {
			manifest.recordMissing(MissingSource{Kind: "transcript", ID: id, Source: p.Source, ObservedAt: observed, Error: err.Error()})
			continue
		}
		if err != nil {
			return fmt.Errorf("read transcript %s: %w", p.Source, err)
		}
		manifest.clearMissing("transcript", id)
		dest := filepath.Join(dir, "transcripts", p.dest)
		if err := writeIfChanged(dest, data); err != nil {
			return err
		}
		versions := manifest.Transcripts[p.dest]
		if len(versions) == 0 || versions[len(versions)-1].SHA256 != digest(data) {
			manifest.Transcripts[p.dest] = append(versions, FileVersion{SHA256: digest(data), Bytes: int64(len(data)), ObservedAt: observed, Source: p.Source})
		}
	}

	for _, missing := range opts.Missing {
		manifest.recordMissing(missing)
	}
	if manifest.SchmuxVersion != opts.SchmuxVersion {
		manifest.SchmuxVersion = opts.SchmuxVersion
	}
	updated, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if bytes.Equal(original, updated) {
		return nil
	}
	manifest.UpdatedAt = observed
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(dir, "manifest.json"), append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("write manifest for flight %s: %w", flightID, err)
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadManifest(dir string) (*Manifest, error) {
	m := &Manifest{SchemaVersion: ManifestSchemaVersion}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read manifest: %w", err)
	default:
		if err := json.Unmarshal(data, m); err != nil {
			return nil, fmt.Errorf("manifest in %s is malformed: %w", dir, err)
		}
	}
	if m.FlightVersions == nil {
		m.FlightVersions = map[string]FileVersion{}
	}
	if m.GitVersions == nil {
		m.GitVersions = map[string]FileVersion{}
	}
	if m.Transcripts == nil {
		m.Transcripts = map[string][]FileVersion{}
	}
	return m, nil
}

func (m *Manifest) recordMissing(src MissingSource) {
	for i, existing := range m.MissingSources {
		if existing.Kind == src.Kind && existing.ID == src.ID && existing.Source == src.Source {
			// Keep the first observation; refresh only a changed reason.
			m.MissingSources[i].Error = src.Error
			return
		}
	}
	m.MissingSources = append(m.MissingSources, src)
}

func (m *Manifest) clearMissing(kind, id string) {
	kept := m.MissingSources[:0]
	for _, s := range m.MissingSources {
		if s.Kind != kind || s.ID != id {
			kept = append(kept, s)
		}
	}
	m.MissingSources = kept
}

// archiveFile writes data as the current content of path and records a version
// keyed by digest. Identical content leaves the file and manifest untouched.
func archiveFile(path string, data []byte, observed time.Time, versions map[string]FileVersion) error {
	if _, err := replaceIfChanged(path, data); err != nil {
		return err
	}
	sum := digest(data)
	if _, ok := versions[sum]; !ok {
		versions[sum] = FileVersion{SHA256: sum, Bytes: int64(len(data)), ObservedAt: observed}
	}
	return nil
}

func writeIfChanged(path string, data []byte) error {
	_, err := replaceIfChanged(path, data)
	return err
}

// replaceIfChanged atomically replaces path with data (mode 0600) unless it
// already holds exactly data. It reports whether it wrote.
func replaceIfChanged(path string, data []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
