// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package bindings_test

import (
	b "github.com/dapr/components-contrib/bindings"
	"github.com/dapr/dapr/pkg/components/bindings" // ok
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

type (
	mockInputBinding struct {
		b.InputBinding
		// 匿名字段
		//目的是当前结构体实例可以用所有实现了
		//该接口的其他结构体来初始化（即使他们的属性不完全一致）

	}

	mockOutputBinding struct {
		b.OutputBinding
	}
)

func TestRegistry(t *testing.T) {
	testRegistry := bindings.NewRegistry()

	t.Run("input binding is registered", func(t *testing.T) {
		const (
			inputBindingName   = "mockInputBinding"
			inputBindingNameV2 = "mockInputBinding/v2"
			componentName      = "bindings." + inputBindingName
		)

		// 初始化mock对象,但是由于里边 接口字段为nil，因此不能调用那些方法
		mockInput := &mockInputBinding{}
		mockInputV2 := &mockInputBinding{}

		// 注册绑定时，会在name前添加  bindings.
		testRegistry.RegisterInputBindings(bindings.NewInput(inputBindingName, func() b.InputBinding {
			return mockInput
		}))
		testRegistry.RegisterInputBindings(bindings.NewInput(inputBindingNameV2, func() b.InputBinding {
			return mockInputV2
		}))

		// assert v0 and v1
		assert.True(t, testRegistry.HasInputBinding(componentName, "v0"))
		p, e := testRegistry.CreateInputBinding(componentName, "v0")
		assert.NoError(t, e)
		assert.Same(t, mockInput, p)
		p, e = testRegistry.CreateInputBinding(componentName, "v1")
		assert.NoError(t, e)
		assert.Same(t, mockInput, p)

		// assert v2
		assert.True(t, testRegistry.HasInputBinding(componentName, "v2"))
		pV2, e := testRegistry.CreateInputBinding(componentName, "v2")
		assert.NoError(t, e)
		assert.Same(t, mockInputV2, pV2)

		// check case-insensitivity
		pV2, e = testRegistry.CreateInputBinding(strings.ToUpper(componentName), "V2")
		assert.NoError(t, e)
		assert.Same(t, mockInputV2, pV2)
	})

	t.Run("input binding is not registered", func(t *testing.T) {
		const (
			inputBindingName = "fakeInputBinding"
			componentName    = "bindings." + inputBindingName
		)

		// act
		assert.False(t, testRegistry.HasInputBinding(componentName, "v0"))
		assert.False(t, testRegistry.HasInputBinding(componentName, "v1"))
		assert.False(t, testRegistry.HasInputBinding(componentName, "v2"))
		p, actualError := testRegistry.CreateInputBinding(componentName, "v1")
		expectedError := errors.Errorf("couldn't find input binding %s/v1", componentName)

		// assert
		assert.Nil(t, p)
		assert.Equal(t, expectedError.Error(), actualError.Error())
	})

	t.Run("output binding is registered", func(t *testing.T) {
		const (
			outputBindingName   = "mockInputBinding"
			outputBindingNameV2 = "mockInputBinding/v2"
			componentName       = "bindings." + outputBindingName
		)

		// Initiate mock object
		mockOutput := &mockOutputBinding{}
		mockOutputV2 := &mockOutputBinding{}

		// act
		testRegistry.RegisterOutputBindings(bindings.NewOutput(outputBindingName, func() b.OutputBinding {
			return mockOutput
		}))
		testRegistry.RegisterOutputBindings(bindings.NewOutput(outputBindingNameV2, func() b.OutputBinding {
			return mockOutputV2
		}))

		// assert v0 and v1
		assert.True(t, testRegistry.HasOutputBinding(componentName, "v0"))
		p, e := testRegistry.CreateOutputBinding(componentName, "v0")
		assert.NoError(t, e)
		assert.Same(t, mockOutput, p)
		assert.True(t, testRegistry.HasOutputBinding(componentName, "v1"))
		p, e = testRegistry.CreateOutputBinding(componentName, "v1")
		assert.NoError(t, e)
		assert.Same(t, mockOutput, p)

		// assert v2
		assert.True(t, testRegistry.HasOutputBinding(componentName, "v2"))
		pV2, e := testRegistry.CreateOutputBinding(componentName, "v2")
		assert.NoError(t, e)
		assert.Same(t, mockOutputV2, pV2)
	})

	t.Run("output binding is not registered", func(t *testing.T) {
		const (
			outputBindingName = "fakeOutputBinding"
			componentName     = "bindings." + outputBindingName
		)

		// act
		assert.False(t, testRegistry.HasOutputBinding(componentName, "v0"))
		assert.False(t, testRegistry.HasOutputBinding(componentName, "v1"))
		assert.False(t, testRegistry.HasOutputBinding(componentName, "v2"))
		p, actualError := testRegistry.CreateOutputBinding(componentName, "v1")
		expectedError := errors.Errorf("couldn't find output binding %s/v1", componentName)

		// assert
		assert.Nil(t, p)
		assert.Equal(t, expectedError.Error(), actualError.Error())
	})
}
