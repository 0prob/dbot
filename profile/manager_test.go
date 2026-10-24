package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestProfileManager_DefaultFallback(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dbot-profiles-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pm, err := NewProfileManager(tempDir)
	if err != nil {
		t.Fatalf("NewProfileManager failed: %v", err)
	}

	profiles, err := pm.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles failed: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatalf("expected at least 1 profile, got 0")
	}
	if profiles[0] != DefaultProfileID {
		t.Errorf("expected default profile ID %q, got %q", DefaultProfileID, profiles[0])
	}

	p, err := pm.LoadProfile(DefaultProfileID)
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if p.Name != DefaultProfileName {
		t.Errorf("expected name %q, got %q", DefaultProfileName, p.Name)
	}
	if p.BaseBet <= 0 {
		t.Errorf("expected BaseBet > 0, got %v", p.BaseBet)
	}
	if p.Chance <= 0 {
		t.Errorf("expected Chance > 0, got %v", p.Chance)
	}
	if p.LossMultiplier != 2.0 {
		t.Errorf("expected LossMultiplier 2.0, got %v", p.LossMultiplier)
	}
}

func TestProfileManager_SaveLoadDelete(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dbot-profiles-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pm, err := NewProfileManager(tempDir)
	if err != nil {
		t.Fatalf("NewProfileManager failed: %v", err)
	}

	custom := &BettingProfile{
		ID:             "dalembert-conservative",
		Name:           "D'Alembert Conservative",
		BaseBet:        0.00000005,
		Chance:         50.0,
		OnLossAction:   OnLossAdd,
		LossMultiplier: 1.0,
		StopLoss:       0.001,
		TakeProfit:     0.0005,
		MaxBet:         0.0001,
	}

	// Save
	if err := pm.SaveProfile(custom); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tempDir, "dalembert-conservative.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected profile file to exist at %s: %v", path, err)
	}

	// List
	list, err := pm.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 profiles in list, got %d (%v)", len(list), list)
	}

	// Load by ID
	loaded, err := pm.LoadProfile("dalembert-conservative")
	if err != nil {
		t.Fatalf("LoadProfile by ID failed: %v", err)
	}
	if loaded.Name != custom.Name || loaded.BaseBet != custom.BaseBet || loaded.OnLossAction != OnLossAdd {
		t.Fatalf("loaded profile mismatch: %+v", loaded)
	}

	// Load by Name
	loadedByName, err := pm.LoadProfile("D'Alembert Conservative")
	if err != nil {
		t.Fatalf("LoadProfile by Name failed: %v", err)
	}
	if loadedByName.ID != custom.ID {
		t.Fatalf("expected ID %q, got %q", custom.ID, loadedByName.ID)
	}

	// Try to delete default profile (should fail)
	if err := pm.DeleteProfile(DefaultProfileID); err == nil {
		t.Errorf("expected error deleting default profile, got nil")
	}

	// Delete custom profile
	if err := pm.DeleteProfile("dalembert-conservative"); err != nil {
		t.Fatalf("DeleteProfile failed: %v", err)
	}

	// Verify it's gone
	if _, err := pm.LoadProfile("dalembert-conservative"); err == nil {
		t.Errorf("expected profile to be deleted, but LoadProfile succeeded")
	}
}

func TestProfileManager_Validate(t *testing.T) {
	pm, _ := NewProfileManager(t.TempDir())

	tests := []struct {
		name    string
		profile *BettingProfile
		wantErr error
	}{
		{
			name:    "nil profile",
			profile: nil,
			wantErr: ErrNilProfile,
		},
		{
			name: "empty name",
			profile: &BettingProfile{
				Name:         "",
				BaseBet:      1,
				Chance:       50,
				OnLossAction: OnLossReset,
			},
			wantErr: ErrEmptyName,
		},
		{
			name: "zero base bet",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      0,
				Chance:       50,
				OnLossAction: OnLossReset,
			},
			wantErr: ErrInvalidBaseBet,
		},
		{
			name: "negative base bet",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      -0.5,
				Chance:       50,
				OnLossAction: OnLossReset,
			},
			wantErr: ErrInvalidBaseBet,
		},
		{
			name: "chance too low",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       0.005,
				OnLossAction: OnLossReset,
			},
			wantErr: ErrInvalidChance,
		},
		{
			name: "chance too high",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       99.0,
				OnLossAction: OnLossReset,
			},
			wantErr: ErrInvalidChance,
		},
		{
			name: "invalid on loss action",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       50,
				OnLossAction: "InvalidAction",
			},
			wantErr: ErrInvalidOnLossAction,
		},
		{
			name: "multiply with zero loss multiplier",
			profile: &BettingProfile{
				Name:           "Test",
				BaseBet:        1,
				Chance:         50,
				OnLossAction:   OnLossMultiply,
				LossMultiplier: 0,
			},
			wantErr: ErrInvalidLossMultiplier,
		},
		{
			name: "negative stop loss",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       50,
				OnLossAction: OnLossReset,
				StopLoss:     -10,
			},
			wantErr: ErrNegativeStopLoss,
		},
		{
			name: "negative take profit",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       50,
				OnLossAction: OnLossReset,
				TakeProfit:   -5,
			},
			wantErr: ErrNegativeTakeProfit,
		},
		{
			name: "negative max bet",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      1,
				Chance:       50,
				OnLossAction: OnLossReset,
				MaxBet:       -1,
			},
			wantErr: ErrNegativeMaxBet,
		},
		{
			name: "base bet exceeds max bet",
			profile: &BettingProfile{
				Name:         "Test",
				BaseBet:      10,
				Chance:       50,
				OnLossAction: OnLossReset,
				MaxBet:       5,
			},
			wantErr: ErrBaseExceedsMaxBet,
		},
		{
			name: "valid profile",
			profile: &BettingProfile{
				Name:           "Martingale Standard",
				BaseBet:        0.0001,
				Chance:         49.5,
				OnLossAction:   OnLossMultiply,
				LossMultiplier: 2.0,
				StopLoss:       0.05,
				TakeProfit:     0.02,
				MaxBet:         0.01,
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pm.Validate(tc.profile)
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Errorf("expected error %v, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestProfileManager_ThreadSafety(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dbot-profiles-concurrency-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pm, err := NewProfileManager(tempDir)
	if err != nil {
		t.Fatalf("NewProfileManager failed: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 25

	for i := 0; i < goroutines; i++ {
		wg.Add(3)

		// Goroutine saving profiles
		go func(id int) {
			defer wg.Done()
			p := &BettingProfile{
				ID:             fmt.Sprintf("profile-%d", id),
				Name:           fmt.Sprintf("Profile %d", id),
				BaseBet:        float64(id+1) * 0.00001,
				Chance:         49.5,
				OnLossAction:   OnLossMultiply,
				LossMultiplier: 2.0,
			}
			_ = pm.SaveProfile(p)
		}(i)

		// Goroutine listing profiles
		go func() {
			defer wg.Done()
			_, _ = pm.ListProfiles()
		}()

		// Goroutine loading profiles
		go func(id int) {
			defer wg.Done()
			_, _ = pm.LoadProfile(fmt.Sprintf("profile-%d", id))
			_, _ = pm.LoadProfile(DefaultProfileID)
		}(i)
	}

	wg.Wait()

	list, err := pm.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles after concurrency test failed: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected multiple profiles after concurrent writes, got %d", len(list))
	}
}
