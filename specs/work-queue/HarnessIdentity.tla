-------------------------- MODULE HarnessIdentity --------------------------
EXTENDS FiniteSets, TLC

CONSTANTS FrontmatterHashes, BodyHashes, FreshnessHashes, WorkflowIDs,
          IdentityModes, Revisions, Workers, None
ASSUME /\ FrontmatterHashes # {}
       /\ BodyHashes # {}
       /\ FreshnessHashes # {}
       /\ WorkflowIDs # {}
       /\ IdentityModes = {"full", "frontmatter", "workflow"}
       /\ Revisions # {}
       /\ Workers # {}
       /\ None \notin Revisions

VARIABLES pureFrontmatterHash, bodyHash, compilerFreshnessHash, workflowID,
          identityMode, sourceRevision, usedRevisions, assignmentRevision
vars == <<pureFrontmatterHash, bodyHash, compilerFreshnessHash, workflowID,
          identityMode, sourceRevision, usedRevisions, assignmentRevision>>

HarnessVersion ==
    CASE identityMode = "full" -> <<compilerFreshnessHash, bodyHash>>
      [] identityMode = "frontmatter" -> pureFrontmatterHash
      [] identityMode = "workflow" -> workflowID

Init ==
    /\ pureFrontmatterHash \in FrontmatterHashes
    /\ bodyHash \in BodyHashes
    /\ compilerFreshnessHash \in FreshnessHashes
    /\ workflowID \in WorkflowIDs
    /\ identityMode \in IdentityModes
    /\ sourceRevision \in Revisions
    /\ usedRevisions = {sourceRevision}
    /\ assignmentRevision = [w \in Workers |-> None]

BodyEdit ==
    /\ bodyHash' \in BodyHashes \ {bodyHash}
    /\ compilerFreshnessHash' \in FreshnessHashes \ {compilerFreshnessHash}
    /\ sourceRevision' \in Revisions \ usedRevisions
    /\ usedRevisions' = usedRevisions \cup {sourceRevision'}
    /\ UNCHANGED <<pureFrontmatterHash, workflowID, identityMode, assignmentRevision>>

FrontmatterEdit ==
    /\ pureFrontmatterHash' \in FrontmatterHashes \ {pureFrontmatterHash}
    /\ compilerFreshnessHash' \in FreshnessHashes \ {compilerFreshnessHash}
    /\ sourceRevision' \in Revisions \ usedRevisions
    /\ usedRevisions' = usedRevisions \cup {sourceRevision'}
    /\ UNCHANGED <<bodyHash, workflowID, identityMode, assignmentRevision>>

Dispatch(w) ==
    /\ assignmentRevision[w] = None
    /\ assignmentRevision' = [assignmentRevision EXCEPT ![w] = sourceRevision]
    /\ UNCHANGED <<pureFrontmatterHash, bodyHash, compilerFreshnessHash,
                    workflowID, identityMode, sourceRevision, usedRevisions>>

Next == BodyEdit \/ FrontmatterEdit \/ \E w \in Workers : Dispatch(w)
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ pureFrontmatterHash \in FrontmatterHashes
    /\ bodyHash \in BodyHashes
    /\ compilerFreshnessHash \in FreshnessHashes
    /\ workflowID \in WorkflowIDs
    /\ identityMode \in IdentityModes
    /\ sourceRevision \in Revisions
    /\ usedRevisions \subseteq Revisions
    /\ sourceRevision \in usedRevisions
    /\ assignmentRevision \in [Workers -> (Revisions \cup {None})]

EvaluationIdentityUsesConfiguredMode ==
    /\ (identityMode = "full" =>
            HarnessVersion = <<compilerFreshnessHash, bodyHash>>)
    /\ (identityMode = "frontmatter" =>
            HarnessVersion = pureFrontmatterHash)
    /\ (identityMode = "workflow" => HarnessVersion = workflowID)

BodyEditIdentityBehavior ==
    [][Next =>
        (bodyHash' # bodyHash =>
            /\ (identityMode = "full" => HarnessVersion' # HarnessVersion)
            /\ (identityMode = "frontmatter" => HarnessVersion' = HarnessVersion)
            /\ (identityMode = "workflow" => HarnessVersion' = HarnessVersion))]_vars

FrontmatterEditIdentityBehavior ==
    [][Next =>
        (pureFrontmatterHash' # pureFrontmatterHash =>
            /\ (identityMode = "full" => HarnessVersion' # HarnessVersion)
            /\ (identityMode = "frontmatter" => HarnessVersion' # HarnessVersion)
            /\ (identityMode = "workflow" => HarnessVersion' = HarnessVersion))]_vars

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
