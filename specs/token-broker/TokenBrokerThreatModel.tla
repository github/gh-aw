----------------------- MODULE TokenBrokerThreatModel -----------------------
EXTENDS Naturals, FiniteSets

\* EXPERIMENTAL. Informative, bounded model of the token broker threat model
\* (Token Broker Specification, Section 5.4 and Appendix E). It is not a
\* conformance artifact and may change without deprecation.
\*
\* Fixes selects candidate hardening beyond the SV-01..SV-35 baseline:
\*   "runner"       broker denies runners not enrolled as GitHub-hosted or
\*                  attested ephemeral self-hosted runners.
\*   "reusable"     both the caller workflow_ref/sha and the reusable
\*                  job_workflow_ref/sha must be approved.
\*   "close-first"  invalidation closes the job before revoking, and recording
\*                  an issued credential atomically re-checks closure.
\*   "server-close" the broker closes a job on trusted job-completion evidence
\*                  even when the client cleanup step never ran.
\* Mutation removes one baseline requirement as a negative control. Mutations
\* are deliberately insecure and are never runtime options.
CONSTANTS Fixes, Mutation, Runner, MaxTokens

AllFixes == {"runner", "reusable", "close-first", "server-close"}
Mutations == {"none", "no-env-exclusion", "unhardened-host-step",
              "no-audience", "no-tenant", "no-revision", "pr-trust",
              "no-job-identity", "no-close-check", "record-after-return",
              "no-runner-check"}

Runners == {"hosted", "ephemeral", "persistent"}

ASSUME /\ Fixes \subseteq AllFixes /\ Mutation \in Mutations
       /\ Runner \in Runners /\ MaxTokens \in Nat

\* Job executions. agent/safeout/caller run gh-aw jobs; prci and foreign only
\* ever present attacker-obtained OIDC tokens.
LegitJobs == {"agent", "safeout", "caller"}
Contexts == LegitJobs \cup {"prci", "foreign"}
Audiences == {"broker", "cloud"}
OIDC == [ctx : Contexts, aud : Audiences]

RepoOf(c) == CASE c = "caller" -> "R2" [] c = "foreign" -> "X" [] OTHER -> "R"
\* Top-level workflow_ref/workflow_sha approval. A foreign clone only matches
\* when approval ignores repository IDs; an agent-authored PR head only
\* matches when approval is by path rather than by revision.
WfApproved(c) == CASE c \in {"agent", "safeout"} -> TRUE
                   [] c = "prci" -> Mutation = "pr-trust"
                   [] c = "foreign" -> Mutation = "no-tenant"
                   [] OTHER -> FALSE
\* caller invokes the approved gh-aw workflow as a reusable workflow.
JwfApproved(c) == IF c = "caller" THEN TRUE ELSE WfApproved(c)

VARIABLES phase, ex, exTok, inv, issued, closed, nextId,
          advOIDC, advHas, used, compromised, prOpened

vars == <<phase, ex, exTok, inv, issued, closed, nextId,
          advOIDC, advHas, used, compromised, prOpened>>

\* Runner kind of the gh-aw jobs; other contexts are GitHub-hosted.
RunnerOf(c) == IF c \in {"agent", "safeout"} THEN Runner ELSE "hosted"

RevisionOK(c) ==
  \/ Mutation = "no-revision"
  \/ IF "reusable" \in Fixes
       THEN WfApproved(c) /\ JwfApproved(c)
       ELSE WfApproved(c) \/ (c = "caller" /\ JwfApproved(c))   \* SV-16 "or"

Accept(o) ==
  /\ o.aud = "broker" \/ Mutation = "no-audience"                   \* SV-12
  /\ o.ctx # "foreign" \/ Mutation = "no-tenant"                    \* SV-15
  /\ RevisionOK(o.ctx)                                              \* SV-16
  /\ o.ctx # "prci" \/ Mutation = "pr-trust"                        \* SV-17
  /\ "runner" \notin Fixes \/ Mutation = "no-runner-check"
     \/ RunnerOf(o.ctx) \in {"hosted", "ephemeral"}
  /\ o.ctx \notin closed \/ Mutation = "no-close-check"             \* SV-19

