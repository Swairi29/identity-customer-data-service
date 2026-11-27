/*
 * Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package provider supplies the source service.
package provider

import (
	"github.com/wso2/identity-customer-data-service/internal/source/service"
)

// SourceProviderInterface defines the interface for the source provider.
type SourceProviderInterface interface {
	GetSourceService() service.SourceServiceInterface
}

// SourceProvider is the default implementation of SourceProviderInterface.
type SourceProvider struct{}

// NewSourceProvider creates a new instance of SourceProvider.
func NewSourceProvider() SourceProviderInterface {
	return &SourceProvider{}
}

// GetSourceService returns the source service instance.
func (p *SourceProvider) GetSourceService() service.SourceServiceInterface {
	return service.GetSourceService()
}
