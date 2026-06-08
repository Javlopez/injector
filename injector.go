package injector

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// errType is the reflect.Type of the error interface, used to detect a
// trailing error return on factory functions.
var errType = reflect.TypeOf((*error)(nil)).Elem()

// Shutdowner is implemented by dependencies that need to release resources.
// Any instance constructed by a factory and implementing this interface is
// tracked and closed (in reverse construction order) by Injector.Shutdown.
// Pre-registered instances are NOT tracked — their lifecycle stays with whoever
// created them.
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// Injector handles dependency registration and resolution
type Injector struct {
	mu           sync.Mutex
	dependencies map[string]interface{}
	factories    map[string]reflect.Value
	typeRegistry map[reflect.Type]interface{}
	// cells memoizes constructed singletons keyed by reflect.Type (unqualified)
	// or qualKey (qualified). Each cell's sync.Once guarantees a provider runs
	// exactly once even under concurrent first-resolution — no double-checked
	// locking, no double construction.
	cells map[any]*cell
	// qualified holds providers keyed by (type, name) so several providers of
	// one type can coexist (e.g. a "primary" and "replica" *sql.DB).
	qualified map[qualKey]interface{}
	// groups holds named collections of providers resolved together as a slice
	// (e.g. all HTTP handlers). Members are NOT deduplicated by type.
	groups map[string][]interface{}
	// shutdownOrder records constructed Shutdowner instances in the order they
	// were built, so Shutdown can close them in reverse.
	shutdownOrder []Shutdowner
	// startOrder records constructed Startable instances in construction order,
	// so Start can run them in dependency order.
	startOrder []Startable
	// strict, when set, records duplicate registrations so Validate/Build can
	// report accidental double-wiring.
	strict  bool
	dupErrs []error
}

// NewInjector creates a new injector instance
func NewInjector() *Injector {
	return &Injector{
		dependencies: make(map[string]interface{}),
		factories:    make(map[string]reflect.Value),
		typeRegistry: make(map[reflect.Type]interface{}),
		cells:        make(map[any]*cell),
		qualified:    make(map[qualKey]interface{}),
		groups:       make(map[string][]interface{}),
	}
}

// qualKey identifies a qualified registration by its type and qualifier name.
type qualKey struct {
	t    reflect.Type
	name string
}

// cell memoizes a single constructed singleton. The sync.Once makes the factory
// run exactly once; concurrent callers block until it is built and observe the
// same value (or the same construction error).
type cell struct {
	once sync.Once
	val  interface{}
	err  error
}

// buildOnce constructs (or returns the memoized result of) the provider for a
// cache key. keyType is the registered type used for the resolution stack and
// error messages. A constructed Shutdowner is tracked for Shutdown.
func (i *Injector) buildOnce(cacheKey any, dep interface{}, keyType reflect.Type, stack []reflect.Type) (interface{}, error) {
	i.mu.Lock()
	c := i.cells[cacheKey]
	if c == nil {
		c = &cell{}
		i.cells[cacheKey] = c
	}
	i.mu.Unlock()

	c.once.Do(func() {
		// The factory runs WITHOUT the lock held (it resolves dependencies
		// re-entrantly); the cell's Once serializes construction per key.
		c.val, c.err = i.callFactory(dep, keyType, append(stack, keyType))
		if c.err == nil {
			i.trackLifecycle(c.val)
		}
	})
	return c.val, c.err
}

// trackLifecycle records a constructed instance for Start/Shutdown if it
// implements the corresponding interface.
func (i *Injector) trackLifecycle(val interface{}) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if s, ok := val.(Shutdowner); ok {
		i.shutdownOrder = append(i.shutdownOrder, s)
	}
	if s, ok := val.(Startable); ok {
		i.startOrder = append(i.startOrder, s)
	}
}

// Strict enables duplicate-registration detection: re-registering a type or
// name that is already present is recorded and surfaced by Validate and Build,
// catching accidental double-wiring. Returns the injector for chaining.
func (i *Injector) Strict() *Injector {
	i.mu.Lock()
	i.strict = true
	i.mu.Unlock()
	return i
}

// Module is a registrar function that wires a related set of providers into an
// injector. A module is just a function — there is no Option/Provide DSL — so a
// large composition root splits into focused units (wireBilling, wireAuth, …)
// with no new types to learn.
type Module = func(*Injector)

// Apply runs each module against the injector, in order, and returns it for
// chaining: injector.NewInjector().Apply(wireDB, wireAuth, wireBilling).
func (i *Injector) Apply(modules ...Module) *Injector {
	for _, m := range modules {
		m(i)
	}
	return i
}

