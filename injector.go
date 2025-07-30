package injector

import (
	"fmt"
	"reflect"
)

// Injector handles dependency registration and resolution
type Injector struct {
	dependencies map[string]interface{}
	factories    map[string]reflect.Value
}

// NewInjector creates a new injector instance
func NewInjector() *Injector {
	return &Injector{
		dependencies: make(map[string]interface{}),
		factories:    make(map[string]reflect.Value),
	}
}

// Inject registers a dependency with its name
// Can be an instance or a factory function
func (i *Injector) Inject(dependency interface{}, name string) {
	depType := reflect.TypeOf(dependency)

	// If it's a function, store it as a factory
	if depType.Kind() == reflect.Func {
		i.factories[name] = reflect.ValueOf(dependency)
	} else {
		// If it's an instance, store it directly
		i.dependencies[name] = dependency
	}
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
