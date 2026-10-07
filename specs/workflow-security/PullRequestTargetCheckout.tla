------------------------- MODULE PullRequestTargetCheckout -------------------------
EXTENDS FiniteSets, TLC

CONSTANT Fault
ASSUME Fault \in {"none", "acknowledgment-bypasses-checkout", "fetch-bypasses-checkout"}

Triggers == {"pull_request_target", "other"}
Checkouts == {"checkout-disabled", "checkout-omitted", "base",
              "external-allowlisted-literal", "external-unlisted-literal",
              "expression", "wildcard", "pull-ref", "external-ref-omitted", "wiki"}
Fetches == {"none", "ordinary-ref", "pull-ref", "expression", "wildcard"}

VARIABLES trigger, strictMode, acknowledged, checkout, fetch
vars == <<trigger, strictMode, acknowledged, checkout, fetch>>

CheckoutNotExplicitlyDisabled == checkout # "checkout-disabled"
ExpectedTrustedCheckout ==
    checkout = "checkout-disabled" \/
    (CheckoutNotExplicitlyDisabled /\ fetch = "none" /\
      checkout \in {"base", "external-allowlisted-literal"})

ObservedTrustedCheckout ==
    ExpectedTrustedCheckout \/
    (Fault = "acknowledgment-bypasses-checkout" /\
      acknowledged /\ CheckoutNotExplicitlyDisabled) \/
    (Fault = "fetch-bypasses-checkout" /\ CheckoutNotExplicitlyDisabled /\
      fetch # "none" /\ checkout \in {"base", "external-allowlisted-literal"})

RiskWarning ==
    trigger = "pull_request_target" /\ strictMode /\ ~acknowledged

CheckoutRejected ==
    trigger = "pull_request_target" /\ CheckoutNotExplicitlyDisabled /\
      ~ObservedTrustedCheckout

CheckoutError == CheckoutRejected /\ strictMode
CheckoutWarning == CheckoutRejected /\ ~strictMode

Init ==
    /\ trigger \in Triggers
    /\ strictMode \in BOOLEAN
    /\ acknowledged \in BOOLEAN
    /\ checkout \in Checkouts
    /\ fetch \in Fetches

Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ trigger \in Triggers
    /\ strictMode \in BOOLEAN
    /\ acknowledged \in BOOLEAN
    /\ checkout \in Checkouts
    /\ fetch \in Fetches

NoUntrustedCheckoutAcceptance ==
    trigger = "pull_request_target" /\ CheckoutNotExplicitlyDisabled /\
      ~ExpectedTrustedCheckout => ~ObservedTrustedCheckout

ExactTrustedCheckoutClasses ==
    ExpectedTrustedCheckout =>
      checkout = "checkout-disabled" \/
      (fetch = "none" /\
        checkout \in {"base", "external-allowlisted-literal"})

WarningAcknowledgmentIsScoped ==
    RiskWarning =
      (trigger = "pull_request_target" /\ strictMode /\ ~acknowledged)

CheckoutDiagnosticsAreIndependentOfAcknowledgment ==
    CheckoutError =
      (trigger = "pull_request_target" /\ CheckoutNotExplicitlyDisabled /\
        ~ObservedTrustedCheckout /\ strictMode)
    /\ CheckoutWarning =
      (trigger = "pull_request_target" /\ CheckoutNotExplicitlyDisabled /\
        ~ObservedTrustedCheckout /\ ~strictMode)

OtherTriggersHaveNoDiagnostics ==
    trigger = "other" => ~RiskWarning /\ ~CheckoutError /\ ~CheckoutWarning

=============================================================================
