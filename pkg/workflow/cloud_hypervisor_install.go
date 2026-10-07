// This file generates GitHub Actions steps required to prepare AWF's preview
// cloud-hypervisor microVM runtime for primary agents and enclaves.

package workflow

import "github.com/github/gh-aw/pkg/logger"

var cloudHypervisorInstallLog = logger.New("workflow:cloud_hypervisor_install")

func generateCloudHypervisorKVMAccessStep() GitHubActionStep {
	cloudHypervisorInstallLog.Print("Generating cloud-hypervisor KVM access step")
	return GitHubActionStep([]string{
		"      - name: Grant runner access to KVM",
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/cloud_hypervisor_kvm_access.sh"`,
	})
}

func generateCloudHypervisorHostPreflightStep() GitHubActionStep {
	cloudHypervisorInstallLog.Print("Generating cloud-hypervisor host eligibility preflight step")
	return GitHubActionStep([]string{
		"      - name: Check host eligibility for cloud-hypervisor",
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/cloud_hypervisor_host_preflight.sh"`,
	})
}

func generateCloudHypervisorBundleSetupStep(awfVersion string) GitHubActionStep {
	cloudHypervisorInstallLog.Printf("Generating cloud-hypervisor bundle setup step for AWF version %q", awfVersion)
	return GitHubActionStep([]string{
		"      - name: Download and verify cloud-hypervisor bundle",
		"        id: cloud-hypervisor-bundle",
		"        env:",
		"          GH_AW_AWF_VERSION: " + awfVersion,
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/cloud_hypervisor_setup_bundle.sh"`,
	})
}

func generateCloudHypervisorEnclaveArtifactsStep() GitHubActionStep {
	return GitHubActionStep([]string{
		"      - name: Download and verify cloud-hypervisor enclave artifacts",
		"        env:",
		"          GH_TOKEN: ${{ github.token }}",
		"          GH_AW_AWF_VERSION: ${{ steps.cloud-hypervisor-bundle.outputs.release_tag }}",
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/cloud_hypervisor_setup_enclave_artifacts.sh"`,
	})
}

func appendCloudHypervisorSetupSteps(steps []GitHubActionStep, workflowData *WorkflowData) []GitHubActionStep {
	if !isCloudHypervisorRuntime(workflowData) && !hasCloudHypervisorEnclaves(workflowData) {
		return steps
	}

	steps = append(steps, generateCloudHypervisorKVMAccessStep())
	steps = append(steps, generateCloudHypervisorHostPreflightStep())
	steps = append(steps, generateCloudHypervisorBundleSetupStep(getAWFVersionForSetup(workflowData)))
	if hasCloudHypervisorEnclaves(workflowData) {
		steps = append(steps, generateCloudHypervisorEnclaveArtifactsStep())
	}
	return steps
}

func generateAWFInstallationStepForWorkflow(version string, workflowData *WorkflowData) GitHubActionStep {
	agentConfig := getAgentConfig(workflowData)
	if hasCloudHypervisorEnclaves(workflowData) && agentConfig != nil && agentConfig.Command == "" {
		// Enclave VMs require a privileged host AWF, installed on sudo's secure_path.
		// This does not change the primary agent's Docker runtime profile.
		return generateAWFInstallationStep(version, nil)
	}
	return generateAWFInstallationStep(version, agentConfig)
}