// InjectByName registers a dependency with a given name.
// The dependency can be either an instance or a factory function.
func (i *Injector) InjectByName(dependency interface{}, name string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.strict {
		_, inDeps := i.dependencies[name]
		_, inFactories := i.factories[name]
		if inDeps || inFactories {
			i.dupErrs = append(i.dupErrs, fmt.Errorf("injector: duplicate registration for name %q", name))
		}
	}

	depType := reflect.TypeOf(dependency)

	if depType.Kind() == reflect.Func {
		i.factories[name] = reflect.ValueOf(dependency)
	} else {
		i.dependencies[name] = dependency
	}
}

// Inject registers a dependency by its type.
// Factory functions are registered by their first return type, instances by
// their concrete type. Factory parameters are auto-wired from the registry at
// resolution time, so the registration order does not matter.
func (i *Injector) Inject(dependency interface{}) {
	i.mu.Lock()
	defer i.mu.Unlock()

	depType := reflect.TypeOf(dependency)

	var key reflect.Type
	if depType.Kind() == reflect.Func {
		if depType.NumOut() > 0 {
			key = depType.Out(0)
		}
	} else {
		key = depType
	}
	if key == nil {
		return // factory with no return value: nothing to register
	}

	if i.strict {
		if _, exists := i.typeRegistry[key]; exists {
			i.dupErrs = append(i.dupErrs, fmt.Errorf("injector: duplicate registration for %v", key))
		}
	}
	i.typeRegistry[key] = dependency
}

// InjectIf registers the dependency only when cond is true. Sugar for the
// "real implementation or nothing" branch at the composition root.
func (i *Injector) InjectIf(cond bool, dependency interface{}) {
	if cond {
		i.Inject(dependency)
	}
}

// InjectOr registers primary when cond is true, otherwise fallback. This models
// the env-driven "real implementation when configured, noop otherwise"
// selection that pervades a composition root (mailer, push, search clients…).
// Note: the conditional choice still lives in caller code — a DI container does
// not remove provider selection, it only wires what it is given.
func (i *Injector) InjectOr(cond bool, primary, fallback interface{}) {
	if cond {
		i.Inject(primary)
	} else {
		i.Inject(fallback)
	}
}

// Override binds an explicit value to type T, replacing any existing
// registration (and discarding a previously cached singleton). T may be an
// interface, so unlike Inject — which keys a factory by its concrete return
// type — this binds a value directly against the interface a consumer depends
// on. Primarily for tests: build the real graph, then swap one collaborator for
// a mock.
func Override[T any](i *Injector, value T) {
	t := reflect.TypeFor[T]()
	i.mu.Lock()
	i.typeRegistry[t] = value
	i.mu.Unlock()
}

// ----------------------------------------------------------------------------
// Groups
// ----------------------------------------------------------------------------

// InjectGroup registers a dependency as a member of a named group. Members may
// be instances or factories; factory parameters are auto-wired from the type
// registry like any other provider. Resolve the whole group as a typed slice
// with ResolveGroup.
func (i *Injector) InjectGroup(group string, dependency interface{}) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.groups[group] = append(i.groups[group], dependency)
}

