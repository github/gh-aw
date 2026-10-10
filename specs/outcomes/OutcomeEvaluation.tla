------------------------- MODULE OutcomeEvaluation -------------------------
EXTENDS Naturals, FiniteSets, TLC

CONSTANTS Observations, Expectations, Bug
VARIABLES observation, phase, goResult, jsResult, exported, summary

Statuses == {"accepted", "rejected", "pending", "ignored", "unknown",
             "error", "lifecycle", "lifecycle_close", "skipped"}
Strengths == {"strong", "medium", "weak", "none"}
Engines == {"go", "js"}
Empty == [status |-> "unknown", strength |-> "none",
          signal |-> "uncomputed", zeroTouch |-> FALSE]

Result(status, strength, signal) ==
    [status |-> status, strength |-> strength, signal |-> signal,
     zeroTouch |-> FALSE]

Base(kind) ==
    [kind |-> kind, reference |-> TRUE, primary |-> "ok", state |-> "open",
     actor |-> "human", execution |-> "present", mutation |-> "retained",
     engaged |-> FALSE, reactions |-> FALSE,
     effortKnown |-> TRUE, humanActivity |-> FALSE,
     timestampKnown |-> TRUE, afterAction |-> TRUE, supplementary |-> "ok"]

PRFacts ==
    {[Base("pr") EXCEPT !.state = state, !.primary = primary,
        !.reference = reference, !.effortKnown = effort,
        !.humanActivity = activity, !.timestampKnown = timestamp] :
        state \in {"open", "closed", "merged"},
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN,
        effort \in BOOLEAN, activity \in BOOLEAN, timestamp \in BOOLEAN}
IssueFacts ==
    {[Base("issue") EXCEPT !.state = state, !.primary = primary,
        !.reference = reference, !.actor = actor, !.engaged = engaged,
        !.supplementary = supplementary] :
        state \in {"open", "completed", "not_planned", "closed"},
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN,
        actor \in {"human", "bot", "unknown"}, engaged \in BOOLEAN,
        supplementary \in {"ok", "error"}}
CommentFacts ==
    {[Base("comment") EXCEPT !.primary = primary, !.reference = reference,
        !.engaged = engaged, !.reactions = reactions,
        !.actor = actor, !.supplementary = supplementary] :
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN,
        engaged \in BOOLEAN, reactions \in BOOLEAN,
        actor \in {"human", "bot", "unknown"},
        supplementary \in {"ok", "error"}}
CloseFacts ==
    {[Base("close") EXCEPT !.state = state, !.primary = primary,
        !.reference = reference, !.actor = actor] :
        state \in {"open", "closed", "merged"},
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN,
        actor \in {"human", "bot", "unknown"}}
MutationFacts ==
    {[Base(kind) EXCEPT !.primary = primary, !.reference = reference,
        !.execution = execution, !.mutation = mutation] :
        kind \in {"labels", "replace"}, primary \in {"ok", "404", "error"},
        reference \in BOOLEAN, execution \in {"present", "missing", "empty"},
        mutation \in {"retained", "reverted", "replaced"}}
PushFacts ==
    {[Base("push") EXCEPT !.state = state, !.primary = primary,
        !.reference = reference, !.execution = execution, !.mutation = mutation] :
        state \in {"open", "closed", "merged"},
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN,
        execution \in {"present", "missing"},
        mutation \in {"retained", "unverified", "api_error"}}
DispatchFacts ==
    {[Base("dispatch") EXCEPT !.state = state, !.primary = primary,
        !.execution = execution, !.reference = reference] :
        state \in {"success", "failure", "pending", "no_effect"},
        primary \in {"ok", "error"}, execution \in {"present", "missing"},
        reference \in BOOLEAN}
OtherFacts ==
    {[Base(kind) EXCEPT !.primary = primary, !.reference = reference] :
        kind \in {"generic", "unsupported", "skipped"},
        primary \in {"ok", "404", "error"}, reference \in BOOLEAN}
