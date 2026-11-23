------------------------- MODULE AdvisoryHandoff -------------------------
EXTENDS Naturals

CONSTANTS Advisories, Focus, AllowBaseDrift, AllowLostRequest
ASSUME Focus \in Advisories

Bases == {0, 1}
Patches == {"patch-1", "patch-2"}
Plans == [patch : Patches, base : Bases]
None == [patch |-> "none", base |-> 0]
Empty == [fork |-> "absent", recorded |-> None, shown |-> None,
          approved |-> None, checked |-> None, base |-> 0,
          branch |-> None, outcome |-> "idle"]

VARIABLES sideReady, handoff
vars == <<sideReady, handoff>>
Init ==
  /\ sideReady = FALSE
  /\ handoff = [a \in Advisories |-> Empty]

Change(a, h) ==
  /\ handoff' = [handoff EXCEPT ![a] = h]
  /\ UNCHANGED sideReady

Setup ==
  /\ ~sideReady
  /\ sideReady' = TRUE
  /\ UNCHANGED handoff

ClaimFork(a) ==
  /\ sideReady
  /\ handoff[a].fork = "absent"
  /\ Change(a, [handoff[a] EXCEPT !.fork = "claimed"])

SendForkRequest(a) ==
  /\ handoff[a].fork = "claimed"
  /\ Change(a, [handoff[a] EXCEPT !.fork = "requested"])

LoseForkRequest(a) ==
  /\ AllowLostRequest
  /\ handoff[a].fork = "claimed"
  /\ Change(a, [handoff[a] EXCEPT !.fork = "uncertain"])

ProvisionFork(a) ==
  /\ handoff[a].fork = "requested"
  /\ Change(a, [handoff[a] EXCEPT !.fork = "private"])

Record(a, p) ==
  /\ sideReady
  /\ handoff[a].fork = "private"
  /\ Change(a, [handoff[a] EXCEPT !.recorded = p])

Inspect(a) ==
  /\ handoff[a].recorded # None
  /\ Change(a, [handoff[a] EXCEPT !.shown = handoff[a].recorded])

Approve(a) ==
  /\ handoff[a].shown # None
  /\ Change(a, [handoff[a] EXCEPT !.approved = handoff[a].shown])

CheckBeforePush(a) ==
  LET h == handoff[a] IN
  /\ h.recorded \in Plans
  /\ h.recorded = h.approved
  /\ h.recorded.base = h.base
  /\ h.fork = "private"
  /\ h.checked = None
  /\ Change(a, [h EXCEPT !.checked = h.approved, !.outcome = "idle"])

MoveBase(a) ==
  /\ AllowBaseDrift
  /\ a = Focus
  /\ handoff[a].base = 0
  /\ Change(a, [handoff[a] EXCEPT !.base = 1])

PushOrRetry(a) ==
  LET h == handoff[a] IN
  /\ h.checked # None
  /\ h.outcome = "idle"
  /\ h.branch \in {None, h.checked}
  /\ Change(a, [h EXCEPT !.branch = h.checked, !.outcome = "pushed"])

CheckAfterPush(a) ==
  LET h == handoff[a] IN
  /\ h.outcome = "pushed"
  /\ Change(a, [h EXCEPT !.outcome = IF h.base = h.checked.base THEN "ready" ELSE "warned",
                         !.checked = None])

Next ==
  \/ Setup
  \/ \E a \in Advisories :
       \/ ClaimFork(a) \/ SendForkRequest(a) \/ LoseForkRequest(a) \/ ProvisionFork(a)
       \/ \E p \in (IF a = Focus THEN Plans ELSE {[patch |-> "patch-1", base |-> 0]}) :
            Record(a, p)
       \/ Inspect(a) \/ Approve(a) \/ CheckBeforePush(a)
       \/ MoveBase(a) \/ PushOrRetry(a) \/ CheckAfterPush(a)

Spec == Init /\ [][Next]_vars
FairSpec ==
  /\ Spec
  /\ \A a \in Advisories :
       /\ WF_vars(SendForkRequest(a))
       /\ WF_vars(ProvisionFork(a))

TypeOK ==
  /\ sideReady \in BOOLEAN
  /\ handoff \in [Advisories ->
       [fork : {"absent", "claimed", "requested", "uncertain", "private"},
        recorded : Plans \cup {None}, shown : Plans \cup {None}, approved : Plans \cup {None},
        checked : Plans \cup {None}, base : Bases, branch : Plans \cup {None},
        outcome : {"idle", "pushed", "ready", "warned"}]]

ApprovedPrivatePush ==
  [][\A a \in Advisories :
    IF handoff[a].branch = None /\ handoff'[a].branch # None
    THEN /\ handoff[a].fork = "private"
         /\ handoff'[a].branch = handoff[a].checked
    ELSE TRUE]_vars

NeverOverwrite ==
  [][\A a \in Advisories :
    handoff[a].branch # None => handoff'[a].branch = handoff[a].branch]_vars

ReportsReadyOnlyAfterMatchingBase ==
  [][\A a \in Advisories :
    handoff[a].outcome = "pushed" /\ handoff'[a].outcome = "ready" =>
      handoff[a].branch.base = handoff[a].base]_vars

BaseMatchesAtBranchCreation ==
  [][\A a \in Advisories :
    handoff[a].branch = None /\ handoff'[a].branch # None =>
      handoff'[a].branch.base = handoff[a].base]_vars

ClaimEventuallyResolves ==
  \A a \in Advisories :
    (handoff[a].fork = "claimed") ~> (handoff[a].fork = "private")
=============================================================================
