----------------------------- MODULE SafeUpdate -----------------------------
EXTENDS Naturals

CONSTANT MaxCompiles
ASSUME MaxCompiles \in Nat \ {0}

Absent == "absent"
Legacy == "legacy"
Empty == "empty"
Manifests == {Empty, "prior", "candidate"}
Locks == Manifests \cup {Absent, Legacy}

VARIABLES phase, compiles, head, disk, cache, strict, approve,
          proposed, selected, firstTrusted, warning
vars == <<phase, compiles, head, disk, cache, strict, approve,
          proposed, selected, firstTrusted, warning>>

Init ==
  /\ phase = "ready"
  /\ compiles = 0
  /\ cache \in Manifests \cup {Absent}
  /\ firstTrusted = cache
  /\ head \in Locks
  /\ disk \in Locks
  /\ strict = TRUE
  /\ approve = FALSE
  /\ proposed = Empty
  /\ selected = Absent
  /\ warning = FALSE

Start(s, a, manifest) ==
  /\ phase = "ready"
  /\ compiles < MaxCompiles
  /\ phase' = "resolving"
  /\ strict' = s
  /\ approve' = a
  /\ proposed' = manifest
  /\ selected' = Absent
  /\ warning' = FALSE
  /\ UNCHANGED <<compiles, head, disk, cache, firstTrusted>>

Active == strict /\ ~approve

Baseline ==
  IF cache # Absent THEN cache
  ELSE IF head # Absent THEN head
  ELSE IF disk # Absent THEN disk
  ELSE Empty

Resolve ==
  /\ phase = "resolving"
  /\ phase' = "comparing"
  /\ selected' = IF Active THEN Baseline ELSE Absent
  /\ cache' = IF selected' \in Manifests THEN selected' ELSE cache
  /\ firstTrusted' =
       IF firstTrusted = Absent /\ selected' \in Manifests
       THEN selected' ELSE firstTrusted
  /\ UNCHANGED <<compiles, head, disk, strict, approve,
                 proposed, warning>>

\* Only the candidate represents one restricted addition to a prior baseline.
RestrictedChange(old, new) == new = "candidate" /\ old # "candidate"
Compare ==
  /\ phase = "comparing"
  /\ phase' = "writing"
  /\ warning' = (selected \in Manifests /\ RestrictedChange(selected, proposed))
  /\ UNCHANGED <<compiles, head, disk, cache, strict, approve,
                 proposed, selected, firstTrusted>>

Write ==
  /\ phase = "writing"
  /\ phase' = "ready"
  /\ compiles' = compiles + 1
  /\ disk' = proposed
  /\ UNCHANGED <<head, cache, strict, approve, proposed,
                 selected, firstTrusted, warning>>

Next ==
  \/ \E s, a \in BOOLEAN, m \in Manifests: Start(s, a, m)
  \/ Resolve
  \/ Compare
  \/ Write
  \/ phase = "ready" /\ compiles = MaxCompiles /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

BaselineHandoff ==
  /\ (selected \in Manifests => selected = firstTrusted)
  /\ cache = firstTrusted

ResolutionUsesPriorState ==
  (phase = "resolving" /\ phase' = "comparing") =>
    (CASE ~Active -> selected' = Absent
       [] cache \in Manifests -> selected' = cache
       [] head # Absent -> selected' = head
       [] disk # Absent -> selected' = disk
       [] OTHER -> selected' = Empty)

StableTrustedBaseline == firstTrusted # Absent => cache' = cache
BaselineProtocol == [][ResolutionUsesPriorState /\ StableTrustedBaseline]_vars

WarningContract ==
  /\ (warning => Active /\ selected \in Manifests)
  /\ (phase = "writing" \/ (phase = "ready" /\ compiles > 0) =>
       warning = (Active /\ selected \in Manifests /\
                  RestrictedChange(selected, proposed)))
  /\ (phase = "ready" /\ compiles > 0 => disk = proposed /\ disk \in Manifests)
=============================================================================
