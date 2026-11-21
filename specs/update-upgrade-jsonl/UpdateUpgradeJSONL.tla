------------------------- MODULE UpdateUpgradeJSONL -------------------------
EXTENDS Naturals, Sequences

Repositories == {"a", "b"}
Event(kind, repo) == [kind |-> kind, repo |-> repo, status |-> "none"]
Started == Event("command_started", "none")
Terminal(value) == [Event("command_completed", "none") EXCEPT !.status = value]
TerminalKinds == {Terminal("success"), Terminal("error")}

VARIABLES phase, dryRun, relaunched, effects, reported, completed, stream, exit
vars == <<phase, dryRun, relaunched, effects, reported, completed, stream, exit>>

Init ==
  /\ phase = "starting"
  /\ dryRun \in BOOLEAN
  /\ relaunched = FALSE
  /\ effects = {}
  /\ reported = {}
  /\ completed = {}
  /\ stream = <<>>
  /\ exit = "pending"

Start ==
  /\ phase = "starting"
  /\ phase' = "reporting"
  /\ stream' = <<Started>>
  /\ UNCHANGED <<dryRun, relaunched, effects, reported, completed, exit>>

Relaunch ==
  /\ phase = "reporting"
  /\ ~relaunched
  /\ relaunched' = TRUE
  /\ UNCHANGED <<phase, dryRun, effects, reported, completed, stream, exit>>

CreatePR(repo) ==
  /\ phase = "reporting"
  /\ ~dryRun
  /\ repo \notin effects \cup completed
  /\ effects' = effects \cup {repo}
  /\ UNCHANGED <<phase, dryRun, relaunched, reported, completed, stream, exit>>

ReportPR(repo) ==
  /\ phase = "reporting"
  /\ repo \in effects \ (reported \cup completed)
  /\ reported' = reported \cup {repo}
  /\ stream' = Append(stream, Event("pull_request", repo))
  /\ UNCHANGED <<phase, dryRun, relaunched, effects, completed, exit>>

CompleteRepo(repo) ==
  /\ phase = "reporting"
  /\ repo \notin completed
  /\ completed' = completed \cup {repo}
  /\ stream' =
       IF dryRun
       THEN Append(Append(stream, Event("repository_preview", repo)),
                   Event("repository_completed", repo))
       ELSE Append(stream, Event("repository_completed", repo))
  /\ UNCHANGED <<phase, dryRun, relaunched, effects, reported, exit>>

Finish(status) ==
  /\ phase = "reporting"
  /\ (status = "success" => completed = Repositories)
  /\ phase' = "terminal"
  /\ stream' = Append(stream, Terminal(status))
  /\ UNCHANGED <<dryRun, relaunched, effects, reported, completed, exit>>

Exit(status) ==
  /\ phase # "closed"
  /\ phase' = "closed"
  /\ exit' = status
  /\ UNCHANGED <<dryRun, relaunched, effects, reported, completed, stream>>

Next ==
  \/ Start
  \/ Relaunch
  \/ \E r \in Repositories: CreatePR(r) \/ ReportPR(r) \/ CompleteRepo(r)
  \/ \E s \in {"success", "error"}: Finish(s)
  \/ phase = "terminal" /\ Exit(stream[Len(stream)].status)
  \/ Exit("interrupted")
  \/ phase = "closed" /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

Has(event) == \E i \in 1..Len(stream): stream[i] = event
EndsWith(status) ==
  IF Len(stream) = 0 THEN FALSE ELSE stream[Len(stream)] = Terminal(status)

ConsumerSuccess == exit = "success" /\ EndsWith("success")
ConsumerPR(repo) == IF Has(Event("pull_request", repo)) THEN "created" ELSE "unknown"

EnvelopeOrder ==
  /\ (Len(stream) > 0 => stream[1] = Started)
  /\ \A i \in 2..Len(stream): stream[i] # Started
  /\ \A i \in 1..(Len(stream) - 1): stream[i] \notin TerminalKinds
  /\ (exit \in {"success", "error"} => EndsWith(exit))

EvidenceBoundary ==
  /\ (dryRun => effects = {} /\ reported = {})
  /\ \A r \in Repositories:
       /\ (ConsumerPR(r) = "created" => r \in effects /\ ~dryRun)
       /\ (Has(Event("repository_completed", r)) <=> r \in completed)
  /\ (ConsumerSuccess => completed = Repositories)
=============================================================================
