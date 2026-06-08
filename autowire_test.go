package injector

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Fixtures: a small multi-level graph that mirrors Healti's repo→service→handler
// shape, including an interface boundary (Mailer) satisfied by a concrete impl.
// ----------------------------------------------------------------------------

type Mailer interface{ Send(to string) error }

type ResendMailer struct{ From string }

func (m *ResendMailer) Send(string) error { return nil }

func NewResendMailer() *ResendMailer { return &ResendMailer{From: "no-reply@healti.com"} }

type Repo struct{ DB *Database }

func NewRepo(db *Database) *Repo { return &Repo{DB: db} }

type Service struct {
	Repo   *Repo
	Mailer Mailer
}

func NewService(r *Repo, m Mailer) *Service { return &Service{Repo: r, Mailer: m} }

type Handler struct{ Svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{Svc: s} }

// TestAutoWire_RecursiveGraph is the core capability that was missing: a factory
// with parameters gets its dependencies resolved recursively, regardless of
// registration order.
func TestAutoWire_RecursiveGraph(t *testing.T) {
	inj := NewInjector()
	// Register out of order on purpose.
	inj.Inject(NewHandler)
	inj.Inject(NewService)
	inj.Inject(NewRepo)
	inj.Inject(NewDB)
	inj.Inject(NewResendMailer) // *ResendMailer satisfies the Mailer interface param

	h, err := Get[*Handler](inj)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.NotNil(t, h.Svc)
	assert.NotNil(t, h.Svc.Repo)
	assert.Equal(t, "db", h.Svc.Repo.DB.Name)
	assert.NotNil(t, h.Svc.Mailer)
}

// TestAutoWire_Singleton verifies the whole graph shares one instance per type:
// the *Database reached through the Handler is the same as resolved directly.
func TestAutoWire_Singleton(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewHandler)
	inj.Inject(NewService)
	inj.Inject(NewRepo)
	inj.Inject(NewDB)
	inj.Inject(NewResendMailer)

	h := Must[*Handler](inj)
	db := Must[*Database](inj)
	assert.Same(t, db, h.Svc.Repo.DB)
}

// ----------------------------------------------------------------------------
// Interface binding
// ----------------------------------------------------------------------------

func TestInterfaceBinding_ConcreteSatisfiesInterface(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewResendMailer)

	m, err := Get[Mailer](inj)
	require.NoError(t, err)
	assert.NotNil(t, m)
}

type OtherMailer struct{}

func (OtherMailer) Send(string) error { return nil }
func NewOtherMailer() *OtherMailer    { return &OtherMailer{} }

func TestInterfaceBinding_AmbiguousIsAnError(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewResendMailer)
	inj.Inject(NewOtherMailer)

	_, err := Get[Mailer](inj)
	require.Error(t, err)
	var re *ResolveError
	require.True(t, errors.As(err, &re))
	assert.Equal(t, reasonAmbiguous, re.Reason)
	assert.Len(t, re.Candidates, 2)
	assert.Contains(t, err.Error(), "ambiguous dependency")
}

// ----------------------------------------------------------------------------
// Cycle detection
// ----------------------------------------------------------------------------

type CycA struct{ B *CycB }
type CycB struct{ A *CycA }

func NewCycA(b *CycB) *CycA { return &CycA{B: b} }
func NewCycB(a *CycA) *CycB { return &CycB{A: a} }

func TestCycleDetection_ReportsPath(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewCycA)
	inj.Inject(NewCycB)

	_, err := Get[*CycA](inj)
	require.Error(t, err)
	var re *ResolveError
	require.True(t, errors.As(err, &re))
	assert.Equal(t, reasonCycle, re.Reason)
	// Path should name both types and close the loop back to CycA.
	msg := err.Error()
	assert.Contains(t, msg, "cyclic dependency detected")
	assert.Contains(t, msg, "CycA")
	assert.Contains(t, msg, "CycB")
	assert.Contains(t, msg, "→")
}

// ----------------------------------------------------------------------------
// Contextual missing-dependency errors
// ----------------------------------------------------------------------------

func TestMissingDependency_ShowsResolutionChain(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewHandler)
	inj.Inject(NewService)
	inj.Inject(NewRepo)
	inj.Inject(NewResendMailer)
	// NewDB intentionally NOT registered → *Database is the missing leaf.

	_, err := Get[*Handler](inj)
	require.Error(t, err)
	var re *ResolveError
	require.True(t, errors.As(err, &re))
	assert.Equal(t, reasonNotFound, re.Reason)
	msg := err.Error()
	assert.Contains(t, msg, "Database")
	assert.Contains(t, msg, "required by")
	// The chain should walk Handler → Service → Repo → Database.
	assert.True(t, strings.Contains(msg, "Handler"), "chain should include the root: %s", msg)
}

// ----------------------------------------------------------------------------
// Factory error propagation
// ----------------------------------------------------------------------------

type Risky struct{}

var errBoom = errors.New("boom")

func NewRiskyOK() (*Risky, error)  { return &Risky{}, nil }
func NewRiskyBad() (*Risky, error) { return nil, errBoom }

func TestFactoryErrorReturn_Propagated(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewRiskyBad)

	_, err := Get[*Risky](inj)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errBoom), "wrapped factory error should be unwrappable")
}

func TestFactoryErrorReturn_OKValueUnwrapped(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewRiskyOK)

	r, err := Get[*Risky](inj)
	require.NoError(t, err)
	assert.NotNil(t, r)
}

// ----------------------------------------------------------------------------
// Variadic guard
// ----------------------------------------------------------------------------

func NewVariadic(parts ...string) *Database { return &Database{Name: fmt.Sprint(parts)} }

func TestVariadicFactory_Rejected(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewVariadic)

	_, err := Get[*Database](inj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variadic")
}
