------------------------- MODULE CommentTriggeredPRCheckout -------------------------
EXTENDS FiniteSets, TLC

CONSTANT Fault
ASSUME Fault \in {"none", "event-bypass", "sender-type-bypass",
                  "author-bypass", "actor-bypass", "allowlist-bypass",
                  "repository-bypass"}

Events == {"issue_comment", "pull_request_review_comment",
           "workflow_dispatch", "other"}
CommentEvents == {"issue_comment", "pull_request_review_comment"}
Actors == {"my-app", "my-app[bot]", "other", "other[bot]"}
SenderTypes == {"Bot", "User"}
RepositoryRelations == {"same", "fork", "missing-head", "missing-base"}
Permissions == {"none", "read", "write", "maintain", "admin"}

VARIABLES event, actor, sender, commentAuthor, senderType, allowListed,
          repositoryRelation, permission
vars == <<event, actor, sender, commentAuthor, senderType, allowListed,
         repositoryRelation, permission>>

CanonicalBotIdentity(login) ==
    IF login = "my-app[bot]" THEN "my-app"
    ELSE IF login = "other[bot]" THEN "other"
    ELSE login

CommentEvent == event \in CommentEvents
ActorMatchesSender ==
    CanonicalBotIdentity(actor) = CanonicalBotIdentity(sender)
CommonBypassGuards ==
    CommentEvent /\ senderType = "Bot" /\ sender = commentAuthor /\
      ActorMatchesSender /\ allowListed /\ repositoryRelation = "same"

ExpectedBotBypass == CommonBypassGuards
ObservedBotBypass ==
    ExpectedBotBypass \/
    (Fault = "event-bypass" /\ ~CommentEvent /\ senderType = "Bot" /\
      sender = commentAuthor /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "sender-type-bypass" /\ CommentEvent /\ senderType # "Bot" /\
      sender = commentAuthor /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "author-bypass" /\ CommentEvent /\ senderType = "Bot" /\
      sender # commentAuthor /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "actor-bypass" /\ CommentEvent /\ senderType = "Bot" /\
      sender = commentAuthor /\ ~ActorMatchesSender /\ allowListed /\
      repositoryRelation = "same") \/
    (Fault = "allowlist-bypass" /\ CommentEvent /\ senderType = "Bot" /\
      sender = commentAuthor /\ ActorMatchesSender /\ ~allowListed /\
      repositoryRelation = "same") \/
    (Fault = "repository-bypass" /\ CommentEvent /\ senderType = "Bot" /\
      sender = commentAuthor /\ ActorMatchesSender /\ allowListed /\
      repositoryRelation # "same")

CollaboratorAuthorized ==
    permission \in {"write", "maintain", "admin"}
ExpectedCheckoutAuthorized == ExpectedBotBypass \/ CollaboratorAuthorized
ObservedCheckoutAuthorized == ObservedBotBypass \/ CollaboratorAuthorized

Init ==
    /\ event \in Events
    /\ actor \in Actors
    /\ sender \in Actors
    /\ commentAuthor \in Actors
    /\ senderType \in SenderTypes
    /\ allowListed \in BOOLEAN
    /\ repositoryRelation \in RepositoryRelations
    /\ permission \in Permissions

Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ event \in Events
    /\ actor \in Actors
    /\ sender \in Actors
    /\ commentAuthor \in Actors
    /\ senderType \in SenderTypes
    /\ allowListed \in BOOLEAN
    /\ repositoryRelation \in RepositoryRelations
    /\ permission \in Permissions

NoUnauthorizedBotBypass ==
    ObservedBotBypass => ExpectedBotBypass

CheckoutAuthorizationMatchesPolicy ==
    ObservedCheckoutAuthorized = ExpectedCheckoutAuthorized

=====================================================================================
