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

	// Maps types to their instances/factories
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

// InjectByName registers a dependency with its name
// Can be an instance or a factory function
func (i *Injector) InjectByName(dependency interface{}, name string) {
	depType := reflect.TypeOf(dependency)

	// If it's a function, store it as a factory
	if depType.Kind() == reflect.Func {
		i.factories[name] = reflect.ValueOf(dependency)
	} else {
		// If it's an instance, store it directly
		i.dependencies[name] = dependency
	}
}

// Inject registers a dependency by its type (default behavior)
func (i *Injector) Inject(dependency interface{}) {
	depType := reflect.TypeOf(dependency)

	if depType.Kind() == reflect.Func {
		// If it's a function, register by its return type
		if depType.NumOut() > 0 {
			returnType := depType.Out(0)
			fmt.Printf("%+v", returnType)
			i.typeRegistry[returnType] = dependency
		}
	} else {
		// If it's an instance, register by its type
		i.typeRegistry[depType] = dependency
	}
}

// ResolveByTypeName resolves a dependency by its type name string
func (i *Injector) ResolveByTypeName(typeName string) (interface{}, error) {
	for registeredType, dependency := range i.typeRegistry {
		if i.getTypeName(registeredType) == typeName {
			return i.resolveRegisteredDependency(dependency, registeredType)
		}
	}
	return nil, fmt.Errorf("no dependency found for type name %s", typeName)
}

// Helper to resolve a registered dependency (instance or factory)
func (i *Injector) resolveRegisteredDependency(dependency interface{}, depType reflect.Type) (interface{}, error) {
	// Check if it's already an instance (not a function)
	if reflect.TypeOf(dependency).Kind() != reflect.Func {
		return dependency, nil
	}

	// It's a factory function, call it
	factoryValue := reflect.ValueOf(dependency)
	results := factoryValue.Call([]reflect.Value{})

	if len(results) == 0 {
		return nil, fmt.Errorf("factory function returned no values")
	}

	instance := results[0].Interface()

	// Store as singleton (replace factory with instance)
	i.typeRegistry[depType] = instance

	return instance, nil
}

// Helper function to get a clean type name
func (i *Injector) getTypeName(t reflect.Type) string {
	name := t.String()

	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		name = parts[len(parts)-1]
	}

	name = strings.TrimPrefix(name, "*")
	return name
}

// Resolve resolves a dependency by its name
func (i *Injector) Resolve(name string) (interface{}, error) {
	// First look for already created instances
	if dep, exists := i.dependencies[name]; exists {
		return dep, nil
	}

	// If not found, look for factories
	if factory, exists := i.factories[name]; exists {
		// Call the factory function
		results := factory.Call([]reflect.Value{})
		if len(results) > 0 {
			instance := results[0].Interface()
			// Store the instance for future resolutions (singleton pattern)
			i.dependencies[name] = instance
			return instance, nil
		}
	}

	return nil, fmt.Errorf("dependency '%s' not found", name)
}

// MustResolve is like Resolve but panics if dependency is not found
func (i *Injector) MustResolve(name string) interface{} {
	dep, err := i.Resolve(name)
	if err != nil {
		panic(err)
	}
	return dep
}
