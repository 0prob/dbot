package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNilProfile            = errors.New("profile cannot be nil")
	ErrEmptyName             = errors.New("profile name cannot be empty")
	ErrInvalidBaseBet        = errors.New("base bet must be greater than 0")
	ErrInvalidChance         = errors.New("chance must be between 0.01 and 98.0")
	ErrInvalidOnLossAction   = errors.New("on loss action must be Reset, Multiply, Add, or Keep")
	ErrInvalidLossMultiplier = errors.New("loss multiplier must be greater than 0 for Multiply action")
	ErrNegativeLossMul       = errors.New("loss multiplier cannot be negative")
	ErrNegativeStopLoss      = errors.New("stop loss cannot be negative")
	ErrNegativeTakeProfit    = errors.New("take profit cannot be negative")
	ErrNegativeMaxBet        = errors.New("max bet cannot be negative")
	ErrBaseExceedsMaxBet     = errors.New("base bet cannot exceed max bet")
	ErrInvalidBankrollPct    = errors.New("bankroll percent must be between 0 and 100")
	ErrInvalidOnWinAction    = errors.New("on win action must be Reset or Keep")
	ErrInvalidStopLossPct    = errors.New("stop loss % must be between 0 and 100")
	ErrInvalidTakeProfitPct  = errors.New("take profit % cannot be negative")
	ErrDeleteDefaultProfile  = errors.New("cannot delete default profile")
	ErrProfileNotFound       = errors.New("profile not found")
	ErrInvalidProfileID      = errors.New("invalid profile ID: cannot contain path separators or parent directory references")
)

var slugRegexp = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// ProfileManager provides thread-safe persistence and validation for betting profiles stored on disk.
type ProfileManager struct {
	dir string
	mu  sync.RWMutex
}

// NewProfileManager initializes a ProfileManager pointing to the given directory.
// If dir is not provided, it defaults to "./profiles".
func NewProfileManager(dir ...string) (*ProfileManager, error) {
	d := "./profiles"
	if len(dir) > 0 && strings.TrimSpace(dir[0]) != "" {
		d = dir[0]
	}

	pm := &ProfileManager{dir: d}
	if err := pm.ensureDefaultProfile(); err != nil {
		return nil, err
	}
	return pm, nil
}

// ensureDefaultProfile checks if the directory exists and has at least one valid profile.
// If empty or missing, it initializes the directory and saves the fallback 'Default Martingale' profile.
func (m *ProfileManager) ensureDefaultProfile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return fmt.Errorf("failed to create profiles directory %q: %w", m.dir, err)
	}

	hasProfiles, err := m.hasAnyProfilesLocked()
	if err != nil {
		return err
	}

	if !hasProfiles {
		def := NewDefaultProfile()
		if err := m.saveProfileLocked(def); err != nil {
			return fmt.Errorf("failed to save fallback default profile: %w", err)
		}
	}
	return nil
}

func (m *ProfileManager) hasAnyProfilesLocked() (bool, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			return true, nil
		}
	}
	return false, nil
}

// ListProfiles returns a list of profile IDs/names present in the ./profiles directory.
// If the directory is empty, the default fallback profile is generated and returned.
func (m *ProfileManager) ListProfiles() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to access profiles directory: %w", err)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read profiles directory: %w", err)
	}

	var jsonFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			jsonFiles = append(jsonFiles, entry.Name())
		}
	}

	// If no profile files exist, create the fallback default profile
	if len(jsonFiles) == 0 {
		def := NewDefaultProfile()
		if err := m.saveProfileLocked(def); err != nil {
			return nil, fmt.Errorf("failed to create fallback profile: %w", err)
		}
		jsonFiles = append(jsonFiles, def.ID+".json")
	}

	var ids []string
	for _, f := range jsonFiles {
		id := strings.TrimSuffix(f, ".json")
		ids = append(ids, id)
	}

	// Sort so DefaultProfileID is first, followed by alphabetical order
	sort.Slice(ids, func(i, j int) bool {
		if ids[i] == DefaultProfileID {
			return true
		}
		if ids[j] == DefaultProfileID {
			return false
		}
		return ids[i] < ids[j]
	})

	return ids, nil
}

// LoadProfile loads a BettingProfile by ID (or name) from disk.
func (m *ProfileManager) LoadProfile(id string) (*BettingProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, ErrProfileNotFound
	}

	// First direct lookup by id.json or id
	candidatePath := filepath.Join(m.dir, trimmed+".json")
	if info, err := os.Stat(candidatePath); err == nil && !info.IsDir() {
		return m.readFileLocked(candidatePath)
	}
	if strings.HasSuffix(trimmed, ".json") {
		directPath := filepath.Join(m.dir, trimmed)
		if info, err := os.Stat(directPath); err == nil && !info.IsDir() {
			return m.readFileLocked(directPath)
		}
	}

	// Secondary lookup: scan files to match profile.ID or profile.Name
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan profiles directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		p, err := m.readFileLocked(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.EqualFold(p.ID, trimmed) || strings.EqualFold(p.Name, trimmed) {
			return p, nil
		}
	}

	// If looking for default profile and not on disk, return default
	if trimmed == DefaultProfileID || strings.EqualFold(trimmed, DefaultProfileName) {
		return NewDefaultProfile(), nil
	}

	return nil, fmt.Errorf("%w: %q", ErrProfileNotFound, id)
}