UpdateFacts ==
    {[Base("update") EXCEPT !.state = state, !.primary = primary,
        !.reference = reference, !.execution = execution, !.mutation = mutation] :
        state \in {"open", "merged"}, primary \in {"ok", "error"},
        reference \in BOOLEAN, execution \in {"present", "missing", "empty"},
        mutation \in {"retained", "reverted", "replaced"}}
MilestoneFacts ==
    {[Base("milestone") EXCEPT !.execution = execution, !.mutation = mutation,
        !.primary = primary, !.reference = reference] :
        execution \in {"present", "missing"}, mutation \in {"retained", "replaced"},
        primary \in {"ok", "error"}, reference \in BOOLEAN}
ActorTimeFacts ==
    {[Base(kind) EXCEPT !.state = state, !.execution = execution,
        !.actor = actor, !.afterAction = afterAction, !.engaged = engaged,
        !.primary = primary, !.reference = reference] :
        kind \in {"ready", "assignment"}, state \in {"open", "closed", "merged"},
        execution \in {"present", "missing"}, actor \in {"human", "bot", "agent", "unknown"},
        afterAction \in BOOLEAN, engaged \in BOOLEAN,
        primary \in {"ok", "error"}, reference \in BOOLEAN}
ReviewFacts ==
    {[Base(kind) EXCEPT !.state = state, !.execution = execution,
        !.mutation = mutation, !.engaged = engaged, !.primary = primary,
        !.reference = reference] :
        kind \in {"requested", "submitted"},
        state \in {"approved", "submitted", "dismissed", "changes", "absent"},
        execution \in {"present", "team", "missing"},
        mutation \in {"open", "closed", "merged", "pending"},
        engaged \in BOOLEAN, primary \in {"ok", "error"}, reference \in BOOLEAN}
BoundedObservations ==
    PRFacts \cup IssueFacts \cup CommentFacts \cup CloseFacts \cup
    MutationFacts \cup PushFacts \cup DispatchFacts \cup OtherFacts \cup
    UpdateFacts \cup MilestoneFacts \cup ActorTimeFacts \cup ReviewFacts
NoExpectations == {}

