-------------------------- MODULE HarnessIdentity --------------------------
EXTENDS FiniteSets, TLC

CONSTANTS FrontmatterHashes, BodyHashes, Revisions, Workers, None
ASSUME /\ FrontmatterHashes # {}
       /\ BodyHashes # {}
       /\ Revisions # {}
       /\ Workers # {}
       /\ None \notin Revisions

VARIABLES frontmatterHash, bodyHash, sourceRevision, usedRevisions,
          assignmentRevision
vars == <<frontmatterHash, bodyHash, sourceRevision, usedRevisions,
          assignmentRevision>>

HarnessVersion == frontmatterHash

Init ==
    /\ frontmatterHash \in FrontmatterHashes
    /\ bodyHash \in BodyHashes
    /\ sourceRevision \in Revisions
    /\ usedRevisions = {sourceRevision}
    /\ assignmentRevision = [w \in Workers |-> None]

BodyEdit ==
    /\ bodyHash' \in BodyHashes \ {bodyHash}
    /\ sourceRevision' \in Revisions \ usedRevisions
    /\ usedRevisions' = usedRevisions \cup {sourceRevision'}
    /\ UNCHANGED <<frontmatterHash, assignmentRevision>>

FrontmatterEdit ==
    /\ frontmatterHash' \in FrontmatterHashes \ {frontmatterHash}
    /\ sourceRevision' \in Revisions \ usedRevisions
    /\ usedRevisions' = usedRevisions \cup {sourceRevision'}
    /\ UNCHANGED <<bodyHash, assignmentRevision>>

Dispatch(w) ==
    /\ assignmentRevision[w] = None
    /\ assignmentRevision' = [assignmentRevision EXCEPT ![w] = sourceRevision]
    /\ UNCHANGED <<frontmatterHash, bodyHash, sourceRevision, usedRevisions>>

Next == BodyEdit \/ FrontmatterEdit \/ \E w \in Workers : Dispatch(w)
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ frontmatterHash \in FrontmatterHashes
    /\ bodyHash \in BodyHashes
    /\ sourceRevision \in Revisions
    /\ usedRevisions \subseteq Revisions
    /\ sourceRevision \in usedRevisions
    /\ assignmentRevision \in [Workers -> (Revisions \cup {None})]

EvaluationIdentityIsFrontmatter == HarnessVersion = frontmatterHash

BodyEditPreservesIdentity ==
    [][Next =>
        (bodyHash' # bodyHash =>
            frontmatterHash' = frontmatterHash /\ HarnessVersion' = HarnessVersion)]_vars

FrontmatterEditUpdatesIdentity ==
    [][Next =>
        (frontmatterHash' # frontmatterHash =>
            HarnessVersion' = frontmatterHash')]_vars

DispatchCapturesCurrentRevision ==
    [][Next =>
        (\A w \in Workers :
            assignmentRevision[w] = None /\ assignmentRevision'[w] # None =>
                assignmentRevision'[w] = sourceRevision)]_vars

DispatchBindingsAreImmutable ==
    [][Next =>
        (\A w \in Workers :
            assignmentRevision[w] # None =>
                assignmentRevision'[w] = assignmentRevision[w])]_vars
=============================================================================
