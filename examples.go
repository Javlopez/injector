package injector

// Your Database struct example
type Database struct {
	Name string
}

func NewDB() *Database {
	return &Database{
		Name: "db",
	}
}

// Example of another dependency
type UserService struct {
	DB *Database
}

func NewUserService(db *Database) *UserService {
	return &UserService{
		DB: db,
	}
}
