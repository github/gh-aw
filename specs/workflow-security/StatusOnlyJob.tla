------------------------- MODULE StatusOnlyJob -------------------------
EXTENDS TLC

CONSTANTS DetectionEnabled, HasDetectionDependency,
          DownloadsArtifacts, CallsOpaqueReusableWorkflow,
          ReadsForbiddenOutput, ReadsAgentFiles, HasCustomDependents,
          UsesOnlyPermittedReferences

ASSUME DetectionEnabled \in BOOLEAN
ASSUME HasDetectionDependency \in BOOLEAN
ASSUME DownloadsArtifacts \in BOOLEAN
ASSUME CallsOpaqueReusableWorkflow \in BOOLEAN
ASSUME ReadsForbiddenOutput \in BOOLEAN
ASSUME ReadsAgentFiles \in BOOLEAN
ASSUME HasCustomDependents \in BOOLEAN
ASSUME UsesOnlyPermittedReferences \in BOOLEAN

DetectionResults == {"pending", "success", "failure", "skipped"}
Verdicts == {"pending", "clean", "warn", "none"}
JobStates == {"queued", "running", "complete"}

StatusOnlyContract ==
    HasDetectionDependency
    /\ ~DownloadsArtifacts
    /\ ~CallsOpaqueReusableWorkflow
    /\ ~ReadsForbiddenOutput
    /\ ~ReadsAgentFiles
    /\ ~HasCustomDependents
    /\ UsesOnlyPermittedReferences

VARIABLE s
vars == <<s>>

Init ==
    s = [detection |-> IF DetectionEnabled THEN "pending" ELSE "skipped",
         verdict |-> IF DetectionEnabled THEN "pending" ELSE "none",
         job |-> "queued"]

CompleteDetection ==
    \E result \in {"success", "failure", "skipped"}:
        \E verdict \in {"clean", "warn", "none"}:
            /\ DetectionEnabled
            /\ s.detection = "pending"
            /\ (result = "success" => verdict \in {"clean", "warn"})
            /\ (result # "success" => verdict = "none")
            /\ s' = [s EXCEPT !.detection = result, !.verdict = verdict]

StartStatus ==
    /\ s.job = "queued"
    /\ IF DetectionEnabled
       THEN s.detection # "pending" /\ StatusOnlyContract
       ELSE TRUE
    /\ s' = [s EXCEPT !.job = "running"]

FinishStatus ==
    /\ s.job = "running"
    /\ s' = [s EXCEPT !.job = "complete"]

Next == CompleteDetection \/ StartStatus \/ FinishStatus

Spec ==
    Init
    /\ [][Next]_vars
    /\ WF_vars(CompleteDetection)
    /\ WF_vars(StartStatus)
    /\ WF_vars(FinishStatus)

TypeOK ==
    /\ s.detection \in DetectionResults
    /\ s.verdict \in Verdicts
    /\ s.job \in JobStates

DetectionDependencyOrdering ==
    s.job \in {"running", "complete"}
    => ~DetectionEnabled \/ (s.detection # "pending" /\ HasDetectionDependency)

StatusOnlyNoAgentData ==
    s.job \in {"running", "complete"}
    => ~DetectionEnabled \/ StatusOnlyContract

StatusEventuallyCompletes == <> (s.job = "complete")

=============================================================================
