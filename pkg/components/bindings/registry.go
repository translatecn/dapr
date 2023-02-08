// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package bindings

import (
	"github.com/dapr/components-contrib/bindings" // 定义了InputBinding、OutputBinding接口
	"github.com/dapr/dapr/pkg/components"         // ok
	"github.com/pkg/errors"                       // ok
	"strings"
)

//type InputBinding interface {
//	Init(metadata Metadata) error
//	Read(handler func(*ReadResponse) ([]byte, error)) error
//}
//type OutputBinding interface {
//	Init(metadata Metadata) error
//	Invoke(req *InvokeRequest) (*InvokeResponse, error)
//	Operations() []OperationKind
//}

type (
	// InputBinding 是一个输入绑定组件的定义。
	InputBinding struct {
		Name          string
		FactoryMethod func() bindings.InputBinding
	}

	// OutputBinding 是一个输出绑定组件的定义。
	OutputBinding struct {
		Name          string
		FactoryMethod func() bindings.OutputBinding
	}

	// Registry 是一个组件的接口，允许调用者获得输入和输出绑定的注册实例。
	Registry interface {
		RegisterInputBindings(components ...InputBinding)
		RegisterOutputBindings(components ...OutputBinding)
		HasInputBinding(name, version string) bool
		HasOutputBinding(name, version string) bool
		CreateInputBinding(name, version string) (bindings.InputBinding, error)
		CreateOutputBinding(name, version string) (bindings.OutputBinding, error)
	}
	// 实现了上边的接口
	bindingsRegistry struct {
		inputBindings  map[string]func() bindings.InputBinding
		outputBindings map[string]func() bindings.OutputBinding
	}
)

// NewInput 创建输入绑定
func NewInput(name string, factoryMethod func() bindings.InputBinding) InputBinding {
	return InputBinding{
		Name:          name,
		FactoryMethod: factoryMethod,
	}
}

// NewOutput 创建输出绑定
func NewOutput(name string, factoryMethod func() bindings.OutputBinding) OutputBinding {
	return OutputBinding{
		Name:          name,
		FactoryMethod: factoryMethod,
	}
}

// NewRegistry 是用来创建绑定注册仓库
func NewRegistry() Registry {
	return &bindingsRegistry{
		inputBindings:  map[string]func() bindings.InputBinding{},
		outputBindings: map[string]func() bindings.OutputBinding{},
	}
}

// 获取绑定的名字
func createFullName(name string) string {
	return strings.ToLower("bindings." + name)
}

// RegisterInputBindings 注册输入绑定
func (b *bindingsRegistry) RegisterInputBindings(components ...InputBinding) {
	for _, component := range components {
		b.inputBindings[createFullName(component.Name)] = component.FactoryMethod
	}
}

// RegisterOutputBindings 注册输出绑定
func (b *bindingsRegistry) RegisterOutputBindings(components ...OutputBinding) {
	for _, component := range components {
		b.outputBindings[createFullName(component.Name)] = component.FactoryMethod
	}
}

//获取输入绑定的工厂函数
func (b *bindingsRegistry) getInputBinding(name, version string) (func() bindings.InputBinding, bool) {
	nameLower := strings.ToLower(name)
	versionLower := strings.ToLower(version)
	// 判断 f"{nameLower}/{versionLower}" 存不存在
	bindingFn, ok := b.inputBindings[nameLower+"/"+versionLower]
	if ok {
		return bindingFn, true
	}
	// 判断是不是初始版本
	if components.IsInitialVersion(versionLower) {
		bindingFn, ok = b.inputBindings[nameLower]
	}
	return bindingFn, ok
}

// CreateInputBinding 创建输入绑定实例，基于 name，version
func (b *bindingsRegistry) CreateInputBinding(name, version string) (bindings.InputBinding, error) {
	if method, ok := b.getInputBinding(name, version); ok {
		return method(), nil
	}
	return nil, errors.Errorf("couldn't find input binding %s/%s", name, version)
}

func (b *bindingsRegistry) getOutputBinding(name, version string) (func() bindings.OutputBinding, bool) {
	nameLower := strings.ToLower(name)
	versionLower := strings.ToLower(version)
	bindingFn, ok := b.outputBindings[nameLower+"/"+versionLower]
	if ok {
		return bindingFn, true
	}
	// 存在一个问题，如果上边获取不到,下边肯定也找不到
	// 因为存储都是"bindings." + name
	// 而这里只查找 name
	if components.IsInitialVersion(versionLower) {
		bindingFn, ok = b.outputBindings[nameLower]
	}
	return bindingFn, ok
}

// CreateOutputBinding 创建输出绑定实例，基于 name，version
func (b *bindingsRegistry) CreateOutputBinding(name, version string) (bindings.OutputBinding, error) {
	if method, ok := b.getOutputBinding(name, version); ok {
		return method(), nil
	}
	return nil, errors.Errorf("不能找到输出绑定 %s/%s", name, version)
}

// HasInputBinding 检查输入绑定存不存在，基于 name, version
func (b *bindingsRegistry) HasInputBinding(name, version string) bool {
	_, ok := b.getInputBinding(name, version)
	return ok
}

// HasOutputBinding 检查输出绑定存不存在，基于 name, version
func (b *bindingsRegistry) HasOutputBinding(name, version string) bool {
	_, ok := b.getOutputBinding(name, version)
	return ok
}

//res   why?
// 1、存储   以bindings.{name} 存储
// 2、以{nameLower}/{versionLower} 查找
// 2.1 以{nameLower} 查找

// eg
//  save     mockInputBinding/v2   				-->   bindings.mockInputBinding/v2
//  search   bindings.mockInputBinding   v2     -->   bindings.mockInputBinding/v2


