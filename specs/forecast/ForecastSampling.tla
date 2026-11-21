-------------------------- MODULE ForecastSampling --------------------------
EXTENDS Naturals

CONSTANTS Workflows, RetryBudget, ServerDelays
VARIABLES samples

Empty == [phase |-> "request", stage |-> "runs", retries |-> 0,
          minimum |-> 0, delay |-> 0, waited |-> 0, warnings |-> 0, row |-> "none"]
Init == samples = [w \in Workflows |-> Empty]

Succeeded(w) ==
    /\ samples[w].phase = "request"
    /\ samples' = [samples EXCEPT ![w] =
        IF @.stage = "runs" THEN [Empty EXCEPT !.stage = "artifact"]
        ELSE [@ EXCEPT !.phase = "done", !.row = "sampled"]]

NoRuns(w) ==
    /\ samples[w].phase = "request"
    /\ samples[w].stage = "runs"
    /\ samples' = [samples EXCEPT ![w].phase = "done", ![w].row = "empty"]

RateLimited(w) ==
    LET s == samples[w] IN
    /\ samples[w].phase = "request"
    /\ samples' = [samples EXCEPT ![w] =
        IF s.retries = RetryBudget THEN
            [s EXCEPT !.phase = "done", !.row = "rate-limited"]
        ELSE
            [s EXCEPT !.phase = "warn",
                      !.delay = IF s.retries = 0
                               THEN 0 ELSE 2 * s.delay, !.waited = 0]]

Warn(w) ==
    /\ samples[w].phase = "warn"
    /\ \E d \in IF samples[w].retries = 0 THEN ServerDelays ELSE {samples[w].delay} :
        samples' = [samples EXCEPT ![w].phase = "wait", ![w].delay = d,
                                  ![w].minimum = IF samples[w].retries = 0 THEN d ELSE @,
                                  ![w].warnings = @ + 1]

Tick(w) ==
    /\ samples[w].phase = "wait"
    /\ samples[w].waited < samples[w].delay
    /\ samples' = [samples EXCEPT ![w].waited = @ + 1]

Retry(w) ==
    /\ samples[w].phase = "wait"
    /\ samples[w].waited = samples[w].delay
    /\ samples' = [samples EXCEPT ![w].phase = "request", ![w].retries = @ + 1]

Partial(w) ==
    /\ samples[w].phase = "request"
    /\ samples' = [samples EXCEPT ![w].phase = "done", ![w].row = "partial"]

Next == \E w \in Workflows :
    Succeeded(w) \/ NoRuns(w) \/ RateLimited(w) \/ Warn(w) \/
    Tick(w) \/ Retry(w) \/ Partial(w)
Spec == Init /\ [][Next]_samples /\ WF_samples(Next)

BoundedRetries ==
    \A w \in Workflows :
        LET s == samples[w] IN
        /\ s.retries \in 0..RetryBudget
        /\ s.phase = "wait" => s.retries < RetryBudget
        /\ s.row = "rate-limited" => s.retries = RetryBudget

WarnAndWaitBeforeRetry ==
    \A w \in Workflows :
        LET s == samples[w] IN
        /\ s.phase = "wait" => s.warnings = s.retries + 1
        /\ s.phase = "request" /\ s.retries > 0 =>
            /\ s.warnings = s.retries
            /\ s.waited = s.delay
            /\ s.minimum \in ServerDelays
            /\ s.delay >= s.minimum * 2 ^ (s.retries - 1)

ReportHasOneRowPerFinishedWorkflow ==
    \A w \in Workflows :
        LET s == samples[w] IN
        /\ (s.phase = "done") = (s.row \in {"sampled", "empty", "rate-limited", "partial"})
        /\ s.row = "sampled" => s.stage = "artifact"
        /\ s.row = "empty" => s.stage = "runs"

EveryWorkflowFinishes == \A w \in Workflows : <>(samples[w].phase = "done")
=============================================================================