// ResolveGroup constructs every member of a named group and returns them as a
// typed slice. Each member is built fresh (members are not deduplicated by
// type), while their dependencies still come from the shared singleton
// registry.
func ResolveGroup[T any](i *Injector, group string) ([]T, error) {
	i.mu.Lock()
	members := append([]interface{}(nil), i.groups[group]...)
	i.mu.Unlock()

	out := make([]T, 0, len(members))
	for idx, dep := range members {
		inst, err := i.constructEntry(dep)
		if err != nil {
			return nil, fmt.Errorf("injector: group %q member %d: %w", group, idx, err)
		}
		typed, ok := inst.(T)
		if !ok {
			return nil, fmt.Errorf("injector: group %q member %d: cannot cast %T to %s",
				group, idx, inst, reflect.TypeFor[T]())
		}
		out = append(out, typed)
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// Qualified (named) registrations
// ----------------------------------------------------------------------------

// InjectQualified registers a dependency under a (type, name) qualifier, so
// several providers of the same type can coexist (e.g. a "primary" and a
// "replica" *sql.DB, or two implementations of one interface). Resolve with
// GetNamed / MustNamed, or with a `name:"..."` tag on an In-struct field.
// Factory parameters are auto-wired like any other provider.
func (i *Injector) InjectQualified(name string, dependency interface{}) {
	i.mu.Lock()
	defer i.mu.Unlock()

	depType := reflect.TypeOf(dependency)
	var key reflect.Type
	if depType.Kind() == reflect.Func {
		if depType.NumOut() > 0 {
			key = depType.Out(0)
		}
	} else {
		key = depType
	}
	if key == nil {
		return
	}

	qk := qualKey{key, name}
	if i.strict {
		if _, exists := i.qualified[qk]; exists {
			i.dupErrs = append(i.dupErrs, fmt.Errorf("injector: duplicate registration for %v name %q", key, name))
		}
	}
	i.qualified[qk] = dependency
}

// lookupQualified finds a qualified entry by (type, name): exact match, then by
// clean type name, then by assignability among entries with the same name.
// Multiple assignable matches under one name are reported as ambiguous. Caller
// must hold the lock.
func (i *Injector) lookupQualified(t reflect.Type, name string) (interface{}, qualKey, bool, error) {
	if d, ok := i.qualified[qualKey{t, name}]; ok {
		return d, qualKey{t, name}, true, nil
	}

	tn := i.getTypeName(t)
	var (
		mDep  interface{}
		mKey  qualKey
		mKeys []qualKey
	)
	for qk, d := range i.qualified {
		if qk.name != name {
			continue
		}
		if i.getTypeName(qk.t) == tn || qk.t.AssignableTo(t) {
			mDep, mKey = d, qk
			mKeys = append(mKeys, qk)
		}
	}
	switch len(mKeys) {
	case 0:
		return nil, qualKey{}, false, nil
	case 1:
		return mDep, mKey, true, nil
	default:
		cands := make([]reflect.Type, len(mKeys))
		for idx, k := range mKeys {
			cands[idx] = k.t
		}
		return nil, qualKey{}, false, &errAmbiguous{candidates: cands}
	}
}

// resolveQualified resolves a (type, name) dependency, auto-wiring and caching
// factory results like resolveType does for unqualified providers.
func (i *Injector) resolveQualified(t reflect.Type, name string, stack []reflect.Type) (interface{}, error) {
	i.mu.Lock()
	dep, qk, ok, lerr := i.lookupQualified(t, name)
	i.mu.Unlock()

	if lerr != nil {
		return nil, &ResolveError{Target: t, Chain: append(stack, t), Reason: reasonAmbiguous, Candidates: ambiguousCandidates(lerr)}
	}
	if !ok {
		suffix := ""
		if len(stack) > 0 {
			suffix = fmt.Sprintf(" (required by %s)", chainString(append(stack, t)))
		}
		return nil, fmt.Errorf("injector: no dependency found for type %v name %q%s", t, name, suffix)
	}

	if reflect.TypeOf(dep).Kind() != reflect.Func {
		return dep, nil
	}

	for _, s := range stack {
		if s == qk.t {
			return nil, &ResolveError{Target: qk.t, Chain: append(stack, qk.t), Reason: reasonCycle}
		}
	}

	return i.buildOnce(qk, dep, qk.t, stack)
}

// GetNamed resolves a qualified dependency by type and name.
func GetNamed[T any](i *Injector, name string) (T, error) {
	var zero T
	t := reflect.TypeFor[T]()
	inst, err := i.resolveQualified(t, name, nil)
	if err != nil {
		return zero, err
	}
	res, ok := inst.(T)
	if !ok {
		return zero, fmt.Errorf("injector: type mismatch: cannot cast %T to %s", inst, t)
	}
	return res, nil
}

// MustNamed is like GetNamed but panics on error.
func MustNamed[T any](i *Injector, name string) T {
	v, err := GetNamed[T](i, name)
	if err != nil {
		panic(err)
	}
	return v
}

// constructEntry builds a single dependency (instance or factory) WITHOUT
// caching it by type — used for group members, which may share a return type.
// Factory parameters resolve from the shared registry, and a constructed
// Shutdowner is tracked for Shutdown.
func (i *Injector) constructEntry(dep interface{}) (interface{}, error) {
	ft := reflect.TypeOf(dep)
	if ft.Kind() != reflect.Func {
		return dep, nil
	}
	if ft.NumOut() == 0 {
		return nil, fmt.Errorf("injector: group factory %v returns no values", ft)
	}

	inst, err := i.callFactory(dep, ft.Out(0), nil)
	if err != nil {
		return nil, err
	}
	i.trackLifecycle(inst)
	return inst, nil
}

// ----------------------------------------------------------------------------
// Resolution errors
// ----------------------------------------------------------------------------

// resolveReason classifies why a type-based resolution failed.
type resolveReason int

const (
	reasonNotFound resolveReason = iota
	reasonCycle
	reasonAmbiguous
)

// ResolveError describes a failure while resolving a type, carrying the chain
// of types that were being constructed so the message points at the exact spot
// in the dependency graph that broke.
type ResolveError struct {
	Target     reflect.Type   // the type that could not be resolved
	Chain      []reflect.Type // resolution path leading to Target (root first)
	Reason     resolveReason
	Candidates []reflect.Type // populated when Reason == reasonAmbiguous
}

// Error implements the error interface with a path-aware message.
func (e *ResolveError) Error() string {
	switch e.Reason {
	case reasonCycle:
		return fmt.Sprintf("injector: cyclic dependency detected: %s", chainString(e.Chain))
	case reasonAmbiguous:
		return fmt.Sprintf("injector: ambiguous dependency for %v: %d candidates assignable (%s)%s",
			e.Target, len(e.Candidates), typeList(e.Candidates), requiredBy(e.Chain, e.Target))
	default:
		return fmt.Sprintf("injector: no dependency found for type %v%s", e.Target, requiredBy(e.Chain, e.Target))
	}
}

// chainString renders a resolution path as "A → B → C".
func chainString(chain []reflect.Type) string {
	parts := make([]string, len(chain))
	for idx, t := range chain {
		parts[idx] = t.String()
	}
	return strings.Join(parts, " → ")
}

// typeList renders candidate types as a comma-separated list.
func typeList(types []reflect.Type) string {
	parts := make([]string, len(types))
	for idx, t := range types {
		parts[idx] = t.String()
	}
	return strings.Join(parts, ", ")
}

// requiredBy appends a " (required by ...)" suffix when the failing type was
// reached through one or more parent constructors.
func requiredBy(chain []reflect.Type, target reflect.Type) string {
	// The last element of chain is target itself; the parents are everything
	// before it. Only render the suffix when there is at least one parent.
	if len(chain) < 2 {
		return ""
	}
	return fmt.Sprintf(" (required by %s)", chainString(chain))
}

// ----------------------------------------------------------------------------
// Core recursive resolver
// ----------------------------------------------------------------------------

// resolveType is the single recursive resolver behind every type-based entry
// point (For, ResolveByType, ResolveInto, Invoke, ResolveByTypeName). It
// auto-wires factory parameters, detects cycles via the resolution stack, and
// returns a ResolveError with the full path on failure.
//
// stack holds the registered key types currently under construction (root
// first). The caller passes nil at the top level.
func (i *Injector) resolveType(t reflect.Type, stack []reflect.Type) (interface{}, error) {
	i.mu.Lock()
	dep, key, ok, err := i.lookup(t)
	i.mu.Unlock()

	if err != nil {
		return nil, &ResolveError{Target: t, Chain: append(stack, t), Reason: reasonAmbiguous, Candidates: ambiguousCandidates(err)}
	}
	if !ok {
		return nil, &ResolveError{Target: t, Chain: append(stack, t), Reason: reasonNotFound}
	}

	// A pre-registered instance (anything that is not a factory func) resolves
	// directly.
	if reflect.TypeOf(dep).Kind() != reflect.Func {
		return dep, nil
	}

	// dep is a factory keyed by `key`. Detect a cycle BEFORE the once-cell: a
	// re-entrant same-key resolution is the cycle, and entering the Once again
	// on the same goroutine would otherwise deadlock.
	for _, s := range stack {
		if s == key {
			return nil, &ResolveError{Target: key, Chain: append(stack, key), Reason: reasonCycle}
		}
	}

	return i.buildOnce(key, dep, key, stack)
}

// In is an embeddable marker. A constructor parameter whose struct embeds In
// has each of its exported fields resolved individually from the container,
// instead of the struct being treated as a single dependency. This keeps
// constructors with many dependencies readable:
//
//	type Deps struct {
//	    injector.In
//	    DB     *sql.DB
//	    Logger *slog.Logger
//	    Repo   UserRepo `optional:"true"`
//	}
//	func NewService(d Deps) *Service { ... }
//
// A field tagged `optional:"true"` is left as its zero value when unresolvable.
type In struct{}

var inType = reflect.TypeOf(In{})

// isInParam reports whether t is a struct that embeds In.
func isInParam(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	for f := 0; f < t.NumField(); f++ {
		sf := t.Field(f)
		if sf.Anonymous && sf.Type == inType {
			return true
		}
	}
	return false
}

// buildIn constructs an In-struct by resolving each exported field from the
// container. Fields tagged `optional:"true"` are left zero when unresolvable.
func (i *Injector) buildIn(t reflect.Type, stack []reflect.Type) (reflect.Value, error) {
	out := reflect.New(t).Elem()
	for f := 0; f < t.NumField(); f++ {
		sf := t.Field(f)
		if (sf.Anonymous && sf.Type == inType) || sf.PkgPath != "" {
			continue // the In marker, or an unexported field
		}

		var (
			inst interface{}
			err  error
		)
		if name := sf.Tag.Get("name"); name != "" {
			inst, err = i.resolveQualified(sf.Type, name, stack)
		} else {
			inst, err = i.resolveType(sf.Type, stack)
		}
		if err != nil {
			if sf.Tag.Get("optional") == "true" {
				continue
			}
			return reflect.Value{}, err
		}
		if inst != nil {
			out.Field(f).Set(reflect.ValueOf(inst))
		}
	}
	return out, nil
}

// callFactory invokes a factory function, resolving each of its parameters from
// the registry. A parameter that embeds In is expanded into its fields. It
// propagates a trailing error return, rejects variadic factories, and recovers
// any panic from the constructor — annotating it with the resolution path so a
// nil-deref deep in the graph yields a diagnosis instead of a bare stack trace.
func (i *Injector) callFactory(factory interface{}, out reflect.Type, stack []reflect.Type) (result interface{}, err error) {
	fv := reflect.ValueOf(factory)
	ft := fv.Type()

	if ft.IsVariadic() {
		return nil, fmt.Errorf("injector: cannot auto-wire variadic factory for %v", out)
	}

	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("injector: panic constructing %v (%s): %v", out, chainString(stack), r)
		}
	}()

	args := make([]reflect.Value, ft.NumIn())
	for idx := 0; idx < ft.NumIn(); idx++ {
		p := ft.In(idx)
		if isInParam(p) {
			v, ierr := i.buildIn(p, stack)
			if ierr != nil {
				return nil, ierr
			}
			args[idx] = v
			continue
		}
		arg, aerr := i.resolveType(p, stack)
		if aerr != nil {
			return nil, aerr
		}
		if arg == nil {
			// A provider returned a nil interface; pass a typed nil so the
			// reflect call does not panic with "zero Value argument".
			args[idx] = reflect.Zero(p)
		} else {
			args[idx] = reflect.ValueOf(arg)
		}
	}

	results := fv.Call(args)
	if len(results) == 0 {
		return nil, fmt.Errorf("injector: factory for %v returned no values", out)
	}

	// Propagate a trailing error return: func(...) (T, error).
	if n := ft.NumOut(); n >= 2 && ft.Out(n-1) == errType {
		if e := results[n-1].Interface(); e != nil {
			return nil, fmt.Errorf("injector: factory for %v failed: %w", out, e.(error))
		}
	}

	return results[0].Interface(), nil
}