\* SV-18/SV-23: without verified job identity reader and writer roles cannot
\* be told apart, so the broker must grant the union to every job.
RoleOf(c) == IF Mutation # "no-job-identity" /\ c = "agent"
               THEN "reader" ELSE "writer"
Scope(c) == {"R"}   \* policy repositories of the approved workflow (SV-22)

Tok(id, c, holder, rec, ret) ==
  [id |-> id, ctx |-> c, role |-> RoleOf(c), scope |-> Scope(c),
   holder |-> holder, recorded |-> rec, returned |-> ret, revoked |-> FALSE]
SetRecorded(id) ==
  {IF t.id = id THEN [t EXCEPT !.recorded = TRUE] ELSE t : t \in issued}
SetAborted(id) ==
  {IF t.id = id THEN [t EXCEPT !.recorded = TRUE, !.revoked = TRUE] ELSE t
     : t \in issued}
SetReturned(id) ==
  {IF t.id = id THEN [t EXCEPT !.returned = TRUE] ELSE t : t \in issued}
RevokeRecorded(j) ==
  {IF t.ctx = j /\ t.recorded THEN [t EXCEPT !.revoked = TRUE] ELSE t
     : t \in issued}

Init ==
  /\ phase = [j \in LegitJobs |-> "idle"]
  /\ ex = [j \in LegitJobs |-> "none"]
  /\ exTok = [j \in LegitJobs |-> 0]
  /\ inv = [j \in LegitJobs |-> "none"]
  /\ issued = {} /\ closed = {} /\ nextId = 1
  /\ advOIDC = {[ctx |-> "foreign", aud |-> "broker"]}
  /\ advHas = {} /\ used = {} /\ compromised = {} /\ prOpened = FALSE

AdvUnchanged == UNCHANGED <<advOIDC, advHas, used, compromised, prOpened>>

---------------------------------------------------------------------------
\* Legitimate job lifecycle. Exchange phases are server-side and continue
\* after the job is cancelled, which is what creates invalidation races.

Start(j) ==
  /\ phase[j] = "idle"
  /\ j = "safeout" => phase["agent"] = "done"
  /\ phase' = [phase EXCEPT ![j] = "running"]
  /\ UNCHANGED <<ex, exTok, inv, issued, closed, nextId>> /\ AdvUnchanged

ExBegin(j) ==
  /\ phase[j] = "running" /\ ex[j] = "none"
  /\ ex' = [ex EXCEPT ![j] = IF Accept([ctx |-> j, aud |-> "broker"])
                              THEN "checked" ELSE "denied"]
  /\ UNCHANGED <<phase, exTok, inv, issued, closed, nextId>> /\ AdvUnchanged

ExMint(j) ==
  /\ ex[j] = "checked" /\ nextId <= MaxTokens
  /\ issued' = issued \cup {Tok(nextId, j, "job", FALSE, FALSE)}
  /\ exTok' = [exTok EXCEPT ![j] = nextId]
  /\ nextId' = nextId + 1
  /\ ex' = [ex EXCEPT ![j] = "minted"]
  /\ UNCHANGED <<phase, inv, closed>> /\ AdvUnchanged

\* SV-28 records before returning. With "close-first" the record is an atomic
\* compare-and-set against closure; a closed job's fresh credential is revoked.
ExRecord(j) ==
  /\ ex[j] \in {"minted", "returned-unrecorded"}
  /\ IF "close-first" \in Fixes /\ j \in closed
       THEN /\ issued' = SetAborted(exTok[j])
            /\ ex' = [ex EXCEPT ![j] = "aborted"]
       ELSE /\ issued' = SetRecorded(exTok[j])
            /\ ex' = [ex EXCEPT ![j] = IF ex[j] = "minted" THEN "recorded"
                                       ELSE "returned"]
  /\ UNCHANGED <<phase, exTok, inv, closed, nextId>> /\ AdvUnchanged

ExReturn(j) ==
  /\ \/ ex[j] = "recorded"
     \/ ex[j] = "minted" /\ Mutation = "record-after-return"
  /\ issued' = SetReturned(exTok[j])
  /\ ex' = [ex EXCEPT ![j] = IF ex[j] = "recorded" THEN "returned"
                             ELSE "returned-unrecorded"]
  /\ advHas' = IF j \in compromised THEN advHas \cup {exTok[j]} ELSE advHas
  /\ UNCHANGED <<phase, exTok, inv, closed, nextId, advOIDC, used,
                 compromised, prOpened>>

