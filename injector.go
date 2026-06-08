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
	// groups holds named collections of providers resolved together as a slice
	// (e.g. all HTTP handlers). Members are NOT deduplicated by type.
	groups map[string][]interface{}
	// shutdownOrder records constructed Shutdowner instances in the order they
	// were built, so Shutdown can close them in reverse.
	shutdownOrder []Shutdowner
}

// NewInjector creates a new injector instance
func NewInjector() *Injector {
	return &Injector{
		dependencies: make(map[string]interface{}),
		factories:    make(map[string]reflect.Value),
		typeRegistry: make(map[reflect.Type]interface{}),
		groups:       make(map[string][]interface{}),
	}
}

// InjectByName registers a dependency with a given name.
// The dependency can be either an instance or a factory function.
func (i *Injector) InjectByName(dependency interface{}, name string) {
	i.mu.Lock()
	defer i.mu.Unlock()

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

	if depType.Kind() == reflect.Func {
		if depType.NumOut() > 0 {
			returnType := depType.Out(0)
			i.typeRegistry[returnType] = dependency
		}
	} else {
		i.typeRegistry[depType] = dependency
	}
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
	t := reflect.TypeOf((*T)(nil)).Elem()
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
				group, idx, inst, reflect.TypeOf((*T)(nil)).Elem())
		}
		out = append(out, typed)
	}
	return out, nil
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
	if s, ok := inst.(Shutdowner); ok {
		i.mu.Lock()
		i.shutdownOrder = append(i.shutdownOrder, s)
		i.mu.Unlock()
	}
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

	// A cached instance (anything that is not a factory func) resolves directly.
	if reflect.TypeOf(dep).Kind() != reflect.Func {
		return dep, nil
	}

	// dep is a factory keyed by `key`. Detect a cycle before descending.
	for _, s := range stack {
		if s == key {
			return nil, &ResolveError{Target: key, Chain: append(stack, key), Reason: reasonCycle}
		}
	}

	// The factory is called WITHOUT the lock held: a factory closure may resolve
	// other dependencies re-entrantly, and a non-reentrant mutex would deadlock.
	inst, ferr := i.callFactory(dep, key, append(stack, key))
	if ferr != nil {
		return nil, ferr
	}

	// Cache the constructed instance under its registered key (singleton).
	// Double-check: a concurrent goroutine may have cached the same key while we
	// were constructing — if so, discard ours and return the winner so callers
	// still observe a single shared instance.
	i.mu.Lock()
	if cached, exists := i.typeRegistry[key]; exists {
		if reflect.TypeOf(cached).Kind() != reflect.Func {
			i.mu.Unlock()
			return cached, nil
		}
	}
	i.typeRegistry[key] = inst
	if s, isShutdowner := inst.(Shutdowner); isShutdowner {
		i.shutdownOrder = append(i.shutdownOrder, s)
	}
	i.mu.Unlock()
	return inst, nil
}

// callFactory invokes a factory function, resolving each of its parameters from
// the registry. It propagates a trailing error return and rejects variadic
// factories, whose parameter list cannot be auto-wired unambiguously.
func (i *Injector) callFactory(factory interface{}, out reflect.Type, stack []reflect.Type) (interface{}, error) {
	fv := reflect.ValueOf(factory)
	ft := fv.Type()

	if ft.IsVariadic() {
		return nil, fmt.Errorf("injector: cannot auto-wire variadic factory for %v", out)
	}

	args := make([]reflect.Value, ft.NumIn())
	for idx := 0; idx < ft.NumIn(); idx++ {
		arg, err := i.resolveType(ft.In(idx), stack)
		if err != nil {
			return nil, err
		}
		args[idx] = reflect.ValueOf(arg)
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

	var errs []error

	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path
		black = 2 // fully explored
	)
	color := make(map[reflect.Type]int)

	var visit func(key reflect.Type, dep interface{}, path []reflect.Type)
	visit = func(key reflect.Type, dep interface{}, path []reflect.Type) {
		color[key] = gray
		path = append(path, key)

		if reflect.TypeOf(dep).Kind() == reflect.Func {
			ft := reflect.TypeOf(dep)
			if ft.IsVariadic() {
				errs = append(errs, fmt.Errorf("injector: cannot auto-wire variadic factory for %v", key))
			} else {
				for idx := 0; idx < ft.NumIn(); idx++ {
					p := ft.In(idx)
					depP, keyP, ok, lerr := i.lookup(p)
					switch {
					case lerr != nil:
						errs = append(errs, &ResolveError{Target: p, Chain: append(append([]reflect.Type{}, path...), p), Reason: reasonAmbiguous, Candidates: ambiguousCandidates(lerr)})
					case !ok:
						errs = append(errs, &ResolveError{Target: p, Chain: append(append([]reflect.Type{}, path...), p), Reason: reasonNotFound})
					case color[keyP] == gray:
						errs = append(errs, &ResolveError{Target: keyP, Chain: append(append([]reflect.Type{}, path...), keyP), Reason: reasonCycle})
					case color[keyP] == white:
						visit(keyP, depP, path)
					}
				}
			}
		}

		color[key] = black
	}

	for key, dep := range i.typeRegistry {
		if color[key] == white {
			visit(key, dep, nil)
		}
	}

	// Group members are not keyed by type (they may share a return type), so
	// validate each member's parameters directly and descend into the type
	// graph for cycle/missing checks.
	for group, members := range i.groups {
		for idx, dep := range members {
			ft := reflect.TypeOf(dep)
			if ft.Kind() != reflect.Func {
				continue
			}
			if ft.IsVariadic() {
				errs = append(errs, fmt.Errorf("injector: group %q member %d: variadic factory cannot be auto-wired", group, idx))
				continue
			}
			for k := 0; k < ft.NumIn(); k++ {
				p := ft.In(k)
				depP, keyP, ok, lerr := i.lookup(p)
				switch {
				case lerr != nil:
					errs = append(errs, &ResolveError{Target: p, Chain: []reflect.Type{p}, Reason: reasonAmbiguous, Candidates: ambiguousCandidates(lerr)})
				case !ok:
					errs = append(errs, &ResolveError{Target: p, Chain: []reflect.Type{p}, Reason: reasonNotFound})
				case color[keyP] == white:
					visit(keyP, depP, nil)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// ----------------------------------------------------------------------------
// Lifecycle
// ----------------------------------------------------------------------------

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
		inst, err := i.resolveType(ft.In(idx), nil)
		if err != nil {
			return err
		}
		args[idx] = reflect.ValueOf(inst)
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
	targetType := reflect.TypeOf((*T)(nil)).Elem()

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
