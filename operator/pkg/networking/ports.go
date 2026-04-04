// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package networking defines common networking logic for redpanda clusters
package networking

import (
	"fmt"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// ExternalPortDefinition defines external port for exposing a listener
type ExternalPortDefinition struct {
	resources.NamedServicePort
	// if this is set to true, it means that if using nodeport, we should let it
	// generate nodeport rather than fixing it to the given number. If this
	// property is set to false, External port will be used for both container
	// port as well as hostPort
	ExternalPortIsGenerated bool
	// For the Kafka API we support the option of having a bootstrap load balancer
	ExternalBootstrap *resources.NamedServicePort
}

// PortsDefinition defines internal/external pair of ports for exposed services
type PortsDefinition struct {
	Internal *resources.NamedServicePort
	External []ExternalPortDefinition
}

// RedpandaPorts defines ports for all redpanda listeners
type RedpandaPorts struct {
	KafkaAPI       PortsDefinition
	AdminAPI       PortsDefinition
	PandaProxy     PortsDefinition
	SchemaRegistry PortsDefinition
}

// NewRedpandaPorts intializes ports for all exposed services based on provided
// configuration and internal conventions.
func NewRedpandaPorts(rpCluster *vectorizedv1alpha1.Cluster) *RedpandaPorts {
	internalListener := rpCluster.InternalListener()
	externalListeners := rpCluster.KafkaAPIExternalListeners()
	adminAPIInternal := rpCluster.AdminAPIInternal()
	adminAPIExternal := rpCluster.AdminAPIExternal()
	proxyAPIInternal := rpCluster.PandaproxyAPIInternal()
	proxyAPIExternals := rpCluster.PandaproxyAPIExternalListeners()

	getPortName := func(listenerName, baseName string, i int) string {
		if listenerName != "" {
			return listenerName
		}
		if i == 0 {
			return baseName
		}
		return fmt.Sprintf("%s-%d", baseName, i)
	}

	result := &RedpandaPorts{}
	if internalListener != nil {
		result.KafkaAPI.Internal = &resources.NamedServicePort{
			Port: internalListener.Port,
			Name: resources.InternalListenerName,
		}
	}
	for i, externalListener := range externalListeners {
		if externalListener.External.ExcludeFromService {
			continue
		}

		portName := getPortName(externalListener.Name, resources.ExternalListenerName, i)
		externalPort := ExternalPortDefinition{}
		if externalListener.Port != 0 {
			externalPort.NamedServicePort = resources.NamedServicePort{
				Port: externalListener.Port,
				Name: portName,
			}
		} else {
			// if port is not set for Kafka API, we default to internal + 1
			externalPort.NamedServicePort = resources.NamedServicePort{
				Port: internalListener.Port + 1,
				Name: portName,
			}
			externalPort.ExternalPortIsGenerated = true
		}
		if externalListener.External.Bootstrap != nil {
			externalPort.ExternalBootstrap = &resources.NamedServicePort{
				Port:       externalListener.External.Bootstrap.Port,
				TargetPort: externalPort.NamedServicePort.Port,
				Name:       portName + "-bootstrap",
			}
		}
		result.KafkaAPI.External = append(result.KafkaAPI.External, externalPort)
	}
	if adminAPIInternal != nil {
		result.AdminAPI.Internal = &resources.NamedServicePort{
			Port: adminAPIInternal.Port,
			Name: resources.AdminPortName,
		}
	}
	if adminAPIExternal != nil && !adminAPIExternal.External.ExcludeFromService {
		externalPort := ExternalPortDefinition{}
		if adminAPIExternal.Port != 0 {
			externalPort.NamedServicePort = resources.NamedServicePort{
				Port: adminAPIExternal.Port,
				Name: resources.AdminPortExternalName,
			}
		} else {
			// if port is not set for admin API, we default to internal + 1
			externalPort.NamedServicePort = resources.NamedServicePort{
				Port: adminAPIInternal.Port + 1,
				Name: resources.AdminPortExternalName,
			}
			externalPort.ExternalPortIsGenerated = true
		}
		result.AdminAPI.External = append(result.AdminAPI.External, externalPort)
	}
	if proxyAPIInternal != nil {
		result.PandaProxy.Internal = &resources.NamedServicePort{
			Port: proxyAPIInternal.Port,
			Name: resources.PandaproxyPortInternalName,
		}

		for i, proxyAPIExternal := range proxyAPIExternals {
			if proxyAPIExternal.External.ExcludeFromService {
				continue
			}

			externalPort := ExternalPortDefinition{}
			portName := getPortName(proxyAPIExternal.Name, resources.PandaproxyPortExternalName, i)
			if proxyAPIExternal.Port != 0 {
				externalPort.NamedServicePort = resources.NamedServicePort{
					Port: proxyAPIExternal.Port,
					Name: portName,
				}
			} else {
				// if port is not set for pandaproxy, we default to internal + 1
				externalPort.NamedServicePort = resources.NamedServicePort{
					Port: proxyAPIInternal.Port + 1,
					Name: portName,
				}
				externalPort.ExternalPortIsGenerated = true
			}
			result.PandaProxy.External = append(result.PandaProxy.External, externalPort)
		}
	}

	for i, sr := range rpCluster.SchemaRegistryListeners() {
		externalPort := ExternalPortDefinition{}
		portName := getPortName(sr.Name, resources.SchemaRegistryPortName, i)
		schemaRegistryPort := resources.NamedServicePort{
			Port: sr.Port,
			Name: portName,
		}
		if sr.IsExternallyAvailable() {
			if sr.External.ExcludeFromService {
				continue
			}
			externalPort.NamedServicePort = schemaRegistryPort
			externalPort.ExternalPortIsGenerated = !sr.External.StaticNodePort
			result.SchemaRegistry.External = append(result.SchemaRegistry.External, externalPort)
		} else {
			result.SchemaRegistry.Internal = &schemaRegistryPort
		}
	}

	return result
}

// ToNamedServiceNodePort returns named node ports if available for given API. If
// no external port is defined, this will be nil
func (pd PortsDefinition) ToNamedServiceNodePorts() []resources.NamedServiceNodePort {
	if pd.External == nil {
		return nil
	}
	namedPorts := make([]resources.NamedServiceNodePort, 0, len(pd.External))
	for _, port := range pd.External {
		namedPorts = append(namedPorts, resources.NamedServiceNodePort{NamedServicePort: port.NamedServicePort, GenerateNodePort: port.ExternalPortIsGenerated})
	}
	return namedPorts
}

// InternalPort returns port of the internal listener
func (pd PortsDefinition) InternalPort() *int {
	if pd.Internal == nil {
		return nil
	}
	return &pd.Internal.Port
}