// errAmbiguous wraps the candidate keys for an ambiguous interface match so the
// information can travel out of lookup without widening its signature.
type errAmbiguous struct{ candidates []reflect.Type }

func (e *errAmbiguous) Error() string { return "ambiguous" }

func ambiguousCandidates(err error) []reflect.Type {
	var a *errAmbiguous
	if errors.As(err, &a) {
		return a.candidates
	}
	return nil
}

// lookup finds the registry entry that satisfies t, returning the stored
// dependency (instance or factory) and the key under which it lives.
//
// Resolution order: (1) exact type match, (2) match by clean type name (handles
// *pkg.T vs T spellings), (3) assignability — used to satisfy an interface
// parameter from a concrete registration. An interface request that matches
// more than one concrete implementation is reported as ambiguous rather than
// resolved arbitrarily.
func (i *Injector) lookup(t reflect.Type) (dep interface{}, key reflect.Type, ok bool, err error) {
	if d, exists := i.typeRegistry[t]; exists {
		return d, t, true, nil
	}

	name := i.getTypeName(t)
	for registeredType, d := range i.typeRegistry {
		if i.getTypeName(registeredType) == name {
			return d, registeredType, true, nil
		}
	}

	// Assignability fallback (e.g. interface target satisfied by a concrete
	// registration). Collect all matches to detect ambiguity.
	var (
		matchDep  interface{}
		matchKey  reflect.Type
		matchKeys []reflect.Type
	)
	for registeredType, d := range i.typeRegistry {
		if registeredType.AssignableTo(t) {
			matchDep = d
			matchKey = registeredType
			matchKeys = append(matchKeys, registeredType)
		}
	}
	switch len(matchKeys) {
	case 0:
		return nil, nil, false, nil
	case 1:
		return matchDep, matchKey, true, nil
	default:
		return nil, nil, false, &errAmbiguous{candidates: matchKeys}
	}
}

