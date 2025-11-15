package injector

import (
	"fmt"
	"reflect"
	"strings"
)

// Injector handles dependency registration and resolution
type Injector struct {
	dependencies map[string]interface{}
	factories    map[string]reflect.Value
	typeRegistry map[reflect.Type]interface{}
}

// NewInjector creates a new injector instance
func NewInjector() *Injector {
	return &Injector{
		dependencies: make(map[string]interface{}),
		factories:    make(map[string]reflect.Value),
		typeRegistry: make(map[reflect.Type]interface{}),
	}
}

// InjectByName registers a dependency with a given name.
// The dependency can be either an instance or a factory function.
func (i *Injector) InjectByName(dependency interface{}, name string) {
	depType := reflect.TypeOf(dependency)

	if depType.Kind() == reflect.Func {
		i.factories[name] = reflect.ValueOf(dependency)
	} else {
		i.dependencies[name] = dependency
	}
}

// Inject registers a dependency by its type.
// Factory functions are registered by their return type, instances by their concrete type.
func (i *Injector) Inject(dependency interface{}) {
	depType := reflect.TypeOf(dependency)

	if depType.Kind() == reflect.Func {
		if depType.NumOut() > 0 {
			returnType := depType.Out(0)
			fmt.Printf("%+v", returnType)
			i.typeRegistry[returnType] = dependency
		}
	} else {
		i.typeRegistry[depType] = dependency
	}
}

// ResolveByTypeName resolves a dependency by its type name string (e.g., "Database").
func (i *Injector) ResolveByTypeName(typeName string) (interface{}, error) {
	for registeredType, dependency := range i.typeRegistry {
		if i.getTypeName(registeredType) == typeName {
			return i.resolveRegisteredDependency(dependency, registeredType)
		}
	}
	return nil, fmt.Errorf("no dependency found for type name %s", typeName)
}

// resolveRegisteredDependency resolves either an instance or calls a factory function.
// Factory functions are called once and cached (singleton pattern).
func (i *Injector) resolveRegisteredDependency(dependency interface{}, depType reflect.Type) (interface{}, error) {
	if reflect.TypeOf(dependency).Kind() != reflect.Func {
		return dependency, nil
	}

	factoryValue := reflect.ValueOf(dependency)
	results := factoryValue.Call([]reflect.Value{})

	if len(results) == 0 {
		return nil, fmt.Errorf("factory function returned no values")
	}

	instance := results[0].Interface()
	i.typeRegistry[depType] = instance

	return instance, nil
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

// Resolve resolves a dependency by its name.
// Factory functions are called once and cached (singleton pattern).
func (i *Injector) Resolve(name string) (interface{}, error) {
	if dep, exists := i.dependencies[name]; exists {
		return dep, nil
	}

	if factory, exists := i.factories[name]; exists {
		results := factory.Call([]reflect.Value{})
		if len(results) > 0 {
			instance := results[0].Interface()
			i.dependencies[name] = instance
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

	if dependency, exists := tr.injector.typeRegistry[targetType]; exists {
		return tr.resolveDependency(dependency, targetType)
	}

	typeName := tr.injector.getTypeName(targetType)
	for registeredType, dependency := range tr.injector.typeRegistry {
		if tr.injector.getTypeName(registeredType) == typeName {
			return tr.resolveDependency(dependency, registeredType)
		}
	}

	return zero, fmt.Errorf("no dependency found for type %v", targetType)
}

// resolveDependency resolves and casts a registered dependency to the target type.
// Factory functions are called once and cached (singleton pattern).
func (tr *TypeResolver[T]) resolveDependency(dependency interface{}, depType reflect.Type) (T, error) {
	var zero T

	if reflect.TypeOf(dependency).Kind() != reflect.Func {
		result, ok := dependency.(T)
		if !ok {
			return zero, fmt.Errorf("type mismatch: cannot cast to %T", zero)
		}
		return result, nil
	}

	factoryValue := reflect.ValueOf(dependency)
	results := factoryValue.Call([]reflect.Value{})

	if len(results) == 0 {
		return zero, fmt.Errorf("factory function returned no values")
	}

	instance := results[0].Interface()
	tr.injector.typeRegistry[depType] = instance

	result, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("type mismatch: cannot cast to %T", zero)
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

// ResolveInto resolves a dependency by type into the provided pointer target.
// Target must be a non-nil pointer to the desired type (e.g., &db where db is *Database).
func (i *Injector) ResolveInto(target interface{}) error {
	if target == nil {
		return fmt.Errorf("target is nil")
	}

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}

	// Desired element type to assign to (e.g., *injector.Database)
	elemType := v.Elem().Type()

	// Try exact match first
	if dep, ok := i.typeRegistry[elemType]; ok {
		inst, err := i.resolveRegisteredDependency(dep, elemType)
		if err != nil {
			return err
		}
		rv := reflect.ValueOf(inst)
		if !rv.Type().AssignableTo(elemType) {
			return fmt.Errorf("resolved type %v is not assignable to %v", rv.Type(), elemType)
		}
		v.Elem().Set(rv)
		return nil
	}

	// Fallback: match by type name (e.g., Database vs *pkg.Database)
	typeName := i.getTypeName(elemType)
	for registeredType, dep := range i.typeRegistry {
		if i.getTypeName(registeredType) == typeName {
			inst, err := i.resolveRegisteredDependency(dep, registeredType)
			if err != nil {
				return err
			}
			rv := reflect.ValueOf(inst)
			if !rv.Type().AssignableTo(elemType) {
				return fmt.Errorf("resolved type %v is not assignable to %v", rv.Type(), elemType)
			}
			v.Elem().Set(rv)
			return nil
		}
	}

	return fmt.Errorf("no dependency found for type %v", elemType)
}

// Invoke calls the provided function, resolving its parameters by type from the injector.
// If the function returns an error as its last return value, it will be returned.
func (i *Injector) Invoke(fn interface{}) error {
	if fn == nil {
		return fmt.Errorf("fn is nil")
	}
	fv := reflect.ValueOf(fn)
	ft := fv.Type()
	if ft.Kind() != reflect.Func {
		return fmt.Errorf("fn must be a function")
	}

	// Build argument list by resolving each parameter type
	args := make([]reflect.Value, ft.NumIn())
	for idx := 0; idx < ft.NumIn(); idx++ {
		pType := ft.In(idx)

		// Try exact type match
		if dep, ok := i.typeRegistry[pType]; ok {
			inst, err := i.resolveRegisteredDependency(dep, pType)
			if err != nil {
				return err
			}
			args[idx] = reflect.ValueOf(inst)
			continue
		}

		// Fallback by type name
		var (
			found bool
			val   interface{}
		)
		for registeredType, dep := range i.typeRegistry {
			if i.getTypeName(registeredType) == i.getTypeName(pType) {
				inst, err := i.resolveRegisteredDependency(dep, registeredType)
				if err != nil {
					return err
				}
				val = inst
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no dependency found for parameter type %v", pType)
		}
		args[idx] = reflect.ValueOf(val)
	}

	results := fv.Call(args)
	// If last return is error, propagate it
	if ft.NumOut() > 0 {
		lastIdx := ft.NumOut() - 1
		if ft.Out(lastIdx) == reflect.TypeOf((*error)(nil)).Elem() {
			if !results[lastIdx].IsNil() {
				return results[lastIdx].Interface().(error)
			}
		}
	}
	return nil
}
