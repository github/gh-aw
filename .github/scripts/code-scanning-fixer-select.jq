def severity_rank:
  (.rule.security_severity_level // "" | tostring | ascii_downcase) as $security
  | if $security == "critical" then 5
    elif $security == "high" then 4
    elif $security == "medium" then 3
    elif $security == "low" then 2
    else (.rule.severity // .severity // "" | tostring | ascii_downcase) as $fallback
      | if $fallback == "error" then 5
        elif $fallback == "warning" then 4
        elif $fallback == "note" then 3
        else 0
        end
    end;
def ai_scan:
  ((.tool.name // "" | ascii_downcase) == "ai scan")
  or (.rule.id // "" | startswith("ai-scan/"));
def cluster_key:
  [.tool.name // "", .rule.id // "",
    (.most_recent_instance.location.path // "" | split("/") | .[0:-1] | join("/"))]
  | tojson | @base64;
def timestamp($record):
  try ($record.started_at | fromdateiso8601) catch null;
def valid_step($step):
  (($step | type) == "string")
  and (["alert_selected", "alert_details_retrieved", "vulnerability_analyzed", "fix_generated_and_reviewed", "patch_preflight_passed"] | any(. == $step));
def normalize_step($record):
  if ($record | has("last_successful_step") | not) then "alert_selected"
  elif valid_step($record.last_successful_step) then $record.last_successful_step
  else "unknown"
  end;
def invalid_step($record):
  ($record | has("last_successful_step"))
  and (valid_step($record.last_successful_step) | not);
def stale($now; $record):
  timestamp($record) as $started
  | ($started == null or ($now - $started) > 1800);
def pending($now; $record):
  (try ($record.recorded_at | fromdateiso8601) catch null) as $recorded
  | ($record.outcome == "pr_pending" and $recorded != null and ($now - $recorded) < 3600);
def stalled_report_count($alert_number; $fingerprint):
  [($cached[0] // [])[]
    | select(
        (.alert_number | tostring) == ($alert_number | tostring)
        and .outcome == "stalled_reported"
        and .fingerprint == $fingerprint
      )]
  | length;
def covers_alert($alert; $candidate):
  ((.title // "") + "\n" + (.body // "")) as $body
  | ($alert.most_recent_instance.location.path // "") as $path
  | ("https://github.com/" + $repo + "/security/code-scanning/" + ($alert.number | tostring)) as $url
  | ((.files // [] | index($path)) != null)
    or ($body | split($url) | .[1:] | any(test("^[^0-9]|^$")))
    or ($body | split("\n") | any(
      test("(?i)\\b(alerts?|ai[ -]scan|code[ -]scanning)\\b")
      and test("#" + ($alert.number | tostring) + "([^0-9]|$)")
    ))
    or (((.title // "") | startswith("[code-scanning-fix]"))
      and ($body | contains("Code scanning cluster: \($candidate.cluster_key)")));
($cached[0] // [] | reduce .[] as $record ({}; .[($record.alert_number | tostring)] = $record)) as $latest
| [$latest[] | select(pending($now; .)) | .cluster_key // empty] as $pending_clusters
| ($latest | to_entries
  | map(select(.value.outcome == "in_progress" and stale($now; .value)))
  | sort_by(.key) | .[0] // null) as $stalled_record
| ($stalled_record.key // null) as $stalled
| ($alerts[0] // [] | sort_by([(if ai_scan then 0 else 1 end), -severity_rank, (.number // 0)])) as $ranked
| ([
    $ranked[] | . as $alert
    | ($latest[($alert.number | tostring)] // {}) as $record
    | select(
        $record.fixed_at == null
        and (pending($now; $record) | not)
        and (($record.outcome // "") != "in_progress"
          or (stale($now; $record) and ($alert.number | tostring) != $stalled))
      )
    | {
        number: $alert.number,
        severity: ($alert.rule.security_severity_level // $alert.rule.severity // $alert.severity // "unknown"),
        rule_id: ($alert.rule.id // ""),
        tool: ($alert.tool.name // ""),
        ai_scan: ($alert | ai_scan),
        cluster_key: ($alert | cluster_key),
        fingerprint: ([
          $alert.rule.id // "",
          $alert.most_recent_instance.location.path // "",
          ($alert.most_recent_instance.location.start_line // 0),
          $alert.most_recent_instance.message.text // ""
        ] | @json)
      } as $candidate
    | select(stalled_report_count($alert.number; $candidate.fingerprint) < 2)
    | select(
        ($record.outcome // "") != "patch_too_large"
        or $record.fingerprint != $candidate.fingerprint
      )
    | select($pending_clusters | index($candidate.cluster_key) == null)
    | $candidate + {covered_by: [($pulls[0] // [])[] | select(covers_alert($alert; $candidate)) | .number]}
  ]) as $considered
| [$considered[] | select(.covered_by | length == 0) | del(.covered_by)] as $eligible
| ($eligible[0].cluster_key // null) as $cluster
| {
    status: "ok",
    cluster_key: $cluster,
    covered_alerts: [$considered[] | select(.covered_by | length > 0) | {number, pull_numbers: .covered_by}],
    stalled_alert_number: $stalled,
    stalled_record: (if $stalled_record == null then null else
      ($stalled_record.value) as $record
      | timestamp($record) as $started
      | {
          alert_number: $record.alert_number,
          fingerprint: $record.fingerprint,
          started_at: (if $started == null then null else $record.started_at end),
          timestamp_invalid: ($started == null),
          last_successful_step: normalize_step($record),
          last_successful_step_invalid: invalid_step($record),
          stall_reason: (if $started == null then
            "The in-progress checkpoint has a missing or invalid started_at timestamp, so it was treated as stale; no completion outcome was recorded."
          else
            "No completion outcome was recorded within 30 minutes of the in-progress checkpoint; the run may have timed out or stalled."
          end)
        }
      end),
    candidates: ([$eligible[] | select(.cluster_key == $cluster)] | .[0:5])
  }