// getTypeName extracts a clean type name, removing package prefixes and pointer markers.
func (i *Injector) getTypeName(t reflect.Type) string {
	name := t.String()

	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		name = parts[len(parts)-1]
	}

	name = strings.TrimPrefix(name, "*")
	return name
}

// ----------------------------------------------------------------------------
// Eager graph validation
// ----------------------------------------------------------------------------

// Validate statically checks the whole registered graph WITHOUT constructing
// anything: every factory parameter must be resolvable, no interface request may
// be ambiguous, and there must be no dependency cycles. It reports ALL problems
// at once (joined), so a misconfigured composition root fails fast at startup
// with a complete diagnosis instead of panicking lazily on first use.
//
// Call it once, right after registration, before serving traffic.
func (i *Injector) Validate() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Duplicate registrations recorded under Strict mode surface here.
	errs := append([]error(nil), i.dupErrs...)

	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path
		black = 2 // fully explored
	)
	color := make(map[reflect.Type]int)

	var (
		visit       func(key reflect.Type, dep interface{}, path []reflect.Type)
		check       func(p reflect.Type, optional bool, path []reflect.Type)
		checkParams func(ft reflect.Type, path []reflect.Type, label string)
	)

	// check validates a single dependency type against the registry and descends
	// into its provider for cycle detection.
	check = func(p reflect.Type, optional bool, path []reflect.Type) {
		depP, keyP, ok, lerr := i.lookup(p)
		chain := append(append([]reflect.Type{}, path...), p)
		switch {
		case lerr != nil:
			errs = append(errs, &ResolveError{Target: p, Chain: chain, Reason: reasonAmbiguous, Candidates: ambiguousCandidates(lerr)})
		case !ok:
			if !optional {
				errs = append(errs, &ResolveError{Target: p, Chain: chain, Reason: reasonNotFound})
			}
		case color[keyP] == gray:
			errs = append(errs, &ResolveError{Target: keyP, Chain: append(append([]reflect.Type{}, path...), keyP), Reason: reasonCycle})
		case color[keyP] == white:
			visit(keyP, depP, path)
		}
	}

	// checkParams checks every parameter of a factory, expanding In-structs into
	// their exported fields (honoring `optional:"true"`).
	checkParams = func(ft reflect.Type, path []reflect.Type, label string) {
		if ft.IsVariadic() {
			errs = append(errs, fmt.Errorf("injector: %svariadic factory cannot be auto-wired", label))
			return
		}
		for idx := 0; idx < ft.NumIn(); idx++ {
			p := ft.In(idx)
			if isInParam(p) {
				for f := 0; f < p.NumField(); f++ {
					sf := p.Field(f)
					if (sf.Anonymous && sf.Type == inType) || sf.PkgPath != "" {
						continue
					}
					optional := sf.Tag.Get("optional") == "true"
					if name := sf.Tag.Get("name"); name != "" {
						if _, _, ok, lerr := i.lookupQualified(sf.Type, name); lerr != nil {
							errs = append(errs, &ResolveError{Target: sf.Type, Chain: []reflect.Type{sf.Type}, Reason: reasonAmbiguous, Candidates: ambiguousCandidates(lerr)})
						} else if !ok && !optional {
							errs = append(errs, fmt.Errorf("injector: no dependency found for type %v name %q", sf.Type, name))
						}
						continue
					}
					check(sf.Type, optional, path)
				}
				continue
			}
			check(p, false, path)
		}
	}

	visit = func(key reflect.Type, dep interface{}, path []reflect.Type) {
		color[key] = gray
		path = append(path, key)
		if ft := reflect.TypeOf(dep); ft.Kind() == reflect.Func {
			checkParams(ft, path, fmt.Sprintf("factory for %v: ", key))
		}
		color[key] = black
	}

	for key, dep := range i.typeRegistry {
		if color[key] == white {
			visit(key, dep, nil)
		}
	}

	// Group members are not keyed by type (they may share a return type), so
	// validate each member's parameters directly.
	for group, members := range i.groups {
		for idx, dep := range members {
			if ft := reflect.TypeOf(dep); ft.Kind() == reflect.Func {
				checkParams(ft, nil, fmt.Sprintf("group %q member %d: ", group, idx))
			}
		}
	}

	// Qualified factories: validate their parameters (unqualified, resolved from
	// the type registry).
	for qk, dep := range i.qualified {
		if ft := reflect.TypeOf(dep); ft.Kind() == reflect.Func {
			checkParams(ft, nil, fmt.Sprintf("qualified %v name %q: ", qk.t, qk.name))
		}
	}

	return errors.Join(errs...)
}

