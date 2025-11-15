package injector

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type InjectorTestSuite struct {
	suite.Suite
	injector *Injector
}

func (suite *InjectorTestSuite) SetupTest() {
	suite.injector = NewInjector()
}

func (suite *InjectorTestSuite) TestNewInjector() {
	injector := NewInjector()

	assert.NotNil(suite.T(), injector)
	assert.NotNil(suite.T(), injector.dependencies)
	assert.NotNil(suite.T(), injector.factories)
	assert.Equal(suite.T(), 0, len(injector.dependencies))
	assert.Equal(suite.T(), 0, len(injector.factories))
}

func (suite *InjectorTestSuite) TestInjectInstance() {
	db := &Database{Name: "test-db"}

	suite.injector.Inject(db)

	assert.Equal(suite.T(), 1, len(suite.injector.typeRegistry))
	assert.Equal(suite.T(), 0, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 0, len(suite.injector.factories))

	dbType := reflect.TypeOf(db)
	storedDep, exists := suite.injector.typeRegistry[dbType]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), db, storedDep)
}

func (suite *InjectorTestSuite) TestInjectFactory() {
	suite.injector.Inject(NewDB)

	assert.Equal(suite.T(), 1, len(suite.injector.typeRegistry))
	assert.Equal(suite.T(), 0, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 0, len(suite.injector.factories))

	factoryType := reflect.TypeOf(NewDB)
	returnType := factoryType.Out(0) // *Database
	storedDep, exists := suite.injector.typeRegistry[returnType]
	assert.True(suite.T(), exists)
	assert.NotNil(suite.T(), storedDep)
}

func (suite *InjectorTestSuite) TestInjectByName() {
	db := &Database{Name: "test-db"}

	suite.injector.Inject(db)

	assert.Equal(suite.T(), 1, len(suite.injector.typeRegistry))
	assert.Equal(suite.T(), 0, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 0, len(suite.injector.factories))

	storedDep, err := suite.injector.ResolveByTypeName("Database")
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), db, storedDep)
}

func (suite *InjectorTestSuite) TestResolveFromFactory() {
	suite.injector.InjectByName(NewDB, "database")

	resolved, err := suite.injector.Resolve("database")

	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), resolved)

	resolvedDB, ok := resolved.(*Database)
	assert.True(suite.T(), ok)
	assert.Equal(suite.T(), "db", resolvedDB.Name)

	// After resolving from factory, it should be stored in dependencies (singleton)
	assert.Equal(suite.T(), 1, len(suite.injector.dependencies))
	storedDep, exists := suite.injector.dependencies["database"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), resolved, storedDep)
}

func (suite *InjectorTestSuite) TestSingletonBehavior() {
	suite.injector.InjectByName(NewDB, "database")

	resolved1, err1 := suite.injector.Resolve("database")
	resolved2, err2 := suite.injector.Resolve("database")

	assert.NoError(suite.T(), err1)
	assert.NoError(suite.T(), err2)
	assert.NotNil(suite.T(), resolved1)
	assert.NotNil(suite.T(), resolved2)

	assert.Same(suite.T(), resolved1, resolved2)
}

func (suite *InjectorTestSuite) TestResolveNonExistent() {
	resolved, err := suite.injector.Resolve("non-existent")

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), resolved)
	assert.Contains(suite.T(), err.Error(), "dependency 'non-existent' not found")
}

func (suite *InjectorTestSuite) TestMustResolveSuccess() {
	db := &Database{Name: "test-db"}
	suite.injector.InjectByName(db, "database")

	resolved := suite.injector.MustResolve("database")

	assert.NotNil(suite.T(), resolved)
	assert.Equal(suite.T(), db, resolved)
}

func (suite *InjectorTestSuite) TestMustResolvePanic() {
	assert.Panics(suite.T(), func() {
		suite.injector.MustResolve("non-existent")
	})
}

