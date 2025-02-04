// Copyright (c) The Cortex Authors.
// Licensed under the Apache License 2.0.

package ring

import (
	"fmt"
	"strings"
	"time"
)

type ReplicationStrategy interface {
	// Filter out unhealthy instances and checks if there're enough instances
	// for an operation to succeed. Returns an error if there are not enough
	// instances.
	Filter(instances []InstanceDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration, zoneAwarenessEnabled bool) (healthy []InstanceDesc, maxFailures int, err error)
}

type defaultReplicationStrategy struct{}

func NewDefaultReplicationStrategy() ReplicationStrategy {
	return &defaultReplicationStrategy{}
}

// Filter decides, given the set of instances eligible for a key,
// which instances you will try and write to and how many failures you will
// tolerate.
// - Filters out unhealthy instances so the one doesn't even try to write to them.
// - Checks there are enough instances for an operation to succeed.
// The instances argument may be overwritten.
func (s *defaultReplicationStrategy) Filter(instances []InstanceDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration, zoneAwarenessEnabled bool) ([]InstanceDesc, int, error) {
	now := time.Now()

	// Skip those that have not heartbeated in a while.
	var unhealthy []string
	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout, now) {
			i++
		} else {
			unhealthy = append(unhealthy, instances[i].Addr)
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	// We need a response from a quorum of instances, which is n/2 + 1.  In the
	// case of a node joining/leaving with extend-writes enabled, the actual replica
	// set will be bigger than the replication factor, so use the bigger or the two.
	if len(instances) > replicationFactor {
		replicationFactor = len(instances)
	}

	minSuccess := (replicationFactor / 2) + 1
	// This is just a shortcut - if there are not minSuccess available instances,
	// after filtering out dead ones, don't even bother trying.
	if len(instances) < minSuccess {
		var err error
		var unhealthyStr string
		if len(unhealthy) > 0 {
			unhealthyStr = fmt.Sprintf(" - unhealthy instances: %s", strings.Join(unhealthy, ","))
		}

		if zoneAwarenessEnabled {
			err = fmt.Errorf("at least %d live replicas required across different availability zones, could only find %d%s", minSuccess, len(instances), unhealthyStr)
		} else {
			err = fmt.Errorf("at least %d live replicas required, could only find %d%s", minSuccess, len(instances), unhealthyStr)
		}

		return nil, 0, err
	}

	return instances, len(instances) - minSuccess, nil
}

type ignoreUnhealthyInstancesReplicationStrategy struct{}

func NewIgnoreUnhealthyInstancesReplicationStrategy() ReplicationStrategy {
	return &ignoreUnhealthyInstancesReplicationStrategy{}
}

func (r *ignoreUnhealthyInstancesReplicationStrategy) Filter(instances []InstanceDesc, op Operation, _ int, heartbeatTimeout time.Duration, _ bool) (healthy []InstanceDesc, maxFailures int, err error) {
	now := time.Now()
	// Filter out unhealthy instances.
	var unhealthy []string
	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout, now) {
			i++
		} else {
			unhealthy = append(unhealthy, instances[i].Addr)
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	// We need at least 1 healthy instance no matter what is the replication factor set to.
	if len(instances) == 0 {
		var unhealthyStr string
		if len(unhealthy) > 0 {
			unhealthyStr = fmt.Sprintf(" - unhealthy instances: %s", strings.Join(unhealthy, ","))
		}
		return nil, 0, fmt.Errorf("at least 1 healthy replica required, could only find 0%s", unhealthyStr)
	}

	return instances, len(instances) - 1, nil
}

func (r *Ring) IsHealthy(instance *InstanceDesc, op Operation, now time.Time) bool {
	return instance.IsHealthy(op, r.cfg.HeartbeatTimeout, now)
}

// ReplicationFactor of the ring.
func (r *Ring) ReplicationFactor() int {
	return r.cfg.ReplicationFactor
}

// InstancesCount returns the number of instances in the ring.
func (r *Ring) InstancesCount() int {
	r.mtx.RLock()
	c := len(r.ringDesc.Ingesters)
	r.mtx.RUnlock()
	return c
}
