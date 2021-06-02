// Code generated by mockery v2.7.4. DO NOT EDIT.

package mocks

import mock "github.com/stretchr/testify/mock"

// ConfigService is an autogenerated mock type for the ConfigService type
type ConfigService struct {
	mock.Mock
}

// GetConfig provides a mock function with given fields: _a0
func (_m *ConfigService) GetConfig(_a0 string) (string, error) {
	ret := _m.Called(_a0)

	var r0 string
	if rf, ok := ret.Get(0).(func(string) string); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}