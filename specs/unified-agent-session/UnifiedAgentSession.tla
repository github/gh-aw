------------------------- MODULE UnifiedAgentSession -------------------------
EXTENDS Naturals, Sequences

CONSTANT MaxRecords
ASSUME MaxRecords \in Nat \ {0}

VARIABLES phase, source, cursor, collected, published, consumed
vars == <<phase, source, cursor, collected, published, consumed>>

NoArtifact == "unpublished"
ToolIds == {"a", "b"}
Record(kind, id) == [kind |-> kind, id |-> id]
Malformed == Record("malformed", "none")
Records == {Malformed, Record("session-error", "none")}
           \cup {Record(k, i): k \in {"start", "complete"}, i \in ToolIds}

Prefix(s, n) == SubSeq(s, 1, n)
Supported(r) == r # Malformed

Init ==
  /\ phase = "observing"
  /\ source = <<>>
  /\ cursor = 0
  /\ collected = <<>>
  /\ published = NoArtifact
  /\ consumed = NoArtifact

Observe(r) ==
  /\ phase = "observing"
  /\ Len(source) < MaxRecords
  /\ source' = Append(source, r)
  /\ UNCHANGED <<phase, cursor, collected, published, consumed>>

\* This transition abstracts completion of downstream downloads and handlers.
BeginCollection ==
  /\ phase = "observing"
  /\ phase' = "collecting"
  /\ UNCHANGED <<source, cursor, collected, published, consumed>>

Collect ==
  /\ phase = "collecting"
  /\ cursor < Len(source)
  /\ cursor' = cursor + 1
  /\ LET r == source[cursor + 1]
     IN collected' = IF Supported(r) THEN Append(collected, r) ELSE collected
  /\ UNCHANGED <<phase, source, published, consumed>>

Publish ==
  /\ phase = "collecting"
  /\ cursor = Len(source)
  /\ phase' = "published"
  /\ published' = collected
  /\ UNCHANGED <<source, cursor, collected, consumed>>

Cleanup ==
  /\ phase = "published"
  /\ phase' = "cleaned"
  /\ UNCHANGED <<source, cursor, collected, published, consumed>>

Read ==
  /\ phase \in {"published", "cleaned"}
  /\ consumed = NoArtifact
  /\ consumed' = published
  /\ UNCHANGED <<phase, source, cursor, collected, published>>

Next ==
  \/ \E r \in Records: Observe(r)
  \/ BeginCollection
  \/ Collect
  \/ Publish
  \/ Cleanup
  \/ Read
  \/ phase = "cleaned" /\ consumed # NoArtifact /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

ObservedRecordsOnly ==
  /\ cursor \in 0..Len(source)
  /\ collected = SelectSeq(Prefix(source, cursor), Supported)

ArtifactLifecycle ==
  /\ (phase \in {"observing", "collecting"} => published = NoArtifact)
  /\ (phase \in {"published", "cleaned"} =>
        /\ cursor = Len(source)
        /\ published = SelectSeq(source, Supported))
  /\ (consumed # NoArtifact =>
        consumed = published /\ phase \in {"published", "cleaned"})
=============================================================================
