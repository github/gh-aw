--------------------------- MODULE PackageLifecycle --------------------------
EXTENDS FiniteSets

VARIABLES op, before, s
vars == <<op, before, s>>

Paths == {"retained", "added", "stale", "modified", "untracked", "foreign"}
Outputs == {"retained", "added"}
Entry(owner, digest, recorded) ==
    [owner |-> owner, digest |-> digest, recorded |-> recorded]
Absent == Entry("none", "none", "none")
Installed == Entry("package", "v1", "v1")
New == Entry("package", "v2", "v2")
Baseline ==
    [p \in Paths |->
        CASE p \in {"retained", "stale"} -> Installed
          [] p = "modified" -> Entry("package", "edited", "v1")
          [] p = "untracked" -> Entry("none", "manual", "none")
          [] p = "foreign" -> Entry("other-package", "v1", "v1")
          [] OTHER -> Absent]

Owned(p) == before[p].owner = "package"
Unchanged(p) == before[p].digest = before[p].recorded
Stale == {p \in Paths \ Outputs : Owned(p) /\ Unchanged(p)}
OwnedPaths == {p \in Paths : Owned(p)}

Init ==
    /\ op \in {"install", "update", "remove"}
    /\ before = IF op = "install"
                 THEN [Baseline EXCEPT !["retained"] = Absent,
                                       !["stale"] = Absent, !["modified"] = Absent]
                 ELSE Baseline
    /\ s = [phase |-> IF op = "remove" THEN "removing" ELSE "manifest",
            repo |-> before, staged |-> {}, writeError |-> "none",
            rollbackErrors |-> {}, attempted |-> {}, deleted |-> {},
            confirmed |-> {}, deleteErrors |-> {}, summary |-> {}]

Prepare ==
    /\ s.phase \in {"manifest", "resolve", "download", "compile", "resources"}
    /\ s' = [s EXCEPT !.phase =
           CASE s.phase = "manifest" -> "resolve"
             [] s.phase = "resolve" -> "download"
             [] s.phase = "download" -> "compile"
             [] s.phase = "compile" -> "resources"
             [] OTHER -> "stage"]

RejectPreparation ==
    /\ s.phase \in {"manifest", "resolve", "download", "compile", "resources"}
    /\ s' = [s EXCEPT !.phase = "failed"]

Stage(path) ==
    /\ s.phase = "stage" /\ path \in Outputs \ s.staged
    /\ \/ s' = [s EXCEPT !.staged = @ \cup {path}]
       \/ s' = [s EXCEPT !.phase = "rollback", !.writeError = path]

Rollback ==
    /\ s.phase = "rollback"
    /\ \E errors \in SUBSET s.staged:
           s' = [s EXCEPT !.phase = "failed", !.staged = errors,
                          !.rollbackErrors = errors]

\* Publication abstracts the atomic visibility of both files and ownership records.
Commit ==
    /\ s.phase = "stage" /\ s.staged = Outputs
    /\ s' = [s EXCEPT !.phase = "installed", !.staged = {},
                   !.repo = [p \in Paths |->
                       CASE p \in Outputs -> New
                         [] op = "update" /\ p \in Stale -> Absent
                         [] OTHER -> before[p]]]

Remove(path) ==
    /\ s.phase = "removing" /\ path \in OwnedPaths \ s.attempted
    /\ \E confirm, denied \in BOOLEAN:
           LET permitted == Unchanged(path) \/ confirm IN
           s' = [s EXCEPT !.attempted = @ \cup {path},
                          !.confirmed = IF confirm THEN @ \cup {path} ELSE @,
                          !.deleteErrors = IF permitted /\ denied THEN @ \cup {path} ELSE @,
                          !.deleted = IF permitted /\ ~denied THEN @ \cup {path} ELSE @,
                          !.repo[path] = IF permitted /\ ~denied THEN Absent ELSE @]

ReportRemoval ==
    /\ s.phase = "removing" /\ s.attempted = OwnedPaths
    /\ s' = [s EXCEPT !.phase = "removed", !.summary = s.deleteErrors]

Next ==
    /\ UNCHANGED <<op, before>>
    /\ \/ Prepare \/ RejectPreparation \/ Rollback \/ Commit \/ ReportRemoval
       \/ \E path \in Paths: Stage(path) \/ Remove(path)

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

AtomicPublication ==
    op \in {"install", "update"} =>
        \A path \in Paths:
            s.repo[path] =
                CASE s.phase # "installed" -> before[path]
                  [] path \in Outputs -> New
                  [] op = "update" /\ path \in Stale -> Absent
                  [] OTHER -> before[path]

RollbackReports ==
    s.phase = "failed" =>
        /\ s.staged = s.rollbackErrors
        /\ s.rollbackErrors # {} => s.writeError \in Outputs
        /\ s.writeError # "none" => s.writeError \in Outputs \ s.rollbackErrors

SafeRemoval ==
    op = "remove" =>
        /\ s.deleted \subseteq (OwnedPaths \cap ({p \in Paths : Unchanged(p)} \cup s.confirmed))
        /\ s.deleteErrors \subseteq (s.attempted \ s.deleted)
        /\ \A p \in Paths: s.repo[p] = IF p \in s.deleted THEN Absent ELSE before[p]
        /\ s.phase = "removed" => s.attempted = OwnedPaths /\ s.summary = s.deleteErrors

Terminates == <>(s.phase \in {"failed", "installed", "removed"})

=============================================================================
