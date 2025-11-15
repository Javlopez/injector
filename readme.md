# Injector

A simple, lightweight dependency injection container for Go.

## Highlights

- Resolve by type or name; prefer type-based for simplicity
- Invoke functions with parameters auto-wired by type
- Type-safe generics: For[T], ResolveByType[T]
- Shortcuts: Get[T], Must[T]
- Lazy factories and direct instances

## Installation

```bash
go get github.com/Javlopez/injector
```

## Quick Start

The easiest way to start is to register by type and use Invoke (or ResolveInto).

```go
package main

import (
    "fmt"
    "log"
    "github.com/Javlopez/injector"
)

type Database struct{ Name string }
func NewDB() *Database { return &Database{Name: "production-db"} }

func main() {
    inj := injector.NewInjector()
    inj.Inject(NewDB)

    // Invoke: auto-wires parameters by type
    if err := inj.Invoke(func(db *Database) {
        fmt.Println(db.Name)
    }); err != nil {
        log.Fatal(err)
    }

    // Or ResolveInto: write directly into your variable
    var db *Database
    if err := inj.ResolveInto(&db); err != nil {
        log.Fatal(err)
    }
    fmt.Println(db.Name)
}
```

## Documentation

See the docs for detailed guides and patterns:

- [ResolveInto](docs/resolve-into.md)
- [Invoke](docs/invoke.md)
- [Generics (For[T], ResolveByType)](docs/generics.md)
- [Must helpers](docs/must.md)
- [Get helper](docs/get.md)
- [Name-Based](docs/name-based.md)
- [API Reference](docs/api.md)

## Best Practices

See docs for wiring patterns and guidance:
- docs/invoke.md for service/controller wiring
- docs/resolve-into.md for in-place resolution

 

## Testing

Run tests:

```bash
go test ./...
```

## Performance

Benchmarks on Linux amd64 (13th Gen Intel Core i9-13980HX):

- Resolve instance: ~5.8 ns/op, 0 B/op, 0 allocs/op
- MustResolve: ~5.6 ns/op, 0 B/op, 0 allocs/op
- Inject instance: ~7.0 ns/op, 0 B/op, 0 allocs/op
- Resolve from factory (cold): ~273 ns/op, 40 B/op, 2 allocs/op

Notes:
- Factory functions run once per type; subsequent resolves are cached and as fast as instance resolution.

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Roadmap

- [x] Auto-wiring by type
- [x] Type-safe generic resolution (Go 1.18+)
- [x] Fluent For[T] API and shortcuts
- [ ] Thread-safety improvements
- [ ] Circular dependency detection
- [ ] Lifecycle management (init/destroy hooks)
- [ ] Configuration from files (JSON/YAML)
- [ ] Performance optimizations
- [ ] Scope management (singleton, transient, scoped)

## FAQ

**Q: Is this thread-safe?**
A: Currently, no. Thread-safety is planned for a future release. For now, register all dependencies at application startup before concurrent access.

**Q: How does this compare to other DI containers?**
A: This injector focuses on simplicity and minimal overhead. It's perfect for small to medium applications that need basic dependency injection without complex features. With the addition of generic type resolution, it now offers modern type-safety while maintaining simplicity.

**Q: Can I register the same dependency with different names?**
A: Yes! You can register the same factory function or instance with multiple names.

**Q: What happens if I register a dependency twice with the same name?**
A: The second registration will override the first one.

**Q: Should I use name-based or type-based resolution?**
A: For new code, **type-based resolution with generics is recommended** because:
- It's type-safe at compile time
- No type assertions needed
- Better IDE support and autocomplete
- Less error-prone

However, name-based resolution is still useful when:
- You need multiple instances of the same type with different configurations
- You're working with interfaces and want to switch implementations

**Q: Which generic API should I use: For[T]() or ResolveByType[T]()?**
A: Both are type-safe and work identically. Choose based on style preference:
- `For[T](inj).MustResolve()` - Fluent, object-oriented style
- `MustResolveByType[T](inj)` - Function-based, more compact

**Q: Do I need Go 1.18+ for the generic features?**
A: Yes, the generic type-safe resolution features (`For[T]`, `ResolveByType[T]`, `MustResolveByType[T]`) require Go 1.18 or later. The traditional name-based resolution works with any Go version.

**Q: Should I use the global injector pattern?**
A: This library no longer provides a global injector to avoid global state. Instead, pass the injector instance where needed, or create a wrapper in your application if you need global access.

**Q: Can I mix registration strategies?**
A: Yes! You can use `Inject()`, `InjectByName()`, and resolve with any method. However, for consistency and maintainability, it's recommended to pick one primary strategy for your project.