\* Engagement is already actor/time-filtered. Reactions have no actor identity
\* and are medium evidence. The fixture projection checks that filtering.
Classify(o) ==
    CASE o.kind = "skipped" -> Result("skipped", "none", "skipped")
      [] o.kind = "unsupported" ->
            Result("unknown", "none", "unsupported_evaluator")
      [] ~o.reference ->
            Result("unknown", "none",
                IF o.kind \in {"replace", "update"} THEN "missing_execution_state"
                ELSE "missing_reference")
      [] o.kind = "dispatch" /\ o.execution = "missing" ->
            Result("pending", "none", "missing_run_id")
      [] o.kind \in {"labels", "replace"} /\ o.execution # "present" ->
            Result("unknown", "none",
                IF o.execution = "empty" THEN "no_state_delta"
                ELSE "missing_execution_state")
      [] o.kind \in {"update", "milestone"} /\ o.execution = "missing" ->
            Result("unknown", "none", "missing_execution_state")
      [] o.primary # "ok" ->
            IF o.primary = "404" /\ o.kind \in {"pr", "issue", "comment"}
            THEN Result("rejected", "strong", "deleted")
            ELSE Result("error", "weak", "evaluation_error")
      [] o.kind = "pr" ->
            CASE o.state = "merged" ->
                [Result("accepted", "strong", "merged") EXCEPT
                    !.zeroTouch = o.effortKnown /\ o.timestampKnown /\
                                  ~o.humanActivity]
              [] o.state = "closed" ->
                    Result("rejected", "strong", "closed_without_merge")
              [] o.state = "open" -> Result("pending", "medium", "open")
              [] OTHER -> Result("unknown", "weak", "unknown")
      [] o.kind = "issue" ->
            CASE o.state = "completed" -> Result("accepted", "strong", "completed")
              [] o.state = "not_planned" /\ o.actor = "unknown" ->
                    Result("error", "weak", "evaluation_error")
              [] o.state = "not_planned" ->
                    IF o.actor = "bot" THEN Result("lifecycle", "medium", "lifecycle")
                    ELSE Result("rejected", "strong", "closed_not_planned")
              [] o.state = "open" /\ o.supplementary # "ok" ->
                    Result("error", "weak", "evaluation_error")
              [] o.state = "open" ->
                    Result("pending", "medium", IF o.engaged THEN "acted_on" ELSE "open")
              [] OTHER -> Result("unknown", "weak", "unknown")
      [] o.kind = "comment" ->
            IF o.supplementary # "ok" /\ ~o.reactions
            THEN Result("error", "weak", "evaluation_error")
            ELSE IF o.engaged \/ o.reactions THEN Result("accepted", "medium", "acted_on")
            ELSE Result("pending", "medium", "pending")
      [] o.kind = "close" ->
            CASE o.state = "open" -> Result("rejected", "strong", "reopened")
              [] o.state = "merged" -> Result("rejected", "strong", "closed_by_merge")
              [] o.actor = "unknown" -> Result("error", "weak", "evaluation_error")
              [] o.actor = "bot" -> Result("lifecycle_close", "medium", "lifecycle_close")
              [] OTHER -> Result("accepted", "strong", "closed")
      [] o.kind \in {"labels", "replace"} ->
            CASE o.mutation = "retained" -> Result("accepted", "medium", "state_retained")
              [] o.kind = "replace" /\ o.mutation = "reverted" ->
                    Result("rejected", "strong", "state_reverted")
              [] OTHER -> Result("rejected", "strong", "state_replaced")
      [] o.kind = "update" ->
            CASE o.execution = "empty" -> Result("unknown", "none", "no_state_delta")
              [] o.mutation = "retained" /\ o.state = "merged" ->
                    Result("accepted", "strong", "state_retained_and_merged")
              [] o.mutation = "retained" -> Result("accepted", "medium", "state_retained")
              [] o.mutation = "reverted" -> Result("rejected", "strong", "state_reverted")
              [] OTHER -> Result("rejected", "strong", "state_replaced")
      [] o.kind = "milestone" ->
            IF o.mutation = "retained" THEN Result("accepted", "medium", "milestone_assigned")
            ELSE Result("rejected", "medium", "milestone_removed")
      [] o.kind = "ready" ->
            CASE o.engaged /\ o.actor = "human" /\ o.afterAction ->
                    Result("accepted", "medium", "reviewed")
              [] o.state = "open" -> Result("pending", "medium", "awaiting_review")
              [] OTHER -> Result("ignored", "medium", "ignored")
      [] o.kind = "assignment" ->
            CASE o.execution # "present" \/ o.actor # "agent" \/ ~o.afterAction ->
                    Result("unknown", "none", "missing_execution_state")
              [] o.state = "merged" -> Result("accepted", "strong", "merged")
              [] o.state = "closed" -> Result("rejected", "strong", "closed_without_merge")
              [] OTHER -> Result("pending", "medium", "open")
      [] o.kind = "requested" ->
            CASE o.execution = "present" /\ o.state = "approved" ->
                    Result("accepted", "strong", "review_approved")
              [] o.execution = "present" /\ o.state # "absent" ->
                    Result("accepted", "medium", "review_submitted")
              [] o.execution = "team" /\ o.engaged ->
                    Result("unknown", "weak", "team_membership_unverified")
              [] o.mutation = "pending" -> Result("pending", "medium", "awaiting_review")
              [] o.execution # "missing" -> Result("rejected", "strong", "review_request_removed")
              [] OTHER -> Result("unknown", "weak", "unknown")
      [] o.kind = "submitted" ->
            CASE o.execution # "present" \/ o.state = "absent" ->
                    Result("unknown", "weak", "review_missing")
              [] o.state = "dismissed" -> Result("rejected", "strong", "review_dismissed")
              [] o.mutation = "merged" /\ o.state = "approved" ->
                    Result("accepted", "strong", "review_approved")
              [] o.mutation = "merged" /\ o.state = "changes" /\ o.engaged ->
                    Result("accepted", "medium", "changes_requested_addressed")
              [] o.mutation = "closed" -> Result("rejected", "medium", "closed_without_merge_after_review")
              [] o.mutation = "open" /\ o.engaged ->
                    Result("pending", "medium", "latest_review_pending")
              [] OTHER -> Result("unknown", "weak", "unknown")
      [] o.kind = "push" ->
            CASE o.state = "open" -> Result("pending", "medium", "open")
              [] o.state = "closed" -> Result("rejected", "strong", "closed_without_merge")
              [] o.execution = "missing" -> Result("unknown", "none", "missing_execution_state")
              [] o.mutation = "api_error" -> Result("error", "weak", "evaluation_error")
              [] o.mutation = "retained" -> Result("accepted", "strong", "merged")
              [] OTHER -> Result("unknown", "weak", "commit_retention_unknown")
      [] o.kind = "dispatch" ->
            CASE o.state = "success" -> Result("accepted", "strong", "workflow_success")
              [] o.state = "failure" -> Result("rejected", "strong", "workflow_failed")
              [] o.state = "pending" -> Result("pending", "medium", "workflow_pending")
              [] OTHER -> Result("ignored", "medium", "workflow_no_effect")
      [] OTHER -> Result("unknown", "weak", "target_exists_only")

