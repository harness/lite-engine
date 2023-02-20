// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package ti

const (
	// AccountIDEnv represents the environment variable for Harness Account ID of the pipeline execution
	AccountIDEnv = "HARNESS_ACCOUNT_ID"

	// OrgIDEnv represents the environment variable for Organization ID of the pipeline execution
	OrgIDEnv = "HARNESS_ORG_ID"

	// ProjectIDEnv represents the environment variable for Project ID of the pipeline execution
	ProjectIDEnv = "HARNESS_PROJECT_ID"

	// PipelineIDEnv represents the environment variable for Pipeline ID of the pipeline execution
	PipelineIDEnv = "HARNESS_PIPELINE_ID"

	// TiSvcEp represents the environment variable for TI service endpoint
	TiSvcEp = "HARNESS_TI_SERVICE_ENDPOINT"

	// TiSvcToken represents the environment variable for TI service token
	TiSvcToken = "HARNESS_TI_SERVICE_TOKEN" //nolint:gosec

	// InfraEnv represents the environment variable for infra on which the pipeline is running
	InfraEnv = "HARNESS_INFRA"

	// HarnessInfra represents the environment in which the build is running
	HarnessInfra = "VM"
)