func (suite *InjectorTestSuite) TestComplexDependencyInjection() {
	suite.injector.InjectByName(NewDB, "database")

	suite.injector.InjectByName(func() *UserRepository {
		db := suite.injector.MustResolve("database").(*Database)
		return NewUserRepository(db)
	}, "userRepository")

	resolved, err := suite.injector.Resolve("userRepository")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), resolved)

	userRepo, ok := resolved.(*UserRepository)
	assert.True(suite.T(), ok)
	assert.NotNil(suite.T(), userRepo.DB)
	assert.Equal(suite.T(), "db", userRepo.DB.Name)

	assert.Equal(suite.T(), 2, len(suite.injector.dependencies))
}

func (suite *InjectorTestSuite) TestOverrideDependency() {
	db1 := &Database{Name: "db1"}
	suite.injector.InjectByName(db1, "database")

	db2 := &Database{Name: "db2"}
	suite.injector.InjectByName(db2, "database")

	// Should resolve to the second dependency
	resolved, err := suite.injector.Resolve("database")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), db2, resolved)

	resolvedDB := resolved.(*Database)
	assert.Equal(suite.T(), "db2", resolvedDB.Name)
}

func (suite *InjectorTestSuite) TestMultipleDependencies() {
	db := &Database{Name: "test-db"}
	userRepo := &UserRepository{DB: db}

	suite.injector.InjectByName(db, "database")
	suite.injector.InjectByName(userRepo, "userRepository")
	suite.injector.InjectByName(NewDB, "dbFactory")

	assert.Equal(suite.T(), 2, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 1, len(suite.injector.factories))

	resolvedDB, err1 := suite.injector.Resolve("database")
	resolvedUserRepo, err2 := suite.injector.Resolve("userRepository")
	resolvedFromFactory, err3 := suite.injector.Resolve("dbFactory")

	assert.NoError(suite.T(), err1)
	assert.NoError(suite.T(), err2)
	assert.NoError(suite.T(), err3)

	assert.Equal(suite.T(), db, resolvedDB)
	assert.Equal(suite.T(), userRepo, resolvedUserRepo)
	assert.NotNil(suite.T(), resolvedFromFactory)
}

// Run the test suite
func TestInjectorTestSuite(t *testing.T) {
	suite.Run(t, new(InjectorTestSuite))
}

func TestResolveByType_WithInstance(t *testing.T) {
	injector := NewInjector()
	db := &Database{Name: "test-db"}
	injector.Inject(db)
	resolved, err := ResolveByType[*Database](injector)

	assert.NoError(t, err)
	assert.NotNil(t, resolved)
	assert.Equal(t, "test-db", resolved.Name)
	assert.Same(t, db, resolved)
}

func TestResolveByType_WithFactory(t *testing.T) {
	injector := NewInjector()

	injector.Inject(NewDB)
	resolved, err := ResolveByType[*Database](injector)

	assert.NoError(t, err)
	assert.NotNil(t, resolved)
	assert.Equal(t, "db", resolved.Name)

	resolved2, err := ResolveByType[*Database](injector)
	assert.NoError(t, err)
	assert.Same(t, resolved, resolved2)
}

func TestResolveByType_NotFound(t *testing.T) {
	injector := NewInjector()

	resolved, err := ResolveByType[*Database](injector)

	assert.Error(t, err)
	assert.Nil(t, resolved)
	assert.Contains(t, err.Error(), "no dependency found for type")
}

func TestMustResolveByType_Success(t *testing.T) {
	injector := NewInjector()
	db := &Database{Name: "test-db"}
	injector.Inject(db)

	resolved := MustResolveByType[*Database](injector)

	assert.NotNil(t, resolved)
	assert.Equal(t, "test-db", resolved.Name)
}

func TestMustResolveByType_Panic(t *testing.T) {
	injector := NewInjector()

	// Should panic when dependency not found
	assert.Panics(t, func() {
		MustResolveByType[*Database](injector)
	})
}

func TestFor_Resolve_WithInstance(t *testing.T) {
	injector := NewInjector()
	db := &Database{Name: "test-db"}
	injector.Inject(db)
	resolved, err := For[*Database](injector).Resolve()

	assert.NoError(t, err)
	assert.NotNil(t, resolved)
	assert.Equal(t, "test-db", resolved.Name)
	assert.Same(t, db, resolved)
}