\* Each negative control changes one rule in the JavaScript projection.
Evaluate(o, engine) ==
    LET correct == Classify(o) IN
    IF engine = "go" THEN correct
    ELSE CASE Bug = "existence" /\ o.kind = "generic" /\ o.reference /\
                    o.primary = "ok" /\ correct.status = "unknown" ->
                    Result("accepted", "weak", "target_exists_only")
           [] Bug = "bot" /\ o.kind = "issue" /\ o.state = "open" /\
                    o.actor = "bot" /\ o.engaged /\
                    o.reference /\ o.primary = "ok" /\ o.supplementary = "ok" ->
                    Result("accepted", "medium", "acted_on")
           [] Bug = "execution" /\ o.kind = "push" /\ o.state = "merged" /\
                    o.reference /\ o.primary = "ok" /\ o.execution = "missing" ->
                    Result("accepted", "strong", "merged")
           [] Bug = "deletion" /\ o.kind = "issue" /\ o.state = "open" /\
                    o.reference /\ o.primary = "ok" /\ o.supplementary # "ok" ->
                    Result("rejected", "strong", "deleted")
           [] Bug = "zero_touch" /\ correct.status = "accepted" /\ o.kind = "pr" ->
                    [correct EXCEPT !.zeroTouch = ~o.humanActivity]
           [] Bug = "review" /\ o.kind = "submitted" /\ o.execution = "missing" /\
                    o.reference /\ o.primary = "ok" ->
                    Result("accepted", "strong", "review_approved")
           [] Bug = "team" /\ o.kind = "requested" /\ o.execution = "team" /\
                    o.reference /\ o.primary = "ok" ->
                    Result("accepted", "medium", "review_submitted")
           [] Bug = "time" /\ o.kind = "ready" /\ o.engaged /\ o.actor = "human" /\
                    o.reference /\ o.primary = "ok" ->
                    Result("accepted", "medium", "reviewed")
           [] OTHER -> correct

Counts(results) ==
    [s \in Statuses |-> Cardinality({e \in Engines : results[e].status = s})]
ZeroCounts == [s \in Statuses |-> 0]
vars == <<observation, phase, goResult, jsResult, exported, summary>>
Init ==
    /\ observation \in Observations
    /\ phase = "observe"
    /\ goResult = Empty /\ jsResult = Empty
    /\ exported = [e \in Engines |-> Empty]
    /\ summary = ZeroCounts
