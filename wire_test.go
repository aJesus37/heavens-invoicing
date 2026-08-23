package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/db"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
	"github.com/ajesus37/heavens-invoicing/internal/telegram"
)

func TestNextBackoff(t *testing.T) {
	const base = 30 * time.Second
	const max = 5 * time.Minute

	tests := []struct {
		name     string
		previous time.Duration
		ran      time.Duration
		want     time.Duration
	}{
		{"first failure doubles", base, time.Second, time.Minute},
		{"keeps doubling under cap", 2 * time.Minute, time.Second, 4 * time.Minute},
		{"caps at max", max, time.Second, max},
		{"healthy run resets to base", max, max, base},
		{"long healthy run resets from mid escalation", 4 * time.Minute, max + time.Minute, base},
		{"short run right below threshold keeps escalating", 4 * time.Minute, max - time.Millisecond, max},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextBackoff(tt.previous, tt.ran, base, max)
			if got != tt.want {
				t.Fatalf("nextBackoff(prev=%s, ran=%s) = %s, want %s", tt.previous, tt.ran, got, tt.want)
			}
		})
	}
}

// flakyAPI fails every poll after holding it for hold; starts records when
// each GetUpdates began so the test can measure the restart cadence.
type flakyAPI struct {
	mu     sync.Mutex
	calls  int
	hold   time.Duration
	starts []time.Time
}

func (f *flakyAPI) SendMessage(_ context.Context, _, _ string) error { return nil }

func (f *flakyAPI) GetUpdates(_ context.Context, _ int64) ([]telegram.Update, error) {
	f.mu.Lock()
	f.starts = append(f.starts, time.Now())
	f.calls++
	f.mu.Unlock()
	time.Sleep(f.hold)
	return nil, errors.New("poll down")
}

func (f *flakyAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// The wrapper must keep restarting through repeated failures and stop
// cleanly on cancel. With every failed run outliving the max backoff, the
// reset policy keeps each restart gap at the base delay instead of letting
// escalation grow unbounded.
func TestRunAdminBotRestartsAndResetsBackoff(t *testing.T) {
	oldBase, oldMax := adminBotInitialBackoff, adminBotMaxBackoff
	adminBotInitialBackoff = 10 * time.Millisecond
	adminBotMaxBackoff = 20 * time.Millisecond
	defer func() {
		adminBotInitialBackoff, adminBotMaxBackoff = oldBase, oldMax
	}()

	api := &flakyAPI{hold: 30 * time.Millisecond} // > max: every run "was healthy long"
	bot := telegram.NewAdminBot(api, "777", nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runAdminBot(ctx, bot)
		close(done)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for api.count() < 5 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("wrapper stopped restarting (calls=%d)", api.count())
		}
		time.Sleep(time.Millisecond)
	}

	api.mu.Lock()
	starts := slices.Clone(api.starts)
	api.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runAdminBot did not stop on canceled ctx")
	}

	// Every inter-attempt gap is hold + one backoff sleep. Exact reset
	// semantics are pinned by TestNextBackoff; here we assert the cadence
	// never balloons past hold + max backoff (with scheduling slack).
	for i := 1; i < len(starts); i++ {
		gap := starts[i].Sub(starts[i-1])
		if limit := api.hold + adminBotMaxBackoff + 3*adminBotInitialBackoff; gap >= limit {
			t.Fatalf("attempt %d gap %s >= %s: restart cadence out of control", i, gap, limit)
		}
	}
}

func TestSetupSenderInfoReadsBusinessSettings(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	repos := repo.New(conn)
	ctx := context.Background()

	si := setupSenderInfo(ctx, repos.Settings)
	if si.Name != "" || si.Address != "" || si.PIXKey != "" {
		t.Fatalf("empty settings should yield empty SenderInfo, got %+v", si)
	}

	mustSet := func(key, value string) {
		t.Helper()
		if err := repos.Settings.Set(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	mustSet(repo.SettingBusinessName, "Minha Empresa")
	mustSet(repo.SettingBusinessAddress, "Rua Um, 123")
	mustSet(repo.SettingDefaultPIXKey, "chave@pix")

	si = setupSenderInfo(ctx, repos.Settings)
	if si.Name != "Minha Empresa" || si.Address != "Rua Um, 123" || si.PIXKey != "chave@pix" {
		t.Fatalf("SenderInfo not wired from settings: %+v", si)
	}
}
