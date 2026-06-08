package injector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primary/replica model: two providers of the same type under different names.

func TestQualified_TwoInstancesSameType(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("primary", &Database{Name: "primary-db"})
	inj.InjectQualified("replica", &Database{Name: "replica-db"})

	p, err := GetNamed[*Database](inj, "primary")
	require.NoError(t, err)
	assert.Equal(t, "primary-db", p.Name)

	r, err := GetNamed[*Database](inj, "replica")
	require.NoError(t, err)
	assert.Equal(t, "replica-db", r.Name)
}

func TestQualified_FactoryAutoWired(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB) // unqualified *Database, shared
	inj.InjectQualified("main", func(db *Database) *UserRepository {
		return &UserRepository{DB: db}
	})

	repo, err := GetNamed[*UserRepository](inj, "main")
	require.NoError(t, err)
	assert.Same(t, Must[*Database](inj), repo.DB)
}

func TestQualified_SingletonCached(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("x", NewDB)

	a := MustNamed[*Database](inj, "x")
	b := MustNamed[*Database](inj, "x")
	assert.Same(t, a, b)
}

func TestQualified_NotFound(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("primary", &Database{Name: "p"})

	_, err := GetNamed[*Database](inj, "replica")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `name "replica"`)
}

func TestQualified_InterfaceResolution(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("transactional", NewResendMailer) // *ResendMailer
	inj.InjectQualified("bulk", NewOtherMailer)           // *OtherMailer

	m, err := GetNamed[Mailer](inj, "transactional")
	require.NoError(t, err)
	assert.IsType(t, &ResendMailer{}, m)
}

// In-struct field resolution by name tag.

type dualDBService struct {
	primary *Database
	replica *Database
}

type dualDBDeps struct {
	In
	Primary *Database `name:"primary"`
	Replica *Database `name:"replica"`
}

func newDualDBService(d dualDBDeps) *dualDBService {
	return &dualDBService{primary: d.Primary, replica: d.Replica}
}

func TestQualified_InStructNameTag(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("primary", &Database{Name: "primary-db"})
	inj.InjectQualified("replica", &Database{Name: "replica-db"})
	inj.Inject(newDualDBService)

	svc, err := Get[*dualDBService](inj)
	require.NoError(t, err)
	assert.Equal(t, "primary-db", svc.primary.Name)
	assert.Equal(t, "replica-db", svc.replica.Name)
}

func TestQualified_ValidateCatchesMissingNamed(t *testing.T) {
	inj := NewInjector()
	inj.InjectQualified("primary", &Database{Name: "primary-db"})
	// "replica" missing.
	inj.Inject(newDualDBService)

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `name "replica"`)
}

func TestQualified_StrictFlagsDuplicate(t *testing.T) {
	inj := NewInjector().Strict()
	inj.InjectQualified("primary", &Database{Name: "a"})
	inj.InjectQualified("primary", &Database{Name: "b"})

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate registration")
	assert.Contains(t, err.Error(), `name "primary"`)
}

func TestQualified_BuildConstructsNamed(t *testing.T) {
	var built bool
	inj := NewInjector()
	inj.InjectQualified("primary", func() *Database { built = true; return &Database{Name: "p"} })

	require.NoError(t, inj.Build())
	assert.True(t, built, "Build must construct qualified providers too")
}