func TestFor_MustResolve_Success(t *testing.T) {
	injector := NewInjector()
	db := &Database{Name: "test-db"}
	injector.Inject(db)
	resolved := For[*Database](injector).MustResolve()

	assert.NotNil(t, resolved)
	assert.Equal(t, "test-db", resolved.Name)
}

func TestFor_MustResolve_Panic(t *testing.T) {
	injector := NewInjector()

	// Should panic when dependency not found
	assert.Panics(t, func() {
		For[*Database](injector).MustResolve()
	})
}

func TestResolveByType_WithComplexDependencies(t *testing.T) {
	injector := NewInjector()

	injector.Inject(NewDB)
	injector.Inject(func() *UserRepository {
		db := For[*Database](injector).MustResolve()
		return NewUserRepository(db)
	})
	db, err := For[*Database](injector).Resolve()
	assert.NoError(t, err)
	assert.NotNil(t, db)
	assert.Equal(t, "db", db.Name)
	userRepo, err := For[*UserRepository](injector).Resolve()
	assert.NoError(t, err)
	assert.NotNil(t, userRepo)
	assert.NotNil(t, userRepo.DB)
	assert.Equal(t, "db", userRepo.DB.Name)
	assert.Same(t, db, userRepo.DB)
}

func TestFor_WithFactory(t *testing.T) {
	injector := NewInjector()
	injector.Inject(NewDB)
	db1 := For[*Database](injector).MustResolve()
	assert.NotNil(t, db1)
	assert.Equal(t, "db", db1.Name)
	db2 := For[*Database](injector).MustResolve()
	assert.Same(t, db1, db2)
}

func TestResolveByType_TypeSafety(t *testing.T) {
	injector := NewInjector()
	db := &Database{Name: "test-db"}
	injector.Inject(db)

	resolved, err := ResolveByType[*Database](injector)
	assert.NoError(t, err)
	assert.NotNil(t, resolved)
	assert.Equal(t, "test-db", resolved.Name)
}

func TestGetAndMustShortcuts(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)

	// Using Get (error-returning)
	db, err := Get[*Database](inj)
	assert.NoError(t, err)
	assert.Equal(t, "db", db.Name)

	// Using Must (panic on error)
	db2 := Must[*Database](inj)
	assert.Equal(t, "db", db2.Name)
}

func TestResolveInto(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)

	var db *Database
	err := inj.ResolveInto(&db)
	assert.NoError(t, err)
	assert.NotNil(t, db)
	assert.Equal(t, "db", db.Name)
}

func TestInvoke_NoErrorReturn(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)

	called := false
	err := inj.Invoke(func(db *Database) {
		assert.NotNil(t, db)
		called = true
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestInvoke_WithErrorReturn(t *testing.T) {
	inj := NewInjector()
	inj.Inject(NewDB)

	err := inj.Invoke(func(db *Database) error {
		if db == nil {
			return fmt.Errorf("db is nil")
		}
		return nil
	})
	assert.NoError(t, err)
}

// Benchmark tests
func BenchmarkInjectInstance(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		injector.InjectByName(db, "database")
	}
}

func BenchmarkResolveInstance(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}
	injector.InjectByName(db, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = injector.Resolve("database")
	}
}

func BenchmarkResolveFromFactory(b *testing.B) {
	injector := NewInjector()
	injector.InjectByName(NewDB, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear dependencies to force factory call each time
		delete(injector.dependencies, "database")
		_, _ = injector.Resolve("database")
	}
}

func BenchmarkMustResolve(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}
	injector.InjectByName(db, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		injector.MustResolve("database")
	}
}

// -------------------------------------------------
// Example structs used for testing purposes
// -------------------------------------------------

func NewDB() *Database {
	return &Database{
		Name: "db",
	}
}

// -------------------------------------------------
// Example structs used for testing purposes
// -------------------------------------------------
type Database struct {
	Name string
}

func NewDatabase() *Database {
	return &Database{Name: "default-db"}
}

type UserRepository struct {
	DB *Database
}

func NewUserRepository(db *Database) *UserRepository {
	return &UserRepository{DB: db}
}

type UserService struct {
	Repo *UserRepository
}
