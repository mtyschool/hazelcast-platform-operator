//go:build localrun
// +build localrun

// This file is used  for running operator locally and connect to the cluster that uses ExposeExternally feature

package hazelcast

import (
	"github.com/hazelcast/hazelcast-go-client"
	hazelcastv1alpha1 "github.com/hazelcast/hazelcast-platform-operator/api/v1alpha1"
)

func buildConfig(h *hazelcastv1alpha1.Hazelcast) hazelcast.Config {
	config := hazelcast.Config{}
	cc := &config.Cluster
	cc.Name = h.Spec.ClusterName
	cc.Network.SetAddresses("127.0.0.1:8000")
	cc.Unisocket = true
	return config
}