\* The broker process fails after minting and before recording.
ExCrash(j) ==
  /\ ex[j] \in {"minted", "returned-unrecorded"}
  /\ ex' = [ex EXCEPT ![j] = "crashed"]
  /\ UNCHANGED <<phase, exTok, inv, issued, closed, nextId>> /\ AdvUnchanged

\* Job ends; the always() cleanup step either runs or never runs (runner loss,
\* hard cancellation, timeout).
End(j) ==
  /\ phase[j] = "running"
  /\ phase' = [phase EXCEPT ![j] = "done"]
  /\ \E c \in {"pending", "skipped"} : inv' = [inv EXCEPT ![j] = c]
  /\ UNCHANGED <<ex, exTok, issued, closed, nextId>> /\ AdvUnchanged

\* Baseline SV-31 order: revoke, then close.
InvRevokeFirst(j) ==
  /\ "close-first" \notin Fixes /\ inv[j] = "pending"
  /\ issued' = RevokeRecorded(j) /\ inv' = [inv EXCEPT ![j] = "revoked"]
  /\ UNCHANGED <<phase, ex, exTok, closed, nextId>> /\ AdvUnchanged
InvCloseSecond(j) ==
  /\ inv[j] = "revoked"
  /\ closed' = closed \cup {j} /\ inv' = [inv EXCEPT ![j] = "done"]
  /\ UNCHANGED <<phase, ex, exTok, issued, nextId>> /\ AdvUnchanged

\* Hardened order: close, then revoke.
InvCloseFirst(j) ==
  /\ "close-first" \in Fixes /\ inv[j] = "pending"
  /\ closed' = closed \cup {j} /\ inv' = [inv EXCEPT ![j] = "closed"]
  /\ UNCHANGED <<phase, ex, exTok, issued, nextId>> /\ AdvUnchanged
InvRevokeSecond(j) ==
  /\ inv[j] = "closed"
  /\ issued' = RevokeRecorded(j) /\ inv' = [inv EXCEPT ![j] = "done"]
  /\ UNCHANGED <<phase, ex, exTok, closed, nextId>> /\ AdvUnchanged

ServerClose(j) ==
  /\ "server-close" \in Fixes /\ phase[j] = "done" /\ inv[j] = "skipped"
  /\ inv' = [inv EXCEPT ![j] = "pending"]
  /\ UNCHANGED <<phase, ex, exTok, issued, closed, nextId>> /\ AdvUnchanged

---------------------------------------------------------------------------
\* Adversary: a prompt-injected agent, attacker code it causes to run, an
\* enrolled-tenant repository writer, and third parties.

Gain(os) ==
  /\ ~(os \subseteq advOIDC)
  /\ advOIDC' = advOIDC \cup os
  /\ UNCHANGED <<phase, ex, exTok, inv, issued, closed, nextId,
                 advHas, used, compromised, prOpened>>

AllAud(c) == {[ctx |-> c, aud |-> a] : a \in Audiences}

\* TM-1: ACTIONS_ID_TOKEN_REQUEST_* reaches the agent sandbox.
StealAgentEnv ==
  Mutation = "no-env-exclusion" /\ phase["agent"] = "running" /\ Gain(AllAud("agent"))

\* A host step after the agent runs workspace-controlled programs (Git hooks,
\* fsmonitor, filters) while the job holds id-token: write.
HostStep ==
  Mutation = "unhardened-host-step" /\ phase["agent"] = "done" /\ Gain(AllAud("agent"))

\* The agent plants files on a persistent self-hosted runner workspace; a
\* later job on the same runner executes them with its own OIDC request token.
PoisonRunner ==
  /\ Runner = "persistent" /\ phase["agent"] = "done"
  /\ phase["safeout"] = "running" /\ "safeout" \notin compromised
  /\ compromised' = compromised \cup {"safeout"}
  /\ advOIDC' = advOIDC \cup AllAud("safeout")
  /\ advHas' = advHas \cup {t.id : t \in {u \in issued :
                 u.ctx = "safeout" /\ u.returned /\ u.holder = "job"}}
  /\ UNCHANGED <<phase, ex, exTok, inv, issued, closed, nextId,
                 used, prOpened>>

