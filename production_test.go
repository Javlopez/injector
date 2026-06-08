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
// Validate: static graph check, no construction
// ----------------------------------------------------------------------------

// validateRepo/Svc/Handler are a minimal repo→service→handler chain used to
// exercise Validate without touching the construction path.
type vRepo struct{}
type vSvc struct{ r *vRepo }
type vHandler struct{ s *vSvc }

func newVRepo() *vRepo              { return &vRepo{} }
func newVSvc(r *vRepo) *vSvc        { return &vSvc{r: r} }
func newVHandler(s *vSvc) *vHandler { return &vHandler{s: s} }

func TestValidate_HealthyGraphPasses(t *testing.T) {
	inj := NewInjector()
	inj.Inject(newVHandler)
	inj.Inject(newVSvc)
	inj.Inject(newVRepo)

	assert.NoError(t, inj.Validate())
}

func TestValidate_DoesNotConstruct(t *testing.T) {
	var built atomic.Int32
	inj := NewInjector()
	inj.Inject(func() *vRepo { built.Add(1); return &vRepo{} })
	inj.Inject(newVSvc)
	inj.Inject(newVHandler)

	require.NoError(t, inj.Validate())
	assert.Equal(t, int32(0), built.Load(), "Validate must not call any factory")
}

func TestValidate_ReportsMissingDependency(t *testing.T) {
	inj := NewInjector()
	inj.Inject(newVHandler)
	inj.Inject(newVSvc)
	// newVRepo intentionally not registered.

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vRepo")
	assert.Contains(t, err.Error(), "no dependency found")
}

func TestValidate_ReportsCycle(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewCycA)
	inj.Inject(NewCycB)

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic dependency")
}

func TestValidate_AggregatesAllProblems(t *testing.T) {
	inj := NewInjector()
	inj.Inject(newVHandler) // needs *vSvc (missing)
	inj.Inject(NewCycA)     // cycle with CycB
	inj.Inject(NewCycB)

	err := inj.Validate()
	require.Error(t, err)
	// errors.Join exposes every problem, not just the first.
	msg := err.Error()
	assert.Contains(t, msg, "vSvc")   // missing
	assert.Contains(t, msg, "cyclic") // cycle
}

// ----------------------------------------------------------------------------
// Shutdown: reverse-order lifecycle
// ----------------------------------------------------------------------------

type closable struct {
	name   string
	log    *[]string
	closed *atomic.Int32
}

func (c *closable) Shutdown(context.Context) error {
	*c.log = append(*c.log, c.name)
	c.closed.Add(1)
	return nil
}

func TestShutdown_ReverseConstructionOrder(t *testing.T) {
	var order []string
	var count atomic.Int32

	// dep chain: A is built first (it's the leaf), then B which needs A.
	type depA struct{ *closable }
	type depB struct{ a *depA }

	inj := NewInjector()
	inj.Inject(func() *depA { return &depA{&closable{name: "A", log: &order, closed: &count}} })
	inj.Inject(func(a *depA) *depB { return &depB{a: a} })
	// depB is not a Shutdowner; only A is. Build the whole graph.
	_, err := Get[*depB](inj)
	require.NoError(t, err)

	require.NoError(t, inj.Shutdown(context.Background()))
	assert.Equal(t, []string{"A"}, order)

	// Idempotent: second call does nothing.
	require.NoError(t, inj.Shutdown(context.Background()))
	assert.Equal(t, int32(1), count.Load())
}

func TestShutdown_OrdersMultipleReverse(t *testing.T) {
	var order []string
	var count atomic.Int32

	type leaf struct{ *closable }
	type mid struct {
		*closable
		l *leaf
	}

	inj := NewInjector()
	inj.Inject(func() *leaf { return &leaf{&closable{name: "leaf", log: &order, closed: &count}} })
	inj.Inject(func(l *leaf) *mid { return &mid{closable: &closable{name: "mid", log: &order, closed: &count}, l: l} })

	_, err := Get[*mid](inj)
	require.NoError(t, err)

	require.NoError(t, inj.Shutdown(context.Background()))
	// leaf constructed before mid → mid closed first.
	assert.Equal(t, []string{"mid", "leaf"}, order)
}

func TestShutdown_JoinsErrors(t *testing.T) {
	errClose := errors.New("close failed")
	type bad struct{}

	inj := NewInjector()
	inj.Inject(func() *bad { return &bad{} })
	// Wrap a Shutdowner that fails via a closure-built instance.
	inj.Inject(func() Shutdowner { return shutdownerFunc(func(context.Context) error { return errClose }) })
	_, err := Get[Shutdowner](inj)
	require.NoError(t, err)

	err = inj.Shutdown(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, errClose))
}

type shutdownerFunc func(context.Context) error

func (f shutdownerFunc) Shutdown(ctx context.Context) error { return f(ctx) }

// ----------------------------------------------------------------------------
// Thread-safety: run under `go test -race`
// ----------------------------------------------------------------------------

func TestConcurrentResolve_NoRaceSharedSingleton(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(NewRepo) // NewRepo(db *Database) *Repo

	const goroutines = 64
	var wg sync.WaitGroup
	results := make([]*Repo, goroutines)
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(idx int) {
			defer wg.Done()
			r, err := Get[*Repo](inj)
			assert.NoError(t, err)
			results[idx] = r
		}(g)
	}
	wg.Wait()

	// Every goroutine observes the same *Database singleton.
	db := Must[*Database](inj)
	for _, r := range results {
		require.NotNil(t, r)
		assert.Same(t, db, r.DB)
	}
}

func TestConcurrentRegisterAndResolve_NoRace(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			inj.Inject(NewDB) // re-register same key concurrently
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, _ = Get[*Database](inj)
		}
	}()
	wg.Wait()
}
