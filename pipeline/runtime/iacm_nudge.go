package runtime

import (
	"errors"

	"github.com/harness/lite-engine/logstream"
)

func getIACMNudges() []logstream.Nudge { //nolint:funlen
	return []logstream.Nudge{
		logstream.NewNudge("[ERROR] approval step has been rejected",
			"[IACM]: The approval step was rejected",
			errors.New("the approval step has been rejected")),
		logstream.NewNudge("[ERROR] Error parsing the init_option block",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error parsing the fmt_option block",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error processing environment variable",
			"[IACM]: Check the environment variables attached to the workspace.",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error setting environment variable",
			"[IACM]: Check the environment variables attached to the workspace",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error processing Terraform variable",
			"[IACM]: Check the terraform variables attached to the workspace",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error determining the current working dir",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error configuring safe directory in git",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] error finding Terraform binary",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error creating the tfexec client for version",
			"[IACM]: Try changing the version of terraform in the workspace",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] error finding Terraform binary",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error parsing the endpoint_data block",
			"[IACM]: Contact Harness Support",
			errors.New("there was an error starting the plugin")),
		logstream.NewNudge("[ERROR] Error during the execution",
			"[IACM]: Check the logs for more information",
			errors.New("error during the execution")),
		logstream.NewNudge("[ERROR] error sending execution event",
			"[IACM]: Contact Harness Support",
			errors.New("error during the communication with the IACM server")),
		logstream.NewNudge("[ERROR] Error initializing Terraform",
			"[IACM]: Error Initializing terraform. Please check the logs.",
			errors.New("error during the init step")),
		logstream.NewNudge("[ERROR] invalid Terraform plugin operation",
			"[IACM]: Supported operations are init, validate, plan, plan-destroy, apply and destroy",
			errors.New("error during the execution of the step. Invalid operation")),
		logstream.NewNudge("[ERROR] error during the apply phase",
			"[IACM]: Error during the apply step. Please check the logs.",
			errors.New("error during the apply step")),
		logstream.NewNudge("[ERROR] error during the destroy phase",
			"[IACM]: Error during the apply step. Please check the logs.",
			errors.New("error during the destroy step")),
		logstream.NewNudge("[ERROR] approval id is empty for the execution",
			"[IACM]: Please contact Harness Support",
			errors.New("error during the approval step")),
		logstream.NewNudge("[ERROR] Error executing Terraform plan",
			"[IACM]: Error generating terraform plan. Please check the logs.",
			errors.New("error during the plan step")),
		logstream.NewNudge("[ERROR] no plan available",
			"[IACM]: No terraform plan was found. Is the init step missing?.",
			errors.New("error during the plan step")),
		logstream.NewNudge("[ERROR] Error converting the Terraform plan to JSON",
			"[IACM]: The generated plan can't be converted to JSON. Check the tf files",
			errors.New("error during the plan step")),
		logstream.NewNudge("[ERROR] no parsed plan available",
			"[IACM]: No terraform plan was found. Is the init step missing?.",
			errors.New("error during the plan step")),
		logstream.NewNudge("[ERROR] Error executing Terraform apply",
			"[IACM]: Error running terraform apply. Please check the logs.",
			errors.New("error during the apply step")),
		logstream.NewNudge("[ERROR] Error executing Terraform destroy",
			"[IACM]: Error running terraform destroy. Please check the logs. ",
			errors.New("error during the destroy step")),
		logstream.NewNudge("[ERROR] no state file available",
			"[IACM]: No terraform state was found. Is the apply step missing?.",
			errors.New("error during the parsing step")),
		logstream.NewNudge("[ERROR] Error converting the Terraform state to JSON",
			"[IACM]: The generated state can't be converted to JSON. Check the tf files",
			errors.New("error during the parsing step")),
		logstream.NewNudge("[ERROR] no parsed state file available",
			"[IACM]: No terraform state was found. Is the apply step missing?.",
			errors.New("error during the parsing step")),
		logstream.NewNudge("[ERROR] Error exporting output results",
			"[IACM]: There was an error parsing the outputs. Please check the logs.",
			errors.New("error generating output values")),
		logstream.NewNudge("[ERROR] error creating the gcloud credentials file",
			"[IACM]: Please check the logs.",
			errors.New("error generating gcp credentials json file")),
		logstream.NewNudge("[ERROR] Error closing GCP credentials file",
			"[IACM]: Please check the logs.",
			errors.New("error generating gcp credentials json file")),
		logstream.NewNudge("[ERROR] error exporting the GCP credentials",
			"[IACM]: Please check the logs.",
			errors.New("error generating gcp credentials json file")),
		logstream.NewNudge("[ERROR] error creating sts creds with web identity token file",
			"[IACM]: Check if the serviceAccount has the proper IAM role attached to it. Please check the logs",
			errors.New("error generating AWS credentials")),
		logstream.NewNudge("[ERROR] error creating sts creds by assuming the role",
			"[IACM]: Check if the IAM role used to assume the rol has the proper policies attached to it (assume role). Please check the logs.",
			errors.New("error generating AWS credentials")),
		logstream.NewNudge("[ERROR] error creating the git audit data",
			"[IACM]: Please contact Harness Support",
			errors.New("error generating git audit data")),
		logstream.NewNudge("[ERROR] Error uploading Terraform data",
			"[IACM]: The communication with the IACM Server seems to be down. Please retry again.",
			errors.New("error sending terraform data to the IACM Server")),
		logstream.NewNudge("[ERROR] error while sending the event with the data",
			"[IACM]: The communication with the IACM Server seems to be down. Please retry",
			errors.New("error sending terraform data to the IACM Server")),
		logstream.NewNudge("[ERROR] Error downloading Terraform state from Harness",
			"[IACM]: The communication with the IACM Server seems to be down. Please retry again.",
			errors.New("error sending terraform data to the IACM Server")),
		logstream.NewNudge("[ERROR] Error downloading state from the Terraform remote backend",
			"[IACM]: The communication with the terraform backend seems to be down. Check that the backend configuration is working.",
			errors.New("error sending terraform data to the terraform backend")),
		logstream.NewNudge("[ERROR] error trying to retrieve the state file from the remote",
			"[IACM]: The communication with the terraform backend seems to be down. Check that the backend configuration is working.",
			errors.New("error retrieving terraform data to the terraform backend")),
		logstream.NewNudge("[ERROR] error trying to save the state file",
			"[IACM]: Please, contact Harness Support",
			errors.New("error saving the state file in local")),
		logstream.NewNudge("[ERROR] error trying to write the state file",
			"[IACM]: Please, contact Harness Support",
			errors.New("error saving the state file in local")),
		logstream.NewNudge("[ERROR] error flushing to the state file",
			"[IACM]: Please, contact Harness Support",
			errors.New("error saving the state file in local")),
	}
}
