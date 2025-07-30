# Injector

A simple and lightweight dependency injection container for Go.

[![Go Reference](https://pkg.go.dev/badge/github.com/Javlopez/injector.svg)](https://pkg.go.dev/github.com/Javlopez/injector)
[![Go Report Card](https://goreportcard.com/badge/github.com/Javlopez/injector)](https://goreportcard.com/report/github.com/Javlopez/injector)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Features

- **Simple API**: Easy to use with minimal boilerplate
- **Factory Functions**: Register factory functions for lazy initialization
- **Direct Instances**: Register pre-created instances
- **Singleton Pattern**: Factory functions are called only once, instances are reused
- **Error Handling**: Proper error handling with optional panic mode
- **Zero Dependencies**: No external dependencies, uses only Go standard library
- **Thread-Safe**: Safe for concurrent use (coming soon)

## Installation

```bash
go get github.com/Javlopez/injector
```

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/Javlopez/injector"
)

type Database struct {
    Name string
}

func NewDB() *Database {
    return &Database{Name: "production-db"}
}

func main() {
    // Create injector
    inj := injector.NewInjector()
    
    // Register factory function
    inj.Inject(NewDB, "database")
    
    // Resolve dependency
    db := inj.MustResolve("database").(*Database)
    fmt.Println(db.Name) // Output: production-db
}
```

## Usage

### Creating an Injector

```go
injector := injector.NewInjector()
```

### Registering Dependencies

#### Option 1: Register Factory Functions

Factory functions are called lazily when the dependency is first resolved:

```go
func NewDatabase() *Database {
    return &Database{
        ConnectionString: "postgres://localhost:5432/mydb",
        MaxConnections:   10,
    }
}

// Register factory
injector.Inject(NewDatabase, "database")
```

#### Option 2: Register Instances Directly

Pre-created instances are stored and returned as-is:

```go
db := &Database{
    ConnectionString: "postgres://localhost:5432/mydb",
    MaxConnections:   10,
}

// Register instance
injector.Inject(db, "database")
```

### Resolving Dependencies

#### Using Resolve (with error handling)

```go
dep, err := injector.Resolve("database")
if err != nil {
    log.Fatal(err)
}
db := dep.(*Database)
```

#### Using MustResolve (panics on error)

```go
db := injector.MustResolve("database").(*Database)
```

### Complex Dependencies

You can register dependencies that depend on other dependencies:

```go
type UserService struct {
    DB     *Database
    Logger *Logger
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{
        DB:     db,
        Logger: logger,
    }
}

// Register dependencies
injector.Inject(NewDatabase, "database")
injector.Inject(NewLogger, "logger")

// Register service that depends on other services
injector.Inject(func() *UserService {
    db := injector.MustResolve("database").(*Database)
    logger := injector.MustResolve("logger").(*Logger)
    return NewUserService(db, logger)
}, "userService")

// Resolve the complex service
userSvc := injector.MustResolve("userService").(*UserService)
```

## Complete Example

```go
package main

import (
    "fmt"
    "log"
    "github.com/Javlopez/injector"
)

// Domain types
type Database struct {
    ConnectionString string
    MaxConnections   int
}

type Logger struct {
    Level string
}

type UserService struct {
    DB     *Database
    Logger *Logger
}

type UserController struct {
    UserService *UserService
    Logger      *Logger
}

// Factory functions
func NewDatabase() *Database {
    return &Database{
        ConnectionString: "postgres://localhost:5432/myapp",
        MaxConnections:   25,
    }
}

func NewLogger() *Logger {
    return &Logger{Level: "info"}
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{DB: db, Logger: logger}
}

func NewUserController(userSvc *UserService, logger *Logger) *UserController {
    return &UserController{UserService: userSvc, Logger: logger}
}

func main() {
    // Create injector
    inj := injector.NewInjector()
    
    // Register base dependencies
    inj.Inject(NewDatabase, "database")
    inj.Inject(NewLogger, "logger")
    
    // Register service layer
    inj.Inject(func() *UserService {
        db := inj.MustResolve("database").(*Database)
        logger := inj.MustResolve("logger").(*Logger)
        return NewUserService(db, logger)
    }, "userService")
    
    // Register controller layer
    inj.Inject(func() *UserController {
        userSvc := inj.MustResolve("userService").(*UserService)
        logger := inj.MustResolve("logger").(*Logger)
        return NewUserController(userSvc, logger)
    }, "userController")
    
    // Resolve and use
    controller := inj.MustResolve("userController").(*UserController)
    
    fmt.Printf("Database: %s\n", controller.UserService.DB.ConnectionString)
    fmt.Printf("Logger Level: %s\n", controller.Logger.Level)
}
```

## Best Practices

### 1. Use Factory Functions for Complex Dependencies

```go
// Good: Factory function handles complex initialization
func NewDatabaseConnection() *Database {
    config := loadConfig()
    db, err := sql.Open("postgres", config.ConnectionString)
    if err != nil {
        log.Fatal(err)
    }
    return &Database{conn: db}
}

injector.Inject(NewDatabaseConnection, "database")
```

### 2. Group Related Dependencies by Scope

```go
// Infrastructure layer
injector.Inject(NewDatabase, "database")
injector.Inject(NewRedisClient, "redis")
injector.Inject(NewLogger, "logger")

// Service layer
injector.Inject(NewUserService, "userService")
injector.Inject(NewOrderService, "orderService")

// Controller layer
injector.Inject(NewUserController, "userController")
injector.Inject(NewOrderController, "orderController")
```

### 3. Use Descriptive Names

```go
// Good
injector.Inject(NewPostgresDatabase, "postgresDatabase")
injector.Inject(NewRedisCache, "redisCache")

// Avoid
injector.Inject(NewDB, "db")
injector.Inject(NewCache, "cache")
```

### 4. Handle Errors Appropriately

```go
// In application startup (use MustResolve)
db := injector.MustResolve("database").(*Database)

// In request handlers (use Resolve)
dep, err := injector.Resolve("optionalService")
if err != nil {
    // Handle gracefully
    log.Printf("Optional service not available: %v", err)
}
```

## API Reference

### Types

#### `type Injector struct`

The main dependency injection container.

### Functions

#### `func NewInjector() *Injector`

Creates a new injector instance.

#### `func (i *Injector) Inject(dependency interface{}, name string)`

Registers a dependency with the given name. The dependency can be:
- A factory function that returns an instance
- A pre-created instance

#### `func (i *Injector) Resolve(name string) (interface{}, error)`

Resolves a dependency by name. Returns the instance and an error if not found.

#### `func (i *Injector) MustResolve(name string) interface{}`

Resolves a dependency by name. Panics if the dependency is not found.

## Testing

The package includes comprehensive tests. Run them with:

```bash
go test -v
go test -race
go test -cover
```

For benchmarks:

```bash
go test -bench=.
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Roadmap

- [ ] Thread-safety improvements
- [ ] Circular dependency detection
- [ ] Auto-wiring by type
- [ ] Lifecycle management (init/destroy hooks)
- [ ] Configuration from files (JSON/YAML)
- [ ] Performance optimizations

## FAQ

**Q: Is this thread-safe?**
A: Currently, no. Thread-safety is planned for a future release. For now, register all dependencies at application startup before concurrent access.

**Q: How does this compare to other DI containers?**
A: This injector focuses on simplicity and minimal overhead. It's perfect for small to medium applications that need basic dependency injection without complex features.

**Q: Can I register the same dependency with different names?**
A: Yes! You can register the same factory function or instance with multiple names.

**Q: What happens if I register a dependency twice with the same name?**
A: The second registration will override the first one.