package injector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// InjectorTestSuite defines our test suite
type InjectorTestSuite struct {
	suite.Suite
	injector *Injector
}

// SetupTest runs before each test
func (suite *InjectorTestSuite) SetupTest() {
	suite.injector = NewInjector()
}

// Test creating a new injector
func (suite *InjectorTestSuite) TestNewInjector() {
	injector := NewInjector()

	assert.NotNil(suite.T(), injector)
	assert.NotNil(suite.T(), injector.dependencies)
	assert.NotNil(suite.T(), injector.factories)
	assert.Equal(suite.T(), 0, len(injector.dependencies))
	assert.Equal(suite.T(), 0, len(injector.factories))
}

// Test injecting an instance directly
func (suite *InjectorTestSuite) TestInjectInstance() {
	db := &Database{Name: "test-db"}

	suite.injector.Inject(db, "database")

	// Should be stored in dependencies, not factories
	assert.Equal(suite.T(), 1, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 0, len(suite.injector.factories))

	// Verify the instance is stored correctly
	storedDep, exists := suite.injector.dependencies["database"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), db, storedDep)
}

// Test injecting a factory function
func (suite *InjectorTestSuite) TestInjectFactory() {
	suite.injector.Inject(NewDB, "database")

	// Should be stored in factories, not dependencies
	assert.Equal(suite.T(), 0, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 1, len(suite.injector.factories))

	// Verify the factory is stored
	_, exists := suite.injector.factories["database"]
	assert.True(suite.T(), exists)
}

// Test resolving an existing instance
func (suite *InjectorTestSuite) TestResolveInstance() {
	db := &Database{Name: "test-db"}
	suite.injector.Inject(db, "database")

	resolved, err := suite.injector.Resolve("database")

	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), resolved)
	assert.Equal(suite.T(), db, resolved)

	// Should be able to cast to Database
	resolvedDB, ok := resolved.(*Database)
	assert.True(suite.T(), ok)
	assert.Equal(suite.T(), "test-db", resolvedDB.Name)
}

// Test resolving from factory function
func (suite *InjectorTestSuite) TestResolveFromFactory() {
	suite.injector.Inject(NewDB, "database")

	resolved, err := suite.injector.Resolve("database")

	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), resolved)

	// Should be able to cast to Database
	resolvedDB, ok := resolved.(*Database)
	assert.True(suite.T(), ok)
	assert.Equal(suite.T(), "db", resolvedDB.Name)

	// After resolving from factory, it should be stored in dependencies (singleton)
	assert.Equal(suite.T(), 1, len(suite.injector.dependencies))
	storedDep, exists := suite.injector.dependencies["database"]
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), resolved, storedDep)
}

// Test singleton behavior - factory should only be called once
func (suite *InjectorTestSuite) TestSingletonBehavior() {
	suite.injector.Inject(NewDB, "database")

	// Resolve twice
	resolved1, err1 := suite.injector.Resolve("database")
	resolved2, err2 := suite.injector.Resolve("database")

	assert.NoError(suite.T(), err1)
	assert.NoError(suite.T(), err2)
	assert.NotNil(suite.T(), resolved1)
	assert.NotNil(suite.T(), resolved2)

	// Should be the exact same instance (pointer equality)
	assert.Same(suite.T(), resolved1, resolved2)
}

// Test resolving non-existent dependency
func (suite *InjectorTestSuite) TestResolveNonExistent() {
	resolved, err := suite.injector.Resolve("non-existent")

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), resolved)
	assert.Contains(suite.T(), err.Error(), "dependency 'non-existent' not found")
}

// Test MustResolve with existing dependency
func (suite *InjectorTestSuite) TestMustResolveSuccess() {
	db := &Database{Name: "test-db"}
	suite.injector.Inject(db, "database")

	// Should not panic
	resolved := suite.injector.MustResolve("database")

	assert.NotNil(suite.T(), resolved)
	assert.Equal(suite.T(), db, resolved)
}

// Test MustResolve with non-existent dependency (should panic)
func (suite *InjectorTestSuite) TestMustResolvePanic() {
	assert.Panics(suite.T(), func() {
		suite.injector.MustResolve("non-existent")
	})
}

// Test complex dependency injection scenario
func (suite *InjectorTestSuite) TestComplexDependencyInjection() {
	// Register database factory
	suite.injector.Inject(NewDB, "database")

	// Register user service factory that depends on database
	suite.injector.Inject(func() *UserService {
		db := suite.injector.MustResolve("database").(*Database)
		return NewUserService(db)
	}, "userService")

	// Resolve user service
	resolved, err := suite.injector.Resolve("userService")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), resolved)

	userSvc, ok := resolved.(*UserService)
	assert.True(suite.T(), ok)
	assert.NotNil(suite.T(), userSvc.DB)
	assert.Equal(suite.T(), "db", userSvc.DB.Name)

	// Both dependencies should be stored
	assert.Equal(suite.T(), 2, len(suite.injector.dependencies))
}

// Test overriding a dependency
func (suite *InjectorTestSuite) TestOverrideDependency() {
	// Register first dependency
	db1 := &Database{Name: "db1"}
	suite.injector.Inject(db1, "database")

	// Override with second dependency
	db2 := &Database{Name: "db2"}
	suite.injector.Inject(db2, "database")

	// Should resolve to the second dependency
	resolved, err := suite.injector.Resolve("database")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), db2, resolved)

	resolvedDB := resolved.(*Database)
	assert.Equal(suite.T(), "db2", resolvedDB.Name)
}

// Test injecting multiple dependencies
func (suite *InjectorTestSuite) TestMultipleDependencies() {
	db := &Database{Name: "test-db"}
	userSvc := &UserService{DB: db}

	suite.injector.Inject(db, "database")
	suite.injector.Inject(userSvc, "userService")
	suite.injector.Inject(NewDB, "dbFactory")

	assert.Equal(suite.T(), 2, len(suite.injector.dependencies))
	assert.Equal(suite.T(), 1, len(suite.injector.factories))

	// Resolve all
	resolvedDB, err1 := suite.injector.Resolve("database")
	resolvedUserSvc, err2 := suite.injector.Resolve("userService")
	resolvedFromFactory, err3 := suite.injector.Resolve("dbFactory")

	assert.NoError(suite.T(), err1)
	assert.NoError(suite.T(), err2)
	assert.NoError(suite.T(), err3)

	assert.Equal(suite.T(), db, resolvedDB)
	assert.Equal(suite.T(), userSvc, resolvedUserSvc)
	assert.NotNil(suite.T(), resolvedFromFactory)
}

// Run the test suite
func TestInjectorTestSuite(t *testing.T) {
	suite.Run(t, new(InjectorTestSuite))
}

// Benchmark tests
func BenchmarkInjectInstance(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		injector.Inject(db, "database")
	}
}

func BenchmarkResolveInstance(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}
	injector.Inject(db, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		injector.Resolve("database")
	}
}

func BenchmarkResolveFromFactory(b *testing.B) {
	injector := NewInjector()
	injector.Inject(NewDB, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear dependencies to force factory call each time
		delete(injector.dependencies, "database")
		injector.Resolve("database")
	}
}

func BenchmarkMustResolve(b *testing.B) {
	injector := NewInjector()
	db := &Database{Name: "bench-db"}
	injector.Inject(db, "database")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		injector.MustResolve("database")
	}
}

// -------------------------------------------------
// Example structs used for testing purposes
// -------------------------------------------------
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
