# API Reference

This page documents the public API of the injector package. See the other docs pages for patterns and guidance.

Package import:

```go
import "github.com/Javlopez/injector"
```

## Types

### type Injector

The main dependency injection container.

```go
type Injector struct { /* internal fields */ }
```

### func NewInjector() *Injector

Constructor.

```go
func NewInjector() *Injector
```

## Registration

### func (*Injector) InjectByName

Register a dependency with an explicit name. The dependency can be a factory (function that returns an instance) or a concrete instance.

```go
func (i *Injector) InjectByName(dependency interface{}, name string)
```

### func (*Injector) Inject

Register by type. Factories are registered by their return type; instances by their concrete type.

```go
func (i *Injector) Inject(dependency interface{})
```

## Resolution (by name)

### func (*Injector) Resolve

Resolve a dependency by name.

```go
func (i *Injector) Resolve(name string) (interface{}, error)
```

### func (*Injector) MustResolve

Resolve by name and panic on error.

```go
func (i *Injector) MustResolve(name string) interface{}
```

## Resolution (by type name)

### func (*Injector) ResolveByTypeName

Resolve by the type name string (e.g., "Database"). Note: still requires type assertions at the call site.

```go
func (i *Injector) ResolveByTypeName(typeName string) (interface{}, error)
```

## Resolution (type-safe generics)

### func For[T any](i *Injector) *TypeResolver[T]

Create a resolver for a specific type.

```go
func For[T any](i *Injector) *TypeResolver[T]
```

### type TypeResolver[T]

Fluent, type-safe resolver.

```go
type TypeResolver[T any] struct { /* internal fields */ }
```

#### func (*TypeResolver[T]) Resolve() (T, error)

```go
func (r *TypeResolver[T]) Resolve() (T, error)
```

#### func (*TypeResolver[T]) MustResolve() T

```go
func (r *TypeResolver[T]) MustResolve() T
```

### func ResolveByType[T any](i *Injector) (T, error)

Function-based resolution by type.

```go
func ResolveByType[T any](i *Injector) (T, error)
```

### func MustResolveByType[T any](i *Injector) T

Function-based, panicking variant.

```go
func MustResolveByType[T any](i *Injector) T
```

### func Get[T any](i *Injector) (T, error)

Shortcut for ResolveByType.

```go
func Get[T any](i *Injector) (T, error)
```

### func Must[T any](i *Injector) T

Shortcut for MustResolveByType.

```go
func Must[T any](i *Injector) T
```

## Convenience

### func (*Injector) ResolveInto(target interface{}) error

Resolve by type into a provided pointer.

```go
func (i *Injector) ResolveInto(target interface{}) error
```

### func (*Injector) Invoke(fn interface{}) error

Invoke a function with parameters auto-wired by type. If the last return value is an error, it's returned.

```go
func (i *Injector) Invoke(fn interface{}) error
```

## Notes
- Factories are invoked lazily and cached (singleton behavior)
- Prefer type-based registration/resolution for new code
- Use name-based registration when you need multiple instances of the same type
