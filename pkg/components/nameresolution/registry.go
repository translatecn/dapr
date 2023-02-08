// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package nameresolution

import (
	"strings"

	"github.com/pkg/errors"

	nr "github.com/dapr/components-contrib/nameresolution"

	"github.com/dapr/dapr/pkg/components"
)

type (
	// NameResolution 是一个名称解析组件的定义。
	NameResolution struct {
		Name          string
		FactoryMethod func() nr.Resolver
	}

	// Registry 处理注册和创建名称解析组件。
	Registry interface {
		Register(components ...NameResolution)
		Create(name, version string) (nr.Resolver, error)
	}

	nameResolutionRegistry struct {
		resolvers map[string]func() nr.Resolver
	}
)

// New 创建一个名称解析
func New(name string, factoryMethod func() nr.Resolver) NameResolution {
	return NameResolution{
		Name:          name,
		FactoryMethod: factoryMethod,
	}
}

// NewRegistry 创建一个名称解析仓库.
func NewRegistry() Registry {
	return &nameResolutionRegistry{
		resolvers: map[string]func() nr.Resolver{},
	}
}

// Register 将一个或多个名称解析组件添加到注册表。
func (s *nameResolutionRegistry) Register(components ...NameResolution) {
	for _, component := range components {
		s.resolvers[createFullName(component.Name)] = component.FactoryMethod
	}
}

// Create 基于`name`实例化一个名字解析解析器。
func (s *nameResolutionRegistry) Create(name, version string) (nr.Resolver, error) {
	if method, ok := s.getResolver(createFullName(name), version); ok {
		return method(), nil
	}
	return nil, errors.Errorf("couldn't find name resolver %s/%s", name, version)
}

func (s *nameResolutionRegistry) getResolver(name, version string) (func() nr.Resolver, bool) {
	nameLower := strings.ToLower(name)
	versionLower := strings.ToLower(version)
	resolverFn, ok := s.resolvers[nameLower+"/"+versionLower]
	if ok {
		return resolverFn, true
	}
	if components.IsInitialVersion(versionLower) {
		resolverFn, ok = s.resolvers[nameLower]
	}
	return resolverFn, ok
}

func createFullName(name string) string {
	return strings.ToLower("nameresolution." + name)
}

//res   why?
// 1、存储   以nameresolution.{name} 存储
// 2、以{nameLower}/{versionLower} 查找
// 2.1 以{nameLower} 查找

// eg
//  save     mockResolver/v2   				-->   nameresolution.mockResolver/v2
//  search   nameresolution.mockResolver   v2     -->   nameresolution.mockResolver/v2
