package injector

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Build: eager construction surfaces runtime failures at startup
// ----------------------------------------------------------------------------

type eagerDB struct{ open bool }

func TestBuild_ConstructsEverything(t *testing.T) {
	var dbBuilt, repoBuilt bool
	inj := NewInjector()
	inj.Inject(func() *eagerDB { dbBuilt = true; return &eagerDB{open: true} })
	inj.Inject(func(db *eagerDB) *Repo { repoBuilt = true; return &Repo{} })

	require.NoError(t, inj.Build())
	assert.True(t, dbBuilt, "Build must construct the leaf")
	assert.True(t, repoBuilt, "Build must construct every provider")
}

func TestBuild_SurfacesRuntimeFactoryError(t *testing.T) {
	bootErr := errors.New("db ping failed")
	inj := NewInjector()
	// Statically valid (no missing deps), but fails at construction time.
	inj.Inject(func() (*eagerDB, error) { return nil, bootErr })
	inj.Inject(func(db *eagerDB) *Repo { return &Repo{} })

	// Validate passes — it never constructs.
	require.NoError(t, inj.Validate())

	// Build catches the runtime failure at startup.
	err := inj.Build()
	require.Error(t, err)
	assert.True(t, errors.Is(err, bootErr))
}

func TestBuild_FailsOnMissingDependency(t *testing.T) {
	inj := NewInjector()
	inj.Inject(func(db *eagerDB) *Repo { return &Repo{} }) // *eagerDB missing

	err := inj.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eagerDB")
}

// ----------------------------------------------------------------------------
// Panic recovery with resolution context
// ----------------------------------------------------------------------------

func TestPanicInConstructor_RecoveredWithChain(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(func(db *Database) *Repo {
		var p *Repo
		_ = p.DB.Name // nil deref -> panic
		return p
	})

	_, err := Get[*Repo](inj)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "panic constructing")
	assert.Contains(t, msg, "Repo")
}

// ----------------------------------------------------------------------------
// nil-interface return guard
// ----------------------------------------------------------------------------

func TestNilInterfaceReturn_DoesNotPanic(t *testing.T) {
	inj := NewInjector()
	// Factory returns a nil Mailer interface.
	inj.Inject(func() Mailer { return nil })
	// Consumer takes the Mailer; must receive a typed nil, not panic.
	inj.Inject(func(m Mailer) *Repo { return &Repo{} })

	r, err := Get[*Repo](inj)
	require.NoError(t, err)
	assert.NotNil(t, r)
}

// ----------------------------------------------------------------------------
// Strict mode: duplicate registration detection
// ----------------------------------------------------------------------------

func TestStrict_FlagsDuplicateTypeRegistration(t *testing.T) {
	inj := NewInjector().Strict()
	inj.Inject(NewDB)
	inj.Inject(NewDB) // duplicate *Database registration

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate registration")
	assert.Contains(t, err.Error(), "Database")
}

func TestStrict_FlagsDuplicateNameRegistration(t *testing.T) {
	inj := NewInjector().Strict()
	inj.InjectByName(NewDB, "db")
	inj.InjectByName(NewDB, "db")

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate registration for name "db"`)
}

func TestStrict_OverrideIsNotADuplicate(t *testing.T) {
	inj := NewInjector().Strict()
	inj.Inject(NewResendMailer)
	Override[Mailer](inj, OtherMailer{}) // explicit, not flagged

	require.NoError(t, inj.Validate())
}

func TestNonStrict_SilentOverrideStillWorks(t *testing.T) {
	inj := NewInjector() // not strict
	inj.Inject(NewDB)
	inj.Inject(NewDB)

	require.NoError(t, inj.Validate())
}

// ----------------------------------------------------------------------------
// Build + Shutdown end-to-end
// ----------------------------------------------------------------------------

type managedConn struct{ closed *bool }

func (c *managedConn) Shutdown(context.Context) error { *c.closed = true; return nil }

func TestBuild_ThenShutdown(t *testing.T) {
	closed := false
	inj := NewInjector()
	inj.Inject(func() *managedConn { return &managedConn{closed: &closed} })
	inj.Inject(func(c *managedConn) *Repo { return &Repo{} })

	require.NoError(t, inj.Build())
	require.NoError(t, inj.Shutdown(context.Background()))
	assert.True(t, closed, "Build-constructed Shutdowner must be closed")
}
