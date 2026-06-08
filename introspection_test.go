package injector

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraph_YieldsProvidersAndDeps(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(NewRepo) // NewRepo(db *Database) *Repo

	deps := map[string][]string{}
	for tp, ds := range inj.Graph() {
		names := make([]string, len(ds))
		for k, d := range ds {
			names[k] = d.String()
		}
		deps[tp.String()] = names
	}

	require.Contains(t, deps, "*injector.Repo")
	assert.Equal(t, []string{"*injector.Database"}, deps["*injector.Repo"])
	assert.Empty(t, deps["*injector.Database"]) // leaf, no deps
}

func TestGraph_EarlyBreakStops(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(NewRepo)

	count := 0
	for range inj.Graph() {
		count++
		break // range-over-func must honor break
	}
	assert.Equal(t, 1, count)
}

func TestGraph_ExpandsInStructFields(t *testing.T) {
	inj := NewInjector()
	inj.Inject(newBigService) // takes bigDeps{In; DB; Repo; Mailer}

	var deps []string
	for tp, ds := range inj.Graph() {
		if tp.String() == "*injector.bigService" {
			for _, d := range ds {
				deps = append(deps, d.String())
			}
		}
	}
	// In marker excluded; fields expanded.
	assert.Contains(t, deps, "*injector.Database")
	assert.Contains(t, deps, "*injector.UserRepository")
	assert.Contains(t, deps, "injector.Mailer")
	assert.NotContains(t, deps, "injector.In")
}

func TestDescribe_IsDeterministicAndComplete(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(NewRepo)
	inj.InjectQualified("primary", &Database{Name: "p"})
	inj.InjectGroup("routes", func(db *Database) Mailer { return &ResendMailer{} })

	a := inj.Describe()
	b := inj.Describe()
	assert.Equal(t, a, b, "Describe must be deterministic")

	assert.Contains(t, a, "providers")
	assert.Contains(t, a, "*injector.Repo ← *injector.Database")
	assert.Contains(t, a, "[named]")
	assert.Contains(t, a, `name "primary"`)
	assert.Contains(t, a, "[group] routes")
}

func TestDOT_ProducesValidGraphvizEdges(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.Inject(NewRepo)

	dot := inj.DOT()
	assert.True(t, strings.HasPrefix(dot, "digraph injector {"))
	assert.Contains(t, dot, `"*injector.Repo" -> "*injector.Database";`)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(dot), "}"))

	// Deterministic (edges sorted).
	assert.Equal(t, dot, inj.DOT())
}
