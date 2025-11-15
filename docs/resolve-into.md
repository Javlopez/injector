# ResolveInto

ResolveInto lets you resolve a dependency by its type directly into a provided pointer. It’s a simple, non-generic way to consume dependencies without type assertions.

## When to use
- You prefer explicit typed variables at the call site without generics
- You want to inject into existing variables or struct fields in-place
- You are writing helpers where passing pointers is natural

## Basic example

```go
package main

import (
    "log"
    "github.com/Javlopez/injector"
)

type Database struct{ Name string }

func NewDB() *Database { return &Database{Name: "prod"} }

func main() {
    inj := injector.NewInjector()
    inj.Inject(NewDB)

    var db *Database
    if err := inj.ResolveInto(&db); err != nil {
        log.Fatal(err)
    }
    // db is ready to use
}
```

## Multiple resolves

```go
var (
    db *Database
    logger *Logger
)

if err := inj.ResolveInto(&db); err != nil { /* handle */ }
if err := inj.ResolveInto(&logger); err != nil { /* handle */ }
```

## Error handling
- If the type isn’t registered, ResolveInto returns an error
- If you pass a non-pointer or a pointer to the wrong type, you’ll get an error

```go
var missing *Cache
if err := inj.ResolveInto(&missing); err != nil {
    // handle not registered error
}
```

## Tips
- Combine with small functions to isolate resolution from business code
- Prefer Invoke when you can pass a function; it’s more concise for multiple deps
