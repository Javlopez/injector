package injector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Groups
// ----------------------------------------------------------------------------

// route models an http.Handler-like member: many implementations share the same
// interface type and must all be collected, not deduplicated.
type route interface{ path() string }

type usersRoute struct{ db *Database }

func (usersRoute) path() string { return "/users" }

type ordersRoute struct{ db *Database }

func (ordersRoute) path() string { return "/orders" }

func newUsersRoute(db *Database) route  { return usersRoute{db: db} }
func newOrdersRoute(db *Database) route { return ordersRoute{db: db} }

func TestGroup_CollectsAllMembers(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB) // shared dependency auto-wired into each member

	inj.InjectGroup("routes", newUsersRoute)
	inj.InjectGroup("routes", newOrdersRoute)

	routes, err := ResolveGroup[route](inj, "routes")
	require.NoError(t, err)
	require.Len(t, routes, 2)

	paths := []string{routes[0].path(), routes[1].path()}
	assert.Contains(t, paths, "/users")
	assert.Contains(t, paths, "/orders")
}

func TestGroup_MembersShareSingletonDeps(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)
	inj.InjectGroup("routes", newUsersRoute)
	inj.InjectGroup("routes", newOrdersRoute)

	routes, err := ResolveGroup[route](inj, "routes")
	require.NoError(t, err)

	db := Must[*Database](inj)
	assert.Same(t, db, routes[0].(usersRoute).db)
	assert.Same(t, db, routes[1].(ordersRoute).db)
}

func TestGroup_InstanceMembers(t *testing.T) {
	inj := NewInjector()
	inj.InjectGroup("routes", usersRoute{})
	inj.InjectGroup("routes", ordersRoute{})

	routes, err := ResolveGroup[route](inj, "routes")
	require.NoError(t, err)
	assert.Len(t, routes, 2)
}

func TestGroup_EmptyGroupIsEmptySlice(t *testing.T) {
	inj := NewInjector()
	routes, err := ResolveGroup[route](inj, "missing")
	require.NoError(t, err)
	assert.Empty(t, routes)
}

func TestGroup_MissingMemberDependencyErrors(t *testing.T) {
	inj := NewInjector()
	// NewDB not registered → member factory cannot be wired.
	inj.InjectGroup("routes", newUsersRoute)

	_, err := ResolveGroup[route](inj, "routes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Database")
}

func TestGroup_ValidateCatchesMissingMemberDep(t *testing.T) {
	inj := NewInjector()
	inj.InjectGroup("routes", newUsersRoute) // needs *Database, not registered

	err := inj.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Database")
}

// ----------------------------------------------------------------------------
// Override (typed bind, incl. interfaces)
// ----------------------------------------------------------------------------

func TestOverride_BindsMockToInterface(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewResendMailer) // concrete satisfies Mailer

	real, err := Get[Mailer](inj)
	require.NoError(t, err)
	assert.IsType(t, &ResendMailer{}, real)

	// Swap the interface binding for a different implementation.
	Override[Mailer](inj, OtherMailer{})
	mock, err := Get[Mailer](inj)
	require.NoError(t, err)
	assert.IsType(t, OtherMailer{}, mock)
}

func TestOverride_ResolvesAmbiguityExplicitly(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewResendMailer)
	inj.Inject(NewOtherMailer)

	// Two concrete impls → ambiguous interface request.
	_, err := Get[Mailer](inj)
	require.Error(t, err)

	// An explicit Override pins the interface to one, removing the ambiguity.
	Override[Mailer](inj, &ResendMailer{From: "pinned"})
	m, err := Get[Mailer](inj)
	require.NoError(t, err)
	assert.Equal(t, "pinned", m.(*ResendMailer).From)
}

// ----------------------------------------------------------------------------
// Conditional registration (noop-vs-real selection)
// ----------------------------------------------------------------------------

func TestInjectOr_PicksByCondition(t *testing.T) {
	real := &ResendMailer{From: "real"}
	noop := &ResendMailer{From: "noop"}

	withCreds := NewInjector()
	withCreds.InjectOr(true, func() Mailer { return real }, func() Mailer { return noop })
	m, err := Get[Mailer](withCreds)
	require.NoError(t, err)
	assert.Equal(t, "real", m.(*ResendMailer).From)

	without := NewInjector()
	without.InjectOr(false, func() Mailer { return real }, func() Mailer { return noop })
	m2, err := Get[Mailer](without)
	require.NoError(t, err)
	assert.Equal(t, "noop", m2.(*ResendMailer).From)
}

func TestInjectIf_RegistersOnlyWhenTrue(t *testing.T) {
	on := NewInjector()
	on.InjectIf(true, NewDB)
	_, err := Get[*Database](on)
	assert.NoError(t, err)

	off := NewInjector()
	off.InjectIf(false, NewDB)
	_, err = Get[*Database](off)
	assert.Error(t, err)
}