// Build validates the graph and then eagerly constructs every registered
// provider (and every group member), so a constructor that only fails at
// runtime — a panic, a failing Ping, a bad config — surfaces at startup instead
// of on the first request. After Build returns nil, all singletons are
// constructed and cached. It is the recommended composition-root entry point.
func (i *Injector) Build() error {
	if err := i.Validate(); err != nil {
		return err
	}

	// Snapshot keys and groups under the lock; resolveType locks internally.
	i.mu.Lock()
	keys := make([]reflect.Type, 0, len(i.typeRegistry))
	for k := range i.typeRegistry {
		keys = append(keys, k)
	}
	qkeys := make([]qualKey, 0, len(i.qualified))
	for qk := range i.qualified {
		qkeys = append(qkeys, qk)
	}
	groupNames := make([]string, 0, len(i.groups))
	for g := range i.groups {
		groupNames = append(groupNames, g)
	}
	i.mu.Unlock()

	for _, k := range keys {
		if _, err := i.resolveType(k, nil); err != nil {
			return err
		}
	}
	for _, qk := range qkeys {
		if _, err := i.resolveQualified(qk.t, qk.name, nil); err != nil {
			return err
		}
	}
	for _, g := range groupNames {
		i.mu.Lock()
		members := append([]interface{}(nil), i.groups[g]...)
		i.mu.Unlock()
		for idx, dep := range members {
			if _, err := i.constructEntry(dep); err != nil {
				return fmt.Errorf("injector: group %q member %d: %w", g, idx, err)
			}
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Lifecycle
// ----------------------------------------------------------------------------

// Startable is implemented by dependencies that need an explicit start step
// (open a pool, launch a background loop, run migrations). Unlike fx.Lifecycle,
// a constructor never depends on the container: it just implements this
// interface, and the container discovers it.
type Startable interface {
	Start(ctx context.Context) error
}

// Start runs every constructed Startable in construction order (dependencies
// before dependents). If one fails, it rolls back by shutting down the
// already-started instances that are Shutdowners, in reverse, then returns the
// error. Call it after Build, before serving traffic.
func (i *Injector) Start(ctx context.Context) error {
	i.mu.Lock()
	order := append([]Startable(nil), i.startOrder...)
	i.mu.Unlock()

	started := make([]Startable, 0, len(order))
	for _, s := range order {
		if err := s.Start(ctx); err != nil {
			for j := len(started) - 1; j >= 0; j-- {
				if sd, ok := started[j].(Shutdowner); ok {
					_ = sd.Shutdown(ctx)
				}
			}
			return fmt.Errorf("injector: start failed: %w", err)
		}
		started = append(started, s)
	}
	return nil
}

// Shutdown closes every constructed Shutdowner in reverse construction order,
// joining any errors. It is idempotent: the tracked set is cleared, so a second
// call is a no-op. Only instances built by a factory are tracked — pre-registered
// instances are left to their owner.
func (i *Injector) Shutdown(ctx context.Context) error {
	i.mu.Lock()
	order := i.shutdownOrder
	i.shutdownOrder = nil
	i.mu.Unlock()

	var errs []error
	for idx := len(order) - 1; idx >= 0; idx-- {
		if err := order[idx].Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ----------------------------------------------------------------------------
// Type-based public API (routes through resolveType)
// ----------------------------------------------------------------------------

// ResolveByTypeName resolves a dependency by its type name string (e.g., "Database").
func (i *Injector) ResolveByTypeName(typeName string) (interface{}, error) {
	i.mu.Lock()
	var match reflect.Type
	for registeredType := range i.typeRegistry {
		if i.getTypeName(registeredType) == typeName {
			match = registeredType
			break
		}
	}
	i.mu.Unlock()

	if match != nil {
		return i.resolveType(match, nil)
	}
	return nil, fmt.Errorf("injector: no dependency found for type name %s", typeName)
}

// ResolveInto resolves a dependency by type into the provided pointer target.
// Target must be a non-nil pointer to the desired type (e.g., &db where db is *Database).
func (i *Injector) ResolveInto(target interface{}) error {
	if target == nil {
		return fmt.Errorf("injector: target is nil")
	}

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("injector: target must be a non-nil pointer")
	}

	elemType := v.Elem().Type()
	inst, err := i.resolveType(elemType, nil)
	if err != nil {
		return err
	}

	if inst == nil {
		v.Elem().Set(reflect.Zero(elemType))
		return nil
	}
	rv := reflect.ValueOf(inst)
	if !rv.Type().AssignableTo(elemType) {
		return fmt.Errorf("injector: resolved type %v is not assignable to %v", rv.Type(), elemType)
	}
	v.Elem().Set(rv)
	return nil
}

// Invoke calls the provided function, resolving its parameters by type from the injector.
// If the function returns an error as its last return value, it will be returned.
func (i *Injector) Invoke(fn interface{}) error {
	if fn == nil {
		return fmt.Errorf("injector: fn is nil")
	}
	fv := reflect.ValueOf(fn)
	ft := fv.Type()
	if ft.Kind() != reflect.Func {
		return fmt.Errorf("injector: fn must be a function")
	}

	args := make([]reflect.Value, ft.NumIn())
	for idx := 0; idx < ft.NumIn(); idx++ {
		p := ft.In(idx)
		if isInParam(p) {
			v, err := i.buildIn(p, nil)
			if err != nil {
				return err
			}
			args[idx] = v
			continue
		}
		inst, err := i.resolveType(p, nil)
		if err != nil {
			return err
		}
		if inst == nil {
			args[idx] = reflect.Zero(p)
		} else {
			args[idx] = reflect.ValueOf(inst)
		}
	}

	results := fv.Call(args)
	if n := ft.NumOut(); n > 0 && ft.Out(n-1) == errType {
		if !results[n-1].IsNil() {
			return results[n-1].Interface().(error)
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Name-based public API (separate registry, zero-arg factories)
// ----------------------------------------------------------------------------

// Resolve resolves a dependency by its name.
// Factory functions are called once and cached (singleton pattern).
func (i *Injector) Resolve(name string) (interface{}, error) {
	i.mu.Lock()
	if dep, exists := i.dependencies[name]; exists {
		i.mu.Unlock()
		return dep, nil
	}
	factory, exists := i.factories[name]
	i.mu.Unlock()

	if exists {
		// Called without the lock held — a name-based factory closure may call
		// back into Resolve, which would deadlock a non-reentrant mutex.
		results := factory.Call([]reflect.Value{})
		if len(results) > 0 {
			instance := results[0].Interface()
			i.mu.Lock()
			i.dependencies[name] = instance
			i.mu.Unlock()
			return instance, nil
		}
	}

	return nil, fmt.Errorf("dependency '%s' not found", name)
}

// MustResolve is like Resolve but panics if the dependency is not found.
func (i *Injector) MustResolve(name string) interface{} {
	dep, err := i.Resolve(name)
	if err != nil {
		panic(err)
	}
	return dep
}

// ----------------------------------------------------------------------------
// Generic type-safe API
// ----------------------------------------------------------------------------

// TypeResolver provides type-safe generic resolution for a specific type.
// Usage: db, err := injector.For[*Database](inj).Resolve()
type TypeResolver[T any] struct {
	injector *Injector
}

// For creates a TypeResolver for type-safe dependency resolution.
// Usage: db := injector.For[*Database](inj).MustResolve()
func For[T any](i *Injector) *TypeResolver[T] {
	return &TypeResolver[T]{injector: i}
}

// Resolve resolves a dependency by its type with error handling.
func (tr *TypeResolver[T]) Resolve() (T, error) {
	var zero T
	targetType := reflect.TypeFor[T]()

	inst, err := tr.injector.resolveType(targetType, nil)
	if err != nil {
		return zero, err
	}

	result, ok := inst.(T)
	if !ok {
		return zero, fmt.Errorf("injector: type mismatch: cannot cast %T to %s", inst, targetType)
	}
	return result, nil
}

// MustResolve is like Resolve but panics if the dependency is not found.
func (tr *TypeResolver[T]) MustResolve() T {
	dep, err := tr.Resolve()
	if err != nil {
		panic(err)
	}
	return dep
}

// ResolveByType resolves a dependency by its type using Go generics.
// This provides type-safe resolution without requiring a name.
// Usage: db, err := injector.ResolveByType[*Database](inj)
func ResolveByType[T any](i *Injector) (T, error) {
	return For[T](i).Resolve()
}

// MustResolveByType is like ResolveByType but panics if the dependency is not found.
// Usage: db := injector.MustResolveByType[*Database](inj)
func MustResolveByType[T any](i *Injector) T {
	return For[T](i).MustResolve()
}

// Get resolves a dependency by type with error handling (short alias of ResolveByType).
// Usage: db, err := injector.Get[*Database](inj)
func Get[T any](i *Injector) (T, error) { // syntactic sugar
	return ResolveByType[T](i)
}

// Must resolves a dependency by type and panics on error (short alias of MustResolveByType).
// Usage: db := injector.Must[*Database](inj)
func Must[T any](i *Injector) T { // syntactic sugar
	return MustResolveByType[T](i)
}
