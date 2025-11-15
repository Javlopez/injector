# Invoke

Invoke lets the injector call your function and automatically supply its parameters by type. If your function returns an error as its last result, Invoke returns it.

## Why use Invoke
- Clean and concise wiring without explicit resolves
- Great for initializing services/controllers from registered factories
- Natural error propagation

## Basic example

```go
package main

import (
    "fmt"
    "log"
    "github.com/Javlopez/injector"
)

type Database struct{ Name string }
func NewDB() *Database { return &Database{Name: "prod"} }

func main() {
    inj := injector.NewInjector()
    inj.Inject(NewDB)

    if err := inj.Invoke(func(db *Database) {
        fmt.Println(db.Name)
    }); err != nil {
        log.Fatal(err)
    }
}
```

## With error return

```go
if err := inj.Invoke(func(db *Database) error {
    if db == nil { return fmt.Errorf("no db") }
    return nil
}); err != nil {
    // error from your function is propagated
}
```

## Wiring factories via Invoke

```go
// Register a service built from other dependencies
inj.Inject(func() *UserService {
    var svc *UserService
    if err := inj.Invoke(func(db *Database, logger *Logger) {
        svc = NewUserService(db, logger)
    }); err != nil { panic(err) }
    return svc
})
```

## Tips
- Keep invoked functions small and side-effect–aware
- Use for app startup wiring, controllers, and handlers
- For optional deps, handle missing registrations by checking errors
