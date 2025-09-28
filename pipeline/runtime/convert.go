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
		ID:             r.ID,
		Auth:           r.Auth,
		CPUPeriod:      r.CPUPeriod,
		CPUQuota:       r.CPUQuota,
		CPUShares:      r.CPUShares,
		CPUSet:         r.CPUSet,
		Detach:         r.Detach,
		Devices:        r.Devices,
		DNS:            r.DNS,
		DNSSearch:      r.DNSSearch,
		Envs:           r.Envs,
		ExtraHosts:     r.ExtraHosts,
		IgnoreStdout:   r.IgnoreStdout,
		IgnoreStderr:   r.IgnoreStderr,
		Image:          r.Image,
		Labels:         r.Labels,
		MemSwapLimit:   r.MemSwapLimit,
		MemLimit:       r.MemLimit,
		Name:           r.Name,
		Network:        r.Network,
		Networks:       r.Networks,
		PortBindings:   r.PortBindings,
		Privileged:     r.Privileged,
		Pull:           r.Pull,
		ShmSize:        r.ShmSize,
		User:           r.User,
		Volumes:        r.Volumes,
		WorkingDir:     r.WorkingDir,
		Files:          r.Files,
		SoftStop:       r.SoftStop,
		NetworkAliases: r.NetworkAliases,
		ProcessConfig:  r.ProcessConfig,
		Secrets:        convertRequestSecretsToStepSecrets(r),
	}
}

// convertRequestSecretsToStepSecrets converts runtime secrets from StartStepRequest to spec.Secret objects
func convertRequestSecretsToStepSecrets(r *api.StartStepRequest) []*spec.Secret {
	var stepSecrets []*spec.Secret
	for _, secretValue := range r.Secrets {
		if secretValue != "" {
			stepSecrets = append(stepSecrets, &spec.Secret{
				Name: "runtime-secret",
				Data: []byte(secretValue),
				Mask: true,
			})
		}
	}
	return stepSecrets
}
