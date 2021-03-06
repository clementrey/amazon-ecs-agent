// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package stats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/dockerclient"
	"github.com/aws/amazon-ecs-agent/agent/dockerclient/dockerapi"
	"github.com/aws/amazon-ecs-agent/agent/stats/resolver"
	"github.com/aws/amazon-ecs-agent/agent/utils/retry"
	"github.com/cihub/seelog"
)

func newStatsContainer(dockerID string, client dockerapi.DockerClient, resolver resolver.ContainerMetadataResolver, cfg *config.Config) (*StatsContainer, error) {
	dockerContainer, err := resolver.ResolveContainer(dockerID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &StatsContainer{
		containerMetadata: &ContainerMetadata{
			DockerID:    dockerID,
			Name:        dockerContainer.Container.Name,
			NetworkMode: dockerContainer.Container.GetNetworkMode(),
		},
		ctx:      ctx,
		cancel:   cancel,
		client:   client,
		resolver: resolver,
		config:   cfg,
	}, nil
}

func (container *StatsContainer) StartStatsCollection() {
	// queue will be sized to hold enough stats for 4 publishing intervals.
	var queueSize int
	if container.config != nil && container.config.PollMetrics {
		pollingInterval := container.config.PollingMetricsWaitDuration.Seconds()
		queueSize = int(config.DefaultContainerMetricsPublishInterval.Seconds() / pollingInterval * 4)
	} else {
		// for streaming stats we assume 1 stat every second
		queueSize = int(config.DefaultContainerMetricsPublishInterval.Seconds() * 4)
	}
	container.statsQueue = NewQueue(queueSize)
	go container.collect()
}

func (container *StatsContainer) StopStatsCollection() {
	container.cancel()
}

func (container *StatsContainer) collect() {
	dockerID := container.containerMetadata.DockerID
	backoff := retry.NewExponentialBackoff(time.Second*1, time.Second*10, 0.5, 2)
	for {
		select {
		case <-container.ctx.Done():
			seelog.Debugf("Stopping stats collection for container %s", dockerID)
			return
		default:
			err := container.processStatsStream()
			if err != nil {
				d := backoff.Duration()
				seelog.Debugf("Error processing stats stream of container %s, backing off %s before reopening", dockerID, d)
				time.Sleep(d)
			}
			// We were disconnected from the stats stream.
			// Check if the container is terminal. If it is, stop collecting metrics.
			// We might sometimes miss events from docker task  engine and this helps
			// in reconciling the state.
			terminal, err := container.terminal()
			if err != nil {
				// Error determining if the container is terminal. This means that the container
				// id could not be resolved to a container that is being tracked by the
				// docker task engine. If the docker task engine has already removed
				// the container from its state, there's no point in stats engine tracking the
				// container. So, clean-up anyway.
				seelog.Warnf("Error determining if the container %s is terminal, stopping stats collection: %v", dockerID, err)
				container.StopStatsCollection()
			} else if terminal {
				seelog.Infof("Container %s is terminal, stopping stats collection", dockerID)
				container.StopStatsCollection()
			}
		}
	}
}

func (container *StatsContainer) processStatsStream() error {
	dockerID := container.containerMetadata.DockerID
	seelog.Debugf("Collecting stats for container %s", dockerID)
	if container.client == nil {
		return errors.New("container processStatsStream: Client is not set.")
	}
	dockerStats, errC := container.client.Stats(container.ctx, dockerID, dockerclient.StatsInactivityTimeout)

	returnError := false
	for {
		select {
		case err := <-errC:
			seelog.Warnf("Error encountered processing metrics stream from docker, this may affect cloudwatch metric accuracy: %s", err)
			returnError = true
		case rawStat, ok := <-dockerStats:
			if !ok {
				if returnError {
					return fmt.Errorf("error encountered processing metrics stream from docker")
				}
				return nil
			}
			if err := container.statsQueue.Add(rawStat); err != nil {
				seelog.Warnf("Error converting stats for container %s: %v", dockerID, err)
			}
		}
	}
}

func (container *StatsContainer) terminal() (bool, error) {
	dockerContainer, err := container.resolver.ResolveContainer(container.containerMetadata.DockerID)
	if err != nil {
		return false, err
	}
	return dockerContainer.Container.KnownTerminal(), nil
}
