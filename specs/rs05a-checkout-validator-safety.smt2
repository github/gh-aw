; RS-05a checkout validator safety model.
;
; This SMT-LIB program models the security-relevant control-flow in
; actions/setup/js/checkout_pr_branch.cjs:
; - workflow_dispatch aw_context parsing and repository scoping
; - assertTrustedCheckoutRuntime fork and actor trust checks
; - refs/pull/N/head isolation for workflow_dispatch PR replay
;
; Every safety query is phrased as "does there exist an unsafe checkout?"
; and is expected to be unsatisfiable. The final query is a positive
; risk-matrix sanity check proving that non-workflow_dispatch PR triggers
; are not rejected solely because the runtime repository is structurally a
; fork.

(set-logic ALL)

(declare-datatypes
  ()
  ((Event
     pull_request
     pull_request_target
     pull_request_review
     pull_request_review_comment
     issue_comment
     workflow_dispatch
     fork_event
     other_event)
   (Permission
     perm_none
     perm_read
     perm_triage
     perm_write
     perm_maintain
     perm_admin)
   (RepoFork
     repo_fork_unknown
     repo_fork_true
     repo_fork_false)))

(declare-const event Event)
(declare-const has_payload_pr Bool)
(declare-const has_issue_pr Bool)
(declare-const aw_context_present Bool)
(declare-const aw_context_json_valid Bool)
(declare-const aw_item_type_pull_request Bool)
(declare-const aw_item_number_canonical Bool)
(declare-const aw_repo_present Bool)
(declare-const aw_repo_matches_current Bool)
(declare-const repository_fork RepoFork)
(declare-const actor_present Bool)
(declare-const actor_is_centralized_router Bool)
(declare-const sender_type_bot Bool)
(declare-const centralized_marker_present Bool)
(declare-const propagated_actor_present Bool)
(declare-const propagated_actor_is_router Bool)
(declare-const actor_permission_verifiable Bool)
(declare-const effective_actor_permission Permission)
(declare-const pull_request_same_repo Bool)
(declare-const api_pr_details_available Bool)

; GitHub event-shape invariants. These keep counterexample searches scoped to
; payload combinations that can actually reach checkout_pr_branch.cjs.
(assert (=> has_issue_pr (= event issue_comment)))
(assert (=> has_payload_pr
            (or (= event pull_request)
                (= event pull_request_target)
                (= event pull_request_review)
                (= event pull_request_review_comment))))

(define-fun trusted_permission () Bool
  (or (= effective_actor_permission perm_write)
      (= effective_actor_permission perm_maintain)
      (= effective_actor_permission perm_admin)))

(define-fun workflow_dispatch_aw_pr_context () Bool
  (and (= event workflow_dispatch)
       (not has_payload_pr)
       aw_context_present
       aw_context_json_valid
       aw_item_type_pull_request
       aw_item_number_canonical
       (or (not aw_repo_present) aw_repo_matches_current)))

(define-fun pull_request_context () Bool
  (or has_payload_pr has_issue_pr workflow_dispatch_aw_pr_context))

(define-fun centralized_dispatch_actor_trusted () Bool
  (and (= event workflow_dispatch)
       actor_present
       actor_is_centralized_router
       sender_type_bot
       centralized_marker_present
       propagated_actor_present
       (not propagated_actor_is_router)
       actor_permission_verifiable
       trusted_permission))

(define-fun direct_actor_trusted () Bool
  (and actor_present
       (not actor_is_centralized_router)
       actor_permission_verifiable
       trusted_permission))

(define-fun actor_trusted () Bool
  (or centralized_dispatch_actor_trusted direct_actor_trusted))

(define-fun fork_runtime_trusted () Bool
  (or (not (= event workflow_dispatch))
      (= repository_fork repo_fork_false)))

(define-fun trusted_runtime () Bool
  (and fork_runtime_trusted actor_trusted))

(define-fun direct_pull_request_branch_checkout () Bool
  (and (= event pull_request)
       has_payload_pr
       pull_request_same_repo))

(define-fun refs_pull_checkout () Bool
  (and pull_request_context
       trusted_runtime
       (not direct_pull_request_branch_checkout)
       api_pr_details_available))

(define-fun checkout_executes () Bool
  (and pull_request_context
       trusted_runtime
       (or direct_pull_request_branch_checkout refs_pull_checkout)))

; EXPECT: workflow_dispatch_requires_verified_non_fork unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert (not (= repository_fork repo_fork_false)))
(echo "workflow_dispatch_requires_verified_non_fork")
(check-sat)
(pop)

; EXPECT: checkout_requires_write_or_higher_permission unsat
(push)
(assert checkout_executes)
(assert (not (and actor_permission_verifiable trusted_permission)))
(echo "checkout_requires_write_or_higher_permission")
(check-sat)
(pop)

; EXPECT: centralized_dispatch_requires_platform_bot_identity unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert actor_is_centralized_router)
(assert (not sender_type_bot))
(echo "centralized_dispatch_requires_platform_bot_identity")
(check-sat)
(pop)

; EXPECT: centralized_dispatch_requires_command_or_label_marker unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert actor_is_centralized_router)
(assert (not centralized_marker_present))
(echo "centralized_dispatch_requires_command_or_label_marker")
(check-sat)
(pop)

; EXPECT: centralized_dispatch_requires_originating_actor unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert actor_is_centralized_router)
(assert (not propagated_actor_present))
(echo "centralized_dispatch_requires_originating_actor")
(check-sat)
(pop)

; EXPECT: centralized_dispatch_rejects_router_self_propagation unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert actor_is_centralized_router)
(assert propagated_actor_is_router)
(echo "centralized_dispatch_rejects_router_self_propagation")
(check-sat)
(pop)

; EXPECT: workflow_dispatch_rejects_cross_repository_aw_context unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert (not has_payload_pr))
(assert aw_context_present)
(assert aw_context_json_valid)
(assert aw_item_type_pull_request)
(assert aw_item_number_canonical)
(assert aw_repo_present)
(assert (not aw_repo_matches_current))
(echo "workflow_dispatch_rejects_cross_repository_aw_context")
(check-sat)
(pop)

; EXPECT: workflow_dispatch_rejects_noncanonical_pr_number unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert (not has_payload_pr))
(assert aw_context_present)
(assert aw_context_json_valid)
(assert aw_item_type_pull_request)
(assert (not aw_item_number_canonical))
(echo "workflow_dispatch_rejects_noncanonical_pr_number")
(check-sat)
(pop)

; EXPECT: workflow_dispatch_uses_refs_pull_checkout unsat
(push)
(assert checkout_executes)
(assert (= event workflow_dispatch))
(assert (not refs_pull_checkout))
(echo "workflow_dispatch_uses_refs_pull_checkout")
(check-sat)
(pop)

; EXPECT: non_dispatch_pr_trigger_allows_forked_runtime_after_trust sat
(push)
(assert (= event pull_request_target))
(assert has_payload_pr)
(assert (= repository_fork repo_fork_true))
(assert actor_present)
(assert (not actor_is_centralized_router))
(assert actor_permission_verifiable)
(assert (= effective_actor_permission perm_write))
(assert api_pr_details_available)
(assert checkout_executes)
(assert refs_pull_checkout)
(echo "non_dispatch_pr_trigger_allows_forked_runtime_after_trust")
(check-sat)
(pop)
