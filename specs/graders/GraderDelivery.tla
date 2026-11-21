--------------------------- MODULE GraderDelivery ---------------------------

VARIABLES registered, conclusion, usage, custom, combined, audit

Results == {"ok", "error", "unavailable"}
Absent == "absent"
NoUsage == [builtIn |-> Absent, customRegistered |-> FALSE]
NoCombined == [builtIn |-> Absent, customResult |-> Absent]
NoAudit == [builtIn |-> Absent, customResult |-> Absent, downloadError |-> FALSE]
Vars == <<registered, conclusion, usage, custom, combined, audit>>

Init ==
    /\ registered \in BOOLEAN
    /\ conclusion = "collecting"
    /\ usage = NoUsage
    /\ custom = "waiting"
    /\ combined = NoCombined
    /\ audit = NoAudit

Redact ==
    /\ conclusion = "collecting"
    /\ conclusion' = "redacted"
    /\ UNCHANGED <<registered, usage, custom, combined, audit>>

PublishUsage ==
    /\ conclusion = "redacted"
    /\ \E builtIn \in Results :
        usage' = [builtIn |-> builtIn, customRegistered |-> registered]
    /\ conclusion' = "published"
    /\ UNCHANGED <<registered, custom, combined, audit>>

FailConclusion ==
    /\ conclusion \in {"collecting", "redacted"}
    /\ conclusion' = "failed"
    /\ UNCHANGED <<registered, usage, custom, combined, audit>>

StartCustom ==
    /\ conclusion = "published"
    /\ usage.customRegistered
    /\ custom = "waiting"
    /\ custom' = "running"
    /\ UNCHANGED <<registered, conclusion, usage, combined, audit>>

PublishCombined ==
    /\ custom = "running"
    /\ \E result \in Results :
        combined' = [builtIn |-> usage.builtIn, customResult |-> result]
    /\ custom' = "published"
    /\ UNCHANGED <<registered, conclusion, usage, audit>>

FailCustom ==
    /\ custom = "running"
    /\ custom' = "failed"
    /\ UNCHANGED <<registered, conclusion, usage, combined, audit>>

ReadUsage ==
    /\ conclusion = "published"
    /\ audit = NoAudit
    /\ audit' = [builtIn |-> usage.builtIn,
                 customResult |-> IF usage.customRegistered /\ combined # NoCombined
                                  THEN combined.customResult ELSE Absent,
                 downloadError |-> FALSE]
    /\ UNCHANGED <<registered, conclusion, usage, custom, combined>>

AuditDownloadError ==
    /\ conclusion = "published"
    /\ usage.customRegistered
    /\ audit.customResult = Absent
    /\ audit' = [builtIn |-> usage.builtIn, customResult |-> Absent, downloadError |-> TRUE]
    /\ UNCHANGED <<registered, conclusion, usage, custom, combined>>

RefreshAudit ==
    /\ audit # NoAudit
    /\ combined # NoCombined
    /\ audit.customResult = Absent
    /\ audit' = [builtIn |-> audit.builtIn, customResult |-> combined.customResult,
                 downloadError |-> FALSE]
    /\ UNCHANGED <<registered, conclusion, usage, custom, combined>>

Next ==
    Redact \/ PublishUsage \/ FailConclusion \/ StartCustom \/ PublishCombined \/
    FailCustom \/ ReadUsage \/ AuditDownloadError \/ RefreshAudit
Spec == Init /\ [][Next]_Vars /\ WF_Vars(RefreshAudit)

CustomRequiresUsage ==
    /\ (usage # NoUsage) = (conclusion = "published")
    /\ usage # NoUsage => usage.customRegistered = registered
    /\ custom # "waiting" => conclusion = "published" /\ registered
    /\ (combined # NoCombined) = (custom = "published")

BuiltInIsPreserved ==
    /\ combined # NoCombined => combined.builtIn = usage.builtIn
    /\ audit # NoAudit => audit.builtIn = usage.builtIn
    /\ audit.customResult # Absent =>
        registered /\ combined # NoCombined /\ audit.customResult = combined.customResult
    /\ audit.downloadError => registered /\ audit.customResult = Absent

UsageUnmodified == [][usage # NoUsage => UNCHANGED usage]_Vars
AuditRefreshes ==
    (audit # NoAudit /\ combined # NoCombined) ~>
        (audit.customResult # Absent /\ ~audit.downloadError)
=============================================================================