func (m *ProfileManager) readFileLocked(path string) (*BettingProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile file %q: %w", path, err)
	}

	var p BettingProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse profile file %q: %w", path, err)
	}

	if p.ID == "" {
		p.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}

	return &p, nil
}

// SaveProfile atomically persists a BettingProfile as JSON to disk using a temporary file rename.
func (m *ProfileManager) SaveProfile(p *BettingProfile) error {
	if err := m.Validate(p); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveProfileLocked(p)
}

func (m *ProfileManager) saveProfileLocked(p *BettingProfile) error {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", m.dir, err)
	}

	// Ensure ID is set and sanitized
	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = Slugify(p.Name)
		if id == "" {
			id = fmt.Sprintf("profile-%d", time.Now().UnixNano())
		}
		p.ID = id
	}

	if err := validateID(id); err != nil {
		return err
	}

	targetPath := filepath.Join(m.dir, id+".json")

	// Atomic write: write to temp file in the same directory, sync, then rename
	tmpFile, err := os.CreateTemp(m.dir, "."+id+"-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for profile: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to serialize profile to JSON: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write data to temp file: %w", err)
	}

	// Ensure bytes are flushed to physical storage
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename replaces the existing file cleanly
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", targetPath, err)
	}

	// Mark cleaned up so defer won't remove target
	tmpName = ""
	return nil
}

// DeleteProfile removes a profile from disk. Deleting the default profile is rejected.
func (m *ProfileManager) DeleteProfile(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ErrProfileNotFound
	}

	if trimmed == DefaultProfileID || strings.EqualFold(trimmed, DefaultProfileName) {
		return ErrDeleteDefaultProfile
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Locate file
	candidatePath := filepath.Join(m.dir, trimmed+".json")
	var targetPath string

	if info, err := os.Stat(candidatePath); err == nil && !info.IsDir() {
		targetPath = candidatePath
	} else if strings.HasSuffix(trimmed, ".json") {
		directPath := filepath.Join(m.dir, trimmed)
		if info, err := os.Stat(directPath); err == nil && !info.IsDir() {
			targetPath = directPath
		}
	} else {
		// Scan to find matching profile
		entries, err := os.ReadDir(m.dir)
		if err != nil {
			return fmt.Errorf("failed to scan profiles directory: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			fPath := filepath.Join(m.dir, entry.Name())
			p, err := m.readFileLocked(fPath)
			if err != nil {
				continue
			}
			if strings.EqualFold(p.ID, trimmed) || strings.EqualFold(p.Name, trimmed) {
				targetPath = fPath
				if p.ID == DefaultProfileID || strings.EqualFold(p.Name, DefaultProfileName) {
					return ErrDeleteDefaultProfile
				}
				break
			}
		}
	}

	if targetPath == "" {
		return ErrProfileNotFound
	}

	// Check if this file happens to be the default profile
	if p, err := m.readFileLocked(targetPath); err == nil {
		if p.ID == DefaultProfileID || strings.EqualFold(p.Name, DefaultProfileName) {
			return ErrDeleteDefaultProfile
		}
	}

	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("failed to remove profile %q: %w", targetPath, err)
	}

	// If no profiles remain, restore fallback default profile
	hasProfiles, _ := m.hasAnyProfilesLocked()
	if !hasProfiles {
		_ = m.saveProfileLocked(NewDefaultProfile())
	}

	return nil
}

// Validate ensures all BettingProfile fields conform to betting engine constraints.
func (m *ProfileManager) Validate(p *BettingProfile) error {
	if p == nil {
		return ErrNilProfile
	}

	if strings.TrimSpace(p.Name) == "" {
		return ErrEmptyName
	}

	if p.BaseBet <= 0 {
		return ErrInvalidBaseBet
	}

	// DuckDice dice odds range between 0.01% and 98.0%
	if p.Chance < 0.01 || p.Chance > 98.0 {
		return ErrInvalidChance
	}

	switch p.OnLossAction {
	case OnLossReset, OnLossMultiply, OnLossAdd, OnLossKeep:
	default:
		return ErrInvalidOnLossAction
	}

	if p.OnLossAction == OnLossMultiply && p.LossMultiplier <= 0 {
		return ErrInvalidLossMultiplier
	}

	if p.LossMultiplier < 0 {
		return ErrNegativeLossMul
	}

	if p.StopLoss < 0 {
		return ErrNegativeStopLoss
	}

	if p.TakeProfit < 0 {
		return ErrNegativeTakeProfit
	}

	if p.MaxBet < 0 {
		return ErrNegativeMaxBet
	}

	if p.MaxBet > 0 && p.BaseBet > p.MaxBet {
		return ErrBaseExceedsMaxBet
	}

	if p.BankrollPercent < 0 || p.BankrollPercent > 100 {
		return ErrInvalidBankrollPct
	}

	if p.StopLossPct < 0 || p.StopLossPct > 100 {
		return ErrInvalidStopLossPct
	}

	if p.TakeProfitPct < 0 {
		return ErrInvalidTakeProfitPct
	}

	if p.OnWinAction != "" && p.OnWinAction != OnWinReset && p.OnWinAction != OnWinKeep {
		return ErrInvalidOnWinAction
	}

	for i, c := range p.Conditions {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("condition %d: %w", i+1, err)
		}
	}

	return nil
}

func validateID(id string) error {
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return ErrInvalidProfileID
	}
	return nil
}

// Slugify converts an arbitrary string into a safe, alphanumeric slug suitable for file names.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRegexp.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
