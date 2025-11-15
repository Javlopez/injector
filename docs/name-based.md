# Name-Based Resolution

Classic registration and resolution by name. Useful when you need multiple instances of the same type with different configs.

## Register by name

```go
inj := injector.NewInjector()

// Register factory by name
inj.InjectByName(NewDatabase, "database")

// Register instance by name
cfg := &Config{Env: "prod"}
inj.InjectByName(cfg, "config")
```

## Resolve by name

```go
dep, err := inj.Resolve("database")
if err != nil { /* handle */ }
db := dep.(*Database)

// Or panic on error
db2 := inj.MustResolve("database").(*Database)
_ = db2
```

## Guidance
- Prefer type-based registration for most cases
- Use names when:
  - Multiple instances of the same type must coexist
  - You’re switching implementations behind an interface by explicit name
