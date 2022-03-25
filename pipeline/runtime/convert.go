// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
)

// Converts api params to engine.Step
func toStep(r *api.StartStepRequest) *spec.Step {
	return &spec.Step{
		ID:           r.ID,
		Auth:         r.Auth,
		CPUPeriod:    r.CPUPeriod,
		CPUQuota:     r.CPUQuota,
		CPUShares:    r.CPUShares,
		CPUSet:       r.CPUSet,
		Detach:       r.Detach,
		Devices:      r.Devices,
		DNS:          r.DNS,
		DNSSearch:    r.DNSSearch,
		Envs:         r.Envs,
		ExtraHosts:   r.ExtraHosts,
		IgnoreStdout: r.IgnoreStdout,
		IgnoreStderr: r.IgnoreStderr,
		Image:        r.Image,
		Labels:       r.Labels,
		MemSwapLimit: r.MemSwapLimit,
		MemLimit:     r.MemLimit,
		Name:         r.Name,
		Network:      r.Network,
		Networks:     r.Networks,
		PortBindings: r.PortBindings,
		Privileged:   r.Privileged,
		Pull:         r.Pull,
		ShmSize:      r.ShmSize,
		User:         r.User,
		Volumes:      r.Volumes,
		WorkingDir:   r.WorkingDir,
		Files:        r.Files,
	}
}