\* TM-11: a cloud relying party receives a safe-output OIDC token for its own
\* audience and replays it.
CloudReplay == phase["safeout"] = "running" /\ Gain({[ctx |-> "safeout", aud |-> "cloud"]})

\* Unlike GITHUB_TOKEN, a GitHub App writer token's pull request triggers
\* workflows. Agent-authored code then runs in a same-repository CI job.
OpenPR ==
  /\ ~prOpened
  /\ \E t \in issued : t.ctx = "safeout" /\ t.role = "writer" /\ t.returned
  /\ prOpened' = TRUE
  /\ advOIDC' = advOIDC \cup {[ctx |-> "prci", aud |-> "broker"]}
  /\ UNCHANGED <<phase, ex, exTok, inv, issued, closed, nextId,
                 advHas, used, compromised>>

\* TM-9: a broker-audience OIDC token leaks after the job is closed.
LeakAfterClose(j) ==
  j \in closed /\ j # "caller" /\ Gain({[ctx |-> j, aud |-> "broker"]})

AdvExchange(o) ==
  /\ o \in advOIDC \ used
  /\ used' = used \cup {o}
  /\ IF Accept(o) /\ nextId <= MaxTokens
       THEN /\ issued' = issued \cup {Tok(nextId, o.ctx, "adv", TRUE, TRUE)}
            /\ advHas' = advHas \cup {nextId}
            /\ nextId' = nextId + 1
       ELSE UNCHANGED <<issued, advHas, nextId>>
  /\ UNCHANGED <<phase, ex, exTok, inv, closed, advOIDC, compromised,
                 prOpened>>

---------------------------------------------------------------------------

JobNext(j) ==
  \/ Start(j) \/ ExBegin(j) \/ ExMint(j) \/ ExRecord(j) \/ ExReturn(j)
  \/ End(j) \/ InvRevokeFirst(j) \/ InvCloseSecond(j) \/ InvCloseFirst(j)
  \/ InvRevokeSecond(j) \/ ServerClose(j)

Next ==
  \/ \E j \in LegitJobs : JobNext(j) \/ ExCrash(j) \/ LeakAfterClose(j)
  \/ StealAgentEnv \/ HostStep \/ PoisonRunner \/ CloudReplay \/ OpenPR
  \/ \E o \in OIDC : AdvExchange(o)

Fairness == \A j \in LegitJobs : WF_vars(JobNext(j))
Spec == Init /\ [][Next]_vars /\ Fairness

---------------------------------------------------------------------------

TypeOK ==
  /\ advOIDC \subseteq OIDC /\ used \subseteq OIDC
  /\ closed \subseteq LegitJobs /\ compromised \subseteq LegitJobs
  /\ advHas \subseteq 1..MaxTokens /\ nextId \in 1..(MaxTokens + 1)

\* No raw brokered credential reaches attacker-controlled code.
NoCredentialDisclosure == advHas = {}
\* Only enrolled, approved gh-aw job executions receive credentials.
ApprovedContextOnly == \A t \in issued : t.ctx \in LegitJobs
\* A credential never covers repositories beyond the requesting repository.
RepositoryBound == \A t \in issued : t.scope \subseteq {RepoOf(t.ctx)}
\* The main agent job (GitHub tools) only receives read-only credentials.
ReaderInAgentJob == \A t \in issued : t.ctx = "agent" => t.role = "reader"
\* After a 204 invalidation, every credential the job could hold is revoked.
InvalidationComplete ==
  \A j \in LegitJobs : inv[j] = "done" =>
    \A t \in issued : (t.ctx = j /\ (t.recorded \/ t.returned)) => t.revoked

AllRevoked(j) ==
  \A t \in issued : (t.ctx = j /\ (t.recorded \/ t.returned)) => t.revoked
\* Every credential of a finished job is eventually revoked, not merely
\* left to expire.
CompletionRevokes == \A j \in LegitJobs : (phase[j] = "done") ~> AllRevoked(j)

\* Witness: agent-authored code can run in same-repository CI.
NoAgentAuthoredCI == ~prOpened
=============================================================================