ComputeGo ==
    /\ phase \in {"observe", "js"}
    /\ goResult' = Evaluate(observation, "go")
    /\ phase' = IF phase = "observe" THEN "go" ELSE "computed"
    /\ UNCHANGED <<observation, jsResult, exported, summary>>
ComputeJS ==
    /\ phase \in {"observe", "go"}
    /\ jsResult' = Evaluate(observation, "js")
    /\ phase' = IF phase = "observe" THEN "js" ELSE "computed"
    /\ UNCHANGED <<observation, goResult, exported, summary>>
Publish ==
    /\ phase = "computed"
    /\ exported' =
        [e \in Engines |->
            IF e = "go" THEN goResult
            ELSE IF Bug = "normalization" /\ jsResult.signal = "target_exists_only"
                 THEN Result("accepted", "weak", "target_exists_only")
                 ELSE jsResult]
    /\ phase' = "published"
    /\ UNCHANGED <<observation, goResult, jsResult, summary>>
Aggregate ==
    /\ phase = "published"
    /\ summary' = IF Bug = "summary" THEN [Counts(exported) EXCEPT !["unknown"] = 0]
                  ELSE Counts(exported)
    /\ phase' = "done"
    /\ UNCHANGED <<observation, goResult, jsResult, exported>>
Next == ComputeGo \/ ComputeJS \/ Publish \/ Aggregate
Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

Computed == phase \in {"computed", "published", "done"}
TypeOK ==
    /\ phase \in {"observe", "go", "js", "computed", "published", "done"}
    /\ \A r \in {goResult, jsResult} :
        r.status \in Statuses /\ r.strength \in Strengths /\ r.zeroTouch \in BOOLEAN
    /\ \A e \in Engines :
        exported[e].status \in Statuses /\ exported[e].strength \in Strengths /\
        exported[e].zeroTouch \in BOOLEAN
    /\ \A s \in Statuses : summary[s] \in 0..2
PrimaryFailureCannotAccept ==
    Computed /\ observation.primary # "ok" =>
        \A r \in {goResult, jsResult} : r.status # "accepted"
AcceptanceEvidence ==
    Computed => \A r \in {goResult, jsResult} :
        r.status = "accepted" => Classify(observation).status = "accepted"
ExecutionAttributionRequired ==
    Computed /\ observation.kind \in {"update", "milestone", "push", "assignment",
                                     "requested", "submitted", "dispatch"} =>
        \A r \in {goResult, jsResult} : r.status = "accepted" =>
            observation.execution = "present"
ReviewRequiresPostActionHuman ==
    Computed /\ observation.kind = "ready" =>
        \A r \in {goResult, jsResult} : r.status = "accepted" =>
            observation.engaged /\ observation.actor = "human" /\ observation.afterAction
OpenCreationsPending ==
    Computed /\ observation.kind \in {"pr", "issue"} /\
    observation.state = "open" /\ observation.reference /\
    observation.primary = "ok" /\ observation.supplementary = "ok"
    => goResult.status = "pending" /\ jsResult.status = "pending"
DeletionRequiresPrimary404 ==
    Computed => \A r \in {goResult, jsResult} :
        r.signal = "deleted" => observation.primary = "404"
ZeroTouchRequiresCompleteEvidence ==
    Computed => \A r \in {goResult, jsResult} :
        r.zeroTouch => observation.kind = "pr" /\ r.status = "accepted" /\
            observation.effortKnown /\ observation.timestampKnown /\ ~observation.humanActivity
RuntimeParity == Computed => goResult = jsResult
TypedExportPreserved ==
    phase \in {"published", "done"} =>
        exported["go"] = goResult /\ exported["js"] = jsResult
SummaryReconciles ==
    phase = "done" =>
        summary = Counts(exported) /\
        Cardinality({e \in Engines : exported[e].status \in Statuses}) = 2
FixtureAgreement ==
    \A pair \in Expectations : Classify(pair.observation) = pair.result
EventuallyPublished == <> (phase = "done")
=============================================================================
