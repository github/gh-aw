package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

func workDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "deploy", Short: "Refresh existing AW worker deployments without changing scheduling policy or draining Work",
		Args: cobra.NoArgs,
		RunE: workRunDeployCommand,
	}
	cmd.Flags().Bool("from-config", false, "Resolve protected AW worker sources and compiled contracts at the target repository's immutable default ref")
	cmd.Flags().String("pool", "", "Limit deployment updates to this existing scheduling pool")
	cmd.Flags().String("worker-profile", "", "Limit deployment updates to this existing worker profile")
	return cmd
}

func workRunDeployCommand(cmd *cobra.Command, _ []string) error {
	fromConfig, _ := cmd.Flags().GetBool("from-config")
	if !fromConfig {
		return errors.New("deployment_invalid: --from-config is required. Use gh aw work deploy --from-config to resolve protected default-ref AW sources and compiler contract stamps")
	}
	branch := workBranch(cmd)
	actor, err := branch.Authenticate(cmd.Context(), "administrator")
	if err != nil {
		return err
	}
	state, err := workRead(cmd)
	if err != nil {
		return err
	}
	requestID, err := workRequestID(cmd)
	if err != nil {
		return err
	}
	pool, _ := cmd.Flags().GetString("pool")
	worker, _ := cmd.Flags().GetString("worker-profile")
	if existing, ok := state.Requests[requestID]; ok {
		return workRecoverDeployment(cmd, branch, actor, existing, pool, worker)
	}
	operations, err := branch.DeploymentOperationsFromConfig(cmd.Context(), state, pool, worker)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return workPrint(cmd, workqueue.Publication{Changed: false}, "Selected worker deployments are already current")
	}
	request, err := workqueue.NewRequest(requestID, "deployment", actor, workqueue.OperationsParameters{Operations: operations})
	if err != nil {
		return err
	}
	published, err := branch.Publish(cmd.Context(), actor, request)
	if err != nil {
		return err
	}
	return workPrint(cmd, published, fmt.Sprintf("Updated %d worker deployments without changing scheduling policy", len(operations)))
}

func workRecoverDeployment(cmd *cobra.Command, branch workqueue.Branch, actor workqueue.Actor, existing workqueue.QueueCommit, pool, worker string) error {
	if existing.Request.Kind != "deployment" || existing.Actor != actor {
		return errors.New("request_reuse: request identity belongs to another command or authenticated actor. Use a new --request-id for a different deployment")
	}
	var parameters workqueue.OperationsParameters
	if err := json.Unmarshal(existing.Request.Parameters, &parameters); err != nil {
		return err
	}
	for _, operation := range parameters.Operations {
		var deployment workqueue.DeploymentOperation
		if err := json.Unmarshal(operation, &deployment); err != nil {
			return err
		}
		if pool != "" && deployment.Pool != pool || worker != "" && deployment.WorkerProfile != worker {
			return errors.New("request_reuse: deployment selectors differ from the committed request. Use the original selectors or a new --request-id")
		}
	}
	published, err := branch.Publish(cmd.Context(), actor, existing.Request)
	if err != nil {
		return err
	}
	return workPrint(cmd, published, "Deployment request already committed")
}
