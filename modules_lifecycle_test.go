package injector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Modules: plain func(*Injector) registrars, composed with Apply
// ----------------------------------------------------------------------------

func wireData(i *Injector) {
	i.Inject(NewDB)
	i.Inject(func(db *Database) *UserRepository { return &UserRepository{DB: db} })
}

func wireServices(i *Injector) {
	i.Inject(func(r *UserRepository) *UserService { return &UserService{Repo: r} })
}

func TestModules_ApplyComposesRegistrars(t *testing.T) {
	inj := NewInjector().Apply(wireServices, wireData) // order between modules irrelevant

	svc, err := Get[*UserService](inj)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.NotNil(t, svc.Repo)
	assert.Equal(t, "db", svc.Repo.DB.Name)
}

func TestModules_ApplyReturnsInjectorForChaining(t *testing.T) {
	inj := NewInjector()
	got := inj.Apply(wireData)
	assert.Same(t, inj, got)
}

// ----------------------------------------------------------------------------
// Lifecycle Start: construction-order start with rollback
// ----------------------------------------------------------------------------

type lifeComp struct {
	name     string
	startLog *[]string
	stopLog  *[]string
	failOn   bool
}

func (c *lifeComp) Start(context.Context) error {
	if c.failOn {
		return errors.New("start failed: " + c.name)
	}
	*c.startLog = append(*c.startLog, c.name)
	return nil
}

func (c *lifeComp) Shutdown(context.Context) error {
	*c.stopLog = append(*c.stopLog, c.name)
	return nil
}

func TestStart_RunsInConstructionOrder(t *testing.T) {
	var starts, stops []string

	inj := NewInjector()
	// leaf built first, then dependent.
	inj.Inject(func() *Database { return &Database{Name: "db"} })
	inj.InjectQualified("a", func() *lifeComp { return &lifeComp{name: "A", startLog: &starts, stopLog: &stops} })
	inj.InjectQualified("b", func(db *Database) *lifeComp { return &lifeComp{name: "B", startLog: &starts, stopLog: &stops} })

	require.NoError(t, inj.Build())
	require.NoError(t, inj.Start(context.Background()))
	// Both started; A and B in construction order (both present).
	assert.Len(t, starts, 2)
	assert.Contains(t, starts, "A")
	assert.Contains(t, starts, "B")
}

// okComp starts cleanly and records whether it was started and rolled back.
type okComp struct {
	started *bool
	stopped *bool
}

func (c *okComp) Start(context.Context) error    { *c.started = true; return nil }
func (c *okComp) Shutdown(context.Context) error { *c.stopped = true; return nil }

// badComp depends on okComp, so okComp is GUARANTEED constructed (and thus
// started) first — making the rollback assertion deterministic regardless of
// map iteration order. Its Start always fails.
type badComp struct{ ok *okComp }

func (c *badComp) Start(context.Context) error { return errors.New("start failed: bad") }

func TestStart_RollsBackOnFailure(t *testing.T) {
	var okStarted, okStopped bool

	inj := NewInjector()
	inj.Inject(func() *okComp { return &okComp{started: &okStarted, stopped: &okStopped} })
	inj.Inject(func(ok *okComp) *badComp { return &badComp{ok: ok} })

	require.NoError(t, inj.Build())
	err := inj.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start failed")
	assert.True(t, okStarted, "ok component should have started")
	assert.True(t, okStopped, "ok component should be rolled back (shut down) on failure")
}

// ----------------------------------------------------------------------------
// Once-cell: exactly-once construction under concurrency
// ----------------------------------------------------------------------------

func TestOnceCell_ConstructsExactlyOnceUnderRace(t *testing.T) {
	var builds atomic.Int32
	inj := NewInjector()
	inj.Inject(func() *Database {
		builds.Add(1)
		return &Database{Name: "once"}
	})

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	results := make([]*Database, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], _ = Get[*Database](inj)
		}(g)
	}
	wg.Wait()

	// The factory ran exactly once, and every goroutine got the same instance.
	assert.Equal(t, int32(1), builds.Load(), "factory must run exactly once even under concurrent first-resolution")
	for _, r := range results {
		assert.Same(t, results[0], r)
	}
}

func TestOnceCell_FailureIsMemoized(t *testing.T) {
	var calls atomic.Int32
	boom := errors.New("boom")
	inj := NewInjector()
	inj.Inject(func() (*Database, error) {
		calls.Add(1)
		return nil, boom
	})

	_, e1 := Get[*Database](inj)
	_, e2 := Get[*Database](inj)
	require.Error(t, e1)
	require.Error(t, e2)
	assert.True(t, errors.Is(e1, boom))
	assert.Equal(t, int32(1), calls.Load(), "a failed construction is memoized, not retried")
}
