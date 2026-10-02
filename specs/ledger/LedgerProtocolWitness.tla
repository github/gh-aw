----------------------- MODULE LedgerProtocolWitness -----------------------
EXTENDS LedgerProtocol

\* TLC reports a counterexample when a target operation reaches trusted
\* persistence. Its JSON error trace is the source of the conformance vectors.
CONSTANT TargetOp
ASSUME TargetOp \in Ops

MissingAgentTarget ==
  ~(\E i \in 1..Len(history) :
      history[i].op = TargetOp /\ history[i].id \in AgentIds)
===========================================================================
