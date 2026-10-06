package workqueue

import (
	"encoding/json"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var assignmentSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	names := []string{"WorkQueueAssignment", "WorkQueueFinishIntent"}
	for _, name := range names {
		data, err := schemas.ReadFile("schema/" + name + ".json")
		if err != nil {
			return nil, err
		}
		var resource any
		if err := json.Unmarshal(data, &resource); err != nil {
			return nil, err
		}
		if err := compiler.AddResource(name+".json", resource); err != nil {
			return nil, err
		}
	}
	result := map[string]*jsonschema.Schema{}
	for _, name := range names {
		schema, err := compiler.Compile(name + ".json")
		if err != nil {
			return nil, err
		}
		result[name] = schema
	}
	return result, nil
})

func validateAssignmentJSON(name string, data []byte) error {
	canonical, err := Canonical(data)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(canonical, &value); err != nil {
		return err
	}
	compiled, err := assignmentSchemas()
	if err != nil {
		return err
	}
	if err := compiled[name].Validate(value); err != nil {
		return queueError("assignment_invalid", "invalid %s: %v", name, err)
	}
	return validateContractIdentityBytes(name, value)
}

func ParseAssignment(data []byte) (Assignment, error) {
	if len(data) > 48<<10 {
		return Assignment{}, queueError("assignment_limit", "assignment exceeds the engineering byte ceiling")
	}
	if err := validateAssignmentJSON("WorkQueueAssignment", data); err != nil {
		return Assignment{}, err
	}
	var assignment Assignment
	if err := json.Unmarshal(data, &assignment); err != nil {
		return Assignment{}, err
	}
	handles, claims, works := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, member := range assignment.Claims {
		if handles[member.Handle] || claims[member.ClaimID] || works[member.WorkID] {
			return Assignment{}, queueError("assignment_invalid", "assignment repeats a handle, Claim, or Work")
		}
		handles[member.Handle], claims[member.ClaimID], works[member.WorkID] = true, true, true
	}
	return assignment, nil
}

// NormalizeFinishIntent preserves absent versus null through schema validation.
// The result still needs durable assignment/run/ownership verification.
func NormalizeFinishIntent(assignment Assignment, data []byte) (FinishParameters, error) {
	if len(data) > 1024 {
		return FinishParameters{}, queueError("claim_scope_invalid", "finish intent exceeds its bounded profile")
	}
	if err := validateAssignmentJSON("WorkQueueFinishIntent", data); err != nil {
		return FinishParameters{}, err
	}
	var intent struct {
		ClaimHandle *string `json:"claim_handle"`
		Outcome     string  `json:"outcome"`
	}
	_ = json.Unmarshal(data, &intent)
	handle, err := NormalizeClaimHandle(assignment, intent.ClaimHandle)
	if err != nil {
		return FinishParameters{}, err
	}
	return FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: handle, Outcome: intent.Outcome}, nil
}
