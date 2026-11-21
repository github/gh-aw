------------------------- MODULE MCPScriptInvocation -------------------------
EXTENDS Naturals, Sequences

CONSTANT MaxAttempts
ASSUME MaxAttempts \in Nat \ {0}

VARIABLES a, history
vars == <<a, history>>

NewAttempt(n) ==
    [id |-> n, phase |-> "request", authenticated |-> FALSE,
     valid |-> FALSE, ready |-> FALSE, executions |-> 0,
     ephemeral |-> FALSE, sanitized |-> 0, file |-> 0, preview |-> 0,
     outcome |-> "none", recoverable |-> FALSE, sideEffect |-> FALSE]

Init == a = NewAttempt(1) /\ history = <<>>

Failure(kind, hasResources) ==
    a' = [a EXCEPT !.phase = "classified", !.outcome = kind,
                   !.ephemeral = hasResources,
                   !.recoverable = (kind \in {"startup-transient", "timeout"})]

Authenticate ==
    /\ a.phase = "request"
    /\ \/ a' = [a EXCEPT !.phase = "authenticated", !.authenticated = TRUE]
       \/ Failure("rejected", FALSE)

Validate ==
    /\ a.phase = "authenticated"
    /\ \/ a' = [a EXCEPT !.phase = "validated", !.valid = TRUE]
       \/ Failure("invalid-input", FALSE)

Bootstrap ==
    /\ a.phase = "validated"
    /\ \/ a' = [a EXCEPT !.phase = "ready", !.ready = TRUE, !.ephemeral = TRUE]
       \/ \E kind \in {"startup-transient", "startup-permanent"},
             hasResources \in BOOLEAN: Failure(kind, hasResources)

Execute ==
    /\ a.phase = "ready"
    /\ \E effect \in BOOLEAN:
           a' = [a EXCEPT !.phase = "executed", !.executions = @ + 1,
                          !.sideEffect = effect]

Capture ==
    /\ a.phase = "executed"
    /\ \/ a' = [a EXCEPT !.phase = "sanitized", !.sanitized = a.id]
       \/ \E kind \in {"timeout", "execution-permanent"}:
              Failure(kind, a.ephemeral)

ClassifySuccess ==
    /\ a.phase = "sanitized"
    /\ \E large, includePreview \in BOOLEAN:
           a' = [a EXCEPT !.phase = "classified",
                          !.outcome = IF large THEN "file" ELSE "inline",
                          !.file = IF large THEN a.sanitized ELSE 0,
                          !.preview = IF large /\ includePreview THEN a.sanitized ELSE 0]

Clean ==
    /\ a.phase = "classified"
    /\ a' = [a EXCEPT !.phase = "clean", !.ephemeral = FALSE]

Respond ==
    /\ a.phase = "clean"
    /\ a' = [a EXCEPT !.phase = "responded"]

Retry ==
    /\ a.phase = "responded"
    /\ a.recoverable /\ a.id < MaxAttempts
    \* A caller checks retry safety for an attempt with possible external effects.
    /\ \E safeToRetry \in BOOLEAN:
           /\ ~a.sideEffect \/ safeToRetry
           /\ a' = NewAttempt(a.id + 1)
    /\ history' = Append(history, a)

Stop ==
    /\ a.phase = "responded"
    /\ a' = [a EXCEPT !.phase = "done"]

Next ==
    \/ /\ UNCHANGED history
       /\ Authenticate \/ Validate \/ Bootstrap \/ Execute \/ Capture \/
          ClassifySuccess \/ Clean \/ Respond \/ Stop
    \/ Retry

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

Attempts == {a} \cup {history[i] : i \in 1..Len(history)}

ValidExecution ==
    \A x \in Attempts:
        /\ x.executions \in 0..1
        /\ x.executions = 1 => x.authenticated /\ x.valid /\ x.ready

SafeOutput ==
    \A x \in Attempts:
        /\ x.sanitized # 0 => x.executions = 1 /\ x.sanitized = x.id
        /\ x.outcome \in {"inline", "file"} => x.sanitized = x.id
        /\ (x.file # 0) = (x.outcome = "file")
        /\ x.file # 0 => x.file = x.sanitized /\ x.file = x.id
        /\ x.preview # 0 => x.file # 0 /\ x.preview = x.file

CleanResponse ==
    \A x \in Attempts:
        x.phase \in {"responded", "done"} => ~x.ephemeral /\ x.outcome # "none"

RetryBudget ==
    /\ a.id = Len(history) + 1
    /\ a.id \in 1..MaxAttempts
    /\ \A i \in 1..Len(history):
        /\ history[i].id = i
        /\ history[i].phase = "responded"
        /\ history[i].recoverable
    /\ \A x \in Attempts:
           x.recoverable = (x.outcome \in {"startup-transient", "timeout"})

Terminates == <>(a.phase = "done")

=============================================================================
