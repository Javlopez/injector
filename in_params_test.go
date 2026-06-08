package injector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigService models a Healti-style constructor with many dependencies, taken as
// a single In-struct instead of a long positional parameter list.
type bigService struct {
	db     *Database
	repo   *UserRepository
	mailer Mailer
}

type bigDeps struct {
	In
	DB     *Database
	Repo   *UserRepository
	Mailer Mailer
}

func newBigService(d bigDeps) *bigService {
	return &bigService{db: d.DB, repo: d.Repo, mailer: d.Mailer}
}

func TestIn_StructParamsAreFieldWired(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(func(db *Database) *UserRepository { return &UserRepository{DB: db} })
	inj.Inject(NewResendMailer) // satisfies Mailer
	inj.Inject(newBigService)

	svc, err := Get[*bigService](inj)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.NotNil(t, svc.db)
	assert.NotNil(t, svc.repo)
	assert.NotNil(t, svc.mailer)
	// Shared singleton reaches both the struct field and the nested repo.
	assert.Same(t, svc.db, svc.repo.DB)
}

type optionalDeps struct {
	In
	DB     *Database
	Mailer Mailer `optional:"true"`
}

func newOptionalService(d optionalDeps) *bigService {
	return &bigService{db: d.DB, mailer: d.Mailer}
}

func TestIn_OptionalFieldLeftZeroWhenMissing(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	// No Mailer registered, but the field is optional.
	inj.Inject(newOptionalService)

	svc, err := Get[*bigService](inj)
	require.NoError(t, err)
	assert.NotNil(t, svc.db)
	assert.Nil(t, svc.mailer)
}

func TestIn_MissingRequiredFieldErrorsWithChain(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	// Repo and Mailer (required) not registered.
	inj.Inject(newBigService)

	_, err := Get[*bigService](inj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UserRepository")
}

func TestIn_ValidateExpandsStructFields(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(newBigService) // needs *UserRepository and Mailer (missing)

	err := inj.Validate()
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "UserRepository")
	assert.Contains(t, msg, "Mailer")
}

func TestIn_ValidateOptionalFieldDoesNotError(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(newOptionalService) // Mailer optional, DB present

	assert.NoError(t, inj.Validate())
}

func TestIn_WorksWithBuild(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(func(db *Database) *UserRepository { return &UserRepository{DB: db} })
	inj.Inject(NewResendMailer)
	inj.Inject(newBigService)

	require.NoError(t, inj.Build())
	svc := Must[*bigService](inj)
	assert.NotNil(t, svc.mailer)
}
