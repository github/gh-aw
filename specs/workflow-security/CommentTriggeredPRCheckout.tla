------------------------- MODULE CommentTriggeredPRCheckout -------------------------
EXTENDS FiniteSets, TLC

CONSTANT Fault
ASSUME Fault \in {"none", "event-bypass", "sender-type-bypass",
                  "author-bypass", "actor-bypass", "allowlist-bypass",
                  "repository-bypass", "branch-event-bypass",
                  "branch-action-bypass", "branch-sender-type-bypass",
                  "branch-sender-bypass", "branch-repository-bypass",
                  "branch-edited-body-bypass",
                  "branch-edited-allowlist-bypass"}

Events == {"issue_comment", "pull_request_review_comment",
           "pull_request_review", "pull_request", "pull_request_target",
           "workflow_dispatch", "other"}
CommentEvents == {"issue_comment", "pull_request_review_comment"}
ReviewEvents == {"pull_request_review"}
CommentOrReviewEvents == CommentEvents \cup ReviewEvents
BranchEvents == {"pull_request", "pull_request_target"}
BranchActions == {"opened", "synchronize", "ready_for_review",
                  "reopened", "edited", "labeled", "other"}
Actors == {"my-app", "my-app[bot]", "other", "other[bot]"}
SenderTypes == {"Bot", "User"}
RepositoryRelations == {"same", "fork", "missing-head", "missing-base"}
Permissions == {"none", "read", "write", "maintain", "admin"}

VARIABLES event, action, actor, sender, commentAuthor, reviewAuthor, senderType,
         allowListed, bodyOnlyEdit, repositoryRelation, permission
vars == <<event, action, actor, sender, commentAuthor, reviewAuthor, senderType,
         allowListed, bodyOnlyEdit, repositoryRelation, permission>>

CanonicalBotIdentity(login) ==
    IF login = "my-app[bot]" THEN "my-app"
    ELSE IF login = "other[bot]" THEN "other"
    ELSE login

CommentEvent == event \in CommentEvents
CommentOrReviewEvent == event \in CommentOrReviewEvents
BranchEvent == event \in BranchEvents
SupportedBranchAction == action \in {"opened", "synchronize",
                                      "ready_for_review", "reopened"} \/
                        (action = "edited" /\ allowListed /\ bodyOnlyEdit)
AuthorMatchesSender ==
    IF CommentEvent THEN sender = commentAuthor
    ELSE IF event \in ReviewEvents THEN sender = reviewAuthor
    ELSE FALSE
ActorMatchesSender ==
    CanonicalBotIdentity(actor) = CanonicalBotIdentity(sender)
CommonBypassGuards ==
    CommentOrReviewEvent /\ senderType = "Bot" /\ AuthorMatchesSender /\
      ActorMatchesSender /\ allowListed /\ repositoryRelation = "same"
BranchBotBypass ==
    BranchEvent /\ SupportedBranchAction /\ senderType = "Bot" /\
      sender = actor /\ repositoryRelation = "same"

ExpectedBotBypass == CommonBypassGuards \/ BranchBotBypass
ObservedBotBypass ==
    ExpectedBotBypass \/
    (Fault = "event-bypass" /\ ~CommentOrReviewEvent /\ senderType = "Bot" /\
      sender = commentAuthor /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "sender-type-bypass" /\ CommentOrReviewEvent /\ senderType # "Bot" /\
      AuthorMatchesSender /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "author-bypass" /\ CommentOrReviewEvent /\ senderType = "Bot" /\
      ~AuthorMatchesSender /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "actor-bypass" /\ CommentOrReviewEvent /\ senderType = "Bot" /\
      AuthorMatchesSender /\ ~ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "allowlist-bypass" /\ CommentOrReviewEvent /\ senderType = "Bot" /\
      AuthorMatchesSender /\ ActorMatchesSender /\ ~allowListed /\
      repositoryRelation = "same") \/
    (Fault = "repository-bypass" /\ CommentOrReviewEvent /\ senderType = "Bot" /\
      AuthorMatchesSender /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation # "same") \/
    (Fault = "branch-event-bypass" /\ ~BranchEvent /\ SupportedBranchAction /\
      senderType = "Bot" /\ sender = actor /\ repositoryRelation = "same") \/
    (Fault = "branch-action-bypass" /\ BranchEvent /\ ~SupportedBranchAction /\
      senderType = "Bot" /\ sender = actor /\ repositoryRelation = "same") \/
    (Fault = "branch-sender-type-bypass" /\ BranchEvent /\ SupportedBranchAction /\
      senderType # "Bot" /\ sender = actor /\ repositoryRelation = "same") \/
    (Fault = "branch-sender-bypass" /\ BranchEvent /\ SupportedBranchAction /\
      senderType = "Bot" /\ sender # actor /\ repositoryRelation = "same") \/
    (Fault = "branch-repository-bypass" /\ BranchEvent /\ SupportedBranchAction /\
      senderType = "Bot" /\ sender = actor /\ repositoryRelation # "same") \/
    (Fault = "branch-edited-body-bypass" /\ BranchEvent /\ action = "edited" /\
      allowListed /\ ~bodyOnlyEdit /\ senderType = "Bot" /\ sender = actor /\
      repositoryRelation = "same") \/
    (Fault = "branch-edited-allowlist-bypass" /\ BranchEvent /\ action = "edited" /\
      bodyOnlyEdit /\ ~allowListed /\ senderType = "Bot" /\ sender = actor /\
      repositoryRelation = "same")

CollaboratorAuthorized ==
    permission \in {"write", "maintain", "admin"}
ExpectedCheckoutAuthorized == ExpectedBotBypass \/ CollaboratorAuthorized
ObservedCheckoutAuthorized == ObservedBotBypass \/ CollaboratorAuthorized

Init ==
    /\ event \in Events
    /\ action \in BranchActions
    /\ actor \in Actors
    /\ sender \in Actors
    /\ commentAuthor \in Actors
    /\ reviewAuthor \in Actors
    /\ senderType \in SenderTypes
    /\ allowListed \in BOOLEAN
    /\ bodyOnlyEdit \in BOOLEAN
    /\ repositoryRelation \in RepositoryRelations
    /\ permission \in Permissions

Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ event \in Events
    /\ action \in BranchActions
    /\ actor \in Actors
    /\ sender \in Actors
    /\ commentAuthor \in Actors
    /\ reviewAuthor \in Actors
    /\ senderType \in SenderTypes
    /\ allowListed \in BOOLEAN
    /\ bodyOnlyEdit \in BOOLEAN
    /\ repositoryRelation \in RepositoryRelations
    /\ permission \in Permissions

NoUnauthorizedBotBypass ==
    ObservedBotBypass => ExpectedBotBypass

CheckoutAuthorizationMatchesPolicy ==
    ObservedCheckoutAuthorized = ExpectedCheckoutAuthorized

=====================================================================================
