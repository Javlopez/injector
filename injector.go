package injector

import "fmt"

var (
	errInjectorNameCannotBeEmpty = fmt.Errorf("injector: name cannot be empty")
	errInjectorSourceCannotBeNil = fmt.Errorf("injector: source cannot be nil")
)

type Injector struct {
	dependencies map[string]interface{}
}

func New() *Injector {
	return &Injector{}
}

func (i *Injector) Inject(name string, source interface{}) error {
	if name == "" {
		return errInjectorNameCannotBeEmpty
	}

	if source == nil {
		return errInjectorSourceCannotBeNil
	}
	return nil
}

func (i *Injector) Get(name string) (interface{}, error) {
	_, ok := i.dependencies[name]
	if !ok {
		return nil, fmt.Errorf("dependency %s not found", name)
	}

	return nil, nil
}
