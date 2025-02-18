package injector

import (
	"fmt"
	"testing"
)
import "github.com/stretchr/testify/assert"

func TestBasicInjector(t *testing.T) {
	type given struct {
		name        string
		injectable  interface{}
		recoverName string
	}

	type expected struct {
		err         error
		dependency  interface{}
		errorInvoke error
	}

	type test struct {
		name     string
		given    given
		expected expected
	}

	tests := []test{
		{
			name: "We raise an error if we injectable has empty name",
			given: given{
				name: "",
			},
			expected: expected{
				err: errInjectorNameCannotBeEmpty,
			},
		},
		{
			name: "Should to get an error since the injectable cannot be nil",
			given: given{
				name: "dependency",
			},
			expected: expected{
				err: errInjectorSourceCannotBeNil,
			},
		},
		{
			name: "Should be able to attach a dependency",
			given: given{
				name: "dependency",
				injectable: func() string {
					return "a dependency"
				},
				recoverName: "invalid dependency",
			},
			expected: expected{
				errorInvoke: fmt.Errorf("dependency %s not found", "invalid dependency"),
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			injector := New()
			err := injector.Inject(tc.given.name, tc.given.injectable)
			if tc.expected.err != nil {
				assert.Equal(t, tc.expected.err.Error(), err.Error())
				return
			}
			assert.NoError(t, err)

			dependency, err := injector.Get(tc.given.recoverName)
			if tc.expected.errorInvoke != nil {
				assert.Equal(t, tc.expected.errorInvoke.Error(), err.Error())
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expected.dependency, dependency)
			//dependency := injector.Get(tc.given.name)
			//assert.Equal(t, tc.expected.dependency, dependency)
		})
	}

}
