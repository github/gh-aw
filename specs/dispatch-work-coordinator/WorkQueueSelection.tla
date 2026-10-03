-------------------------- MODULE WorkQueueSelection --------------------------
EXTENDS DispatchWorkCoordinator

\* Dispatcher-only refinement: worker/recovery interleavings are checked by the
\* baseline profiles; this profile isolates multi-item queue selection and CAS.
SelectionNext ==
    \E d \in Dispatchers :
         (\E t \in Intents : Stage(d, t))
         \/ CaptureDispatcherSnapshot(d)
         \/ (\E p \in Policies, c \in Claims : StageNext(d, p, c))
         \/ PrepareDispatch(d) \/ PushDispatch(d) \/ RetryDispatch(d)

SelectionSpec == Init /\ [][SelectionNext]_vars
=============================================================================
