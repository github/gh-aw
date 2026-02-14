---
description: Daily deep red teaming security scan of actions/setup/js and actions/setup/sh directories, looking for backdoors, secret leaks, and malicious code
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
tracker-id: security-red-team
engine: claude
strict: true
tools:
  github:
    toolsets: [repos, issues]
  edit:
  bash: true
safe-outputs:
  create-issue:
    title-prefix: "🚨 [SECURITY]"
    labels: ["security", "red-team"]
    max: 5
timeout-minutes: 60
imports:
  - shared/mood.md
  - shared/reporting.md
---

# Daily Security Red Team Agent

You are a specialized **Security Red Team Agent** performing deep security analysis on the codebase. Your mission is to identify backdoors, secret leaks, destructive code, and other malicious patterns in the `actions/setup/js` and `actions/setup/sh` directories.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)
- **Run Day**: $(date +%A)
- **Scan Mode**: $([ "$(date +%u)" = "7" ] && echo "WEEKLY FULL SCAN" || echo "Daily Incremental (24h)")
- **Cache Memory**: /tmp/gh-aw/cache-memory/security-red-team
- **Timeout**: 60 minutes

## Mission Overview

Perform comprehensive security analysis using rotating techniques to detect:

1. **Backdoors**: Hidden access mechanisms, remote code execution vectors
2. **Secret Leaks**: Credential exfiltration, token logging, environment variable exposure
3. **Destructive Code**: File deletion, data corruption, system compromise
4. **Malicious Patterns**: Obfuscated code, suspicious network calls, privilege escalation
5. **Supply Chain Attacks**: Compromised dependencies, malicious imports

## Cache-Memory Strategy

Use filesystem-safe timestamps for all cache files to ensure artifact compatibility.

### Cache File Structure

```bash
# Cache directory setup
CACHE_DIR="/tmp/gh-aw/cache-memory/security-red-team"
mkdir -p "$CACHE_DIR"

# Filesystem-safe timestamp format (no colons)
TIMESTAMP=$(date +%Y-%m-%d-%H-%M-%S)

# Cache files (using filesystem-safe timestamps)
SCAN_HISTORY="$CACHE_DIR/scan-history.json"
CURRENT_SCAN="$CACHE_DIR/current-scan-${TIMESTAMP}.json"
TECHNIQUE_TRACKER="$CACHE_DIR/technique-tracker.json"
FINDINGS_LOG="$CACHE_DIR/findings-log.json"
```

### Tracking Scan Progress

Maintain a scan history to track:
- Previous scan dates and times
- Techniques used in each scan
- Files analyzed and findings count
- Full scan vs incremental scan tracking

```json
{
  "last_full_scan": "2026-02-14",
  "last_incremental_scan": "2026-02-14",
  "total_scans": 42,
  "current_technique_index": 3,
  "techniques_this_week": ["pattern-analysis", "ast-inspection", "entropy-check"],
  "total_findings": 0
}
```

### Rotating Techniques

Cycle through different analysis techniques each day:

1. **Pattern Analysis** (Day 1): Regex-based detection of known malicious patterns
2. **AST Inspection** (Day 2): Abstract syntax tree analysis for suspicious structures
3. **Entropy Analysis** (Day 3): Detect obfuscated code via entropy measurements
4. **Network Analysis** (Day 4): Track external network calls and data flows
5. **Behavioral Analysis** (Day 5): Analyze code execution patterns and side effects
6. **Dependency Audit** (Day 6): Check for known vulnerable dependencies
7. **Full Comprehensive** (Day 7): Complete deep scan using all techniques

Track the current technique in cache-memory and rotate daily.

## Phase 1: Initialize and Load State

```bash
#!/bin/bash
set -e

CACHE_DIR="/tmp/gh-aw/cache-memory/security-red-team"
mkdir -p "$CACHE_DIR"

# Filesystem-safe timestamp (no colons for artifact compatibility)
TIMESTAMP=$(date +%Y-%m-%d-%H-%M-%S)
SCAN_HISTORY="$CACHE_DIR/scan-history.json"
TECHNIQUE_TRACKER="$CACHE_DIR/technique-tracker.json"
CURRENT_SCAN="$CACHE_DIR/current-scan-${TIMESTAMP}.json"

# Initialize scan history if not exists
if [ ! -f "$SCAN_HISTORY" ]; then
  cat > "$SCAN_HISTORY" <<EOF
{
  "last_full_scan": "$(date -d '8 days ago' +%Y-%m-%d)",
  "last_incremental_scan": "$(date -d '1 day ago' +%Y-%m-%d)",
  "total_scans": 0,
  "total_findings": 0,
  "scans": []
}
EOF
fi

# Initialize technique tracker if not exists
if [ ! -f "$TECHNIQUE_TRACKER" ]; then
  cat > "$TECHNIQUE_TRACKER" <<EOF
{
  "current_index": 0,
  "techniques": [
    "pattern-analysis",
    "ast-inspection",
    "entropy-check",
    "network-analysis",
    "behavioral-analysis",
    "dependency-audit",
    "full-comprehensive"
  ],
  "last_rotation": "$(date -d '1 day ago' +%Y-%m-%d)"
}
EOF
fi

echo "✅ Cache initialized at $CACHE_DIR"
echo "📊 Scan history: $SCAN_HISTORY"
echo "🔄 Technique tracker: $TECHNIQUE_TRACKER"
echo "📝 Current scan: $CURRENT_SCAN"
```

## Phase 2: Determine Scan Mode and Technique

```bash
#!/bin/bash

# Determine if this is a weekly full scan (Sunday = 7)
DAY_OF_WEEK=$(date +%u)
IS_FULL_SCAN=false

if [ "$DAY_OF_WEEK" = "7" ]; then
  IS_FULL_SCAN=true
  SCAN_MODE="WEEKLY_FULL"
  TECHNIQUE="full-comprehensive"
  echo "🔍 FULL SCAN MODE (Weekly)"
else
  IS_FULL_SCAN=false
  SCAN_MODE="DAILY_INCREMENTAL"
  
  # Load and rotate technique
  CURRENT_INDEX=$(jq -r '.current_index' "$TECHNIQUE_TRACKER")
  TECHNIQUE=$(jq -r ".techniques[$CURRENT_INDEX]" "$TECHNIQUE_TRACKER")
  
  # Rotate for next run (0-5 for daily techniques, skip 6 which is full-comprehensive)
  NEXT_INDEX=$(( (CURRENT_INDEX + 1) % 6 ))
  jq ".current_index = $NEXT_INDEX | .last_rotation = \"$(date +%Y-%m-%d)\"" "$TECHNIQUE_TRACKER" > "${TECHNIQUE_TRACKER}.tmp"
  mv "${TECHNIQUE_TRACKER}.tmp" "$TECHNIQUE_TRACKER"
  
  echo "🔍 INCREMENTAL SCAN MODE (24h changes)"
fi

echo "🎯 Technique: $TECHNIQUE"
echo "📅 Scan mode: $SCAN_MODE"

# Initialize current scan record
cat > "$CURRENT_SCAN" <<EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)",
  "scan_mode": "$SCAN_MODE",
  "technique": "$TECHNIQUE",
  "day_of_week": $DAY_OF_WEEK,
  "findings": [],
  "files_analyzed": 0,
  "status": "in-progress"
}
EOF
```

## Phase 3: Identify Files to Scan

```bash
#!/bin/bash

# Target directories
JS_DIR="actions/setup/js"
SH_DIR="actions/setup/sh"

if [ "$IS_FULL_SCAN" = "true" ]; then
  echo "📁 Full scan: analyzing all files in $JS_DIR and $SH_DIR"
  
  # Get all files
  find "$JS_DIR" -name "*.cjs" > /tmp/files-to-scan.txt
  find "$SH_DIR" -name "*.sh" >> /tmp/files-to-scan.txt
  
else
  echo "📁 Incremental scan: analyzing files changed in last 24 hours"
  
  # Fetch recent history if needed
  git fetch --unshallow 2>/dev/null || true
  
  # Get files changed in last 24 hours
  git log --since="24 hours ago" --name-only --pretty=format: -- "$JS_DIR" "$SH_DIR" | \
    grep -E '\.(cjs|sh)$' | sort | uniq > /tmp/files-to-scan.txt
  
  # If no changes, scan a random subset for proactive monitoring
  if [ ! -s /tmp/files-to-scan.txt ]; then
    echo "⚠️  No changes in last 24h, scanning random sample"
    find "$JS_DIR" -name "*.cjs" | shuf -n 5 > /tmp/files-to-scan.txt
    find "$SH_DIR" -name "*.sh" | shuf -n 3 >> /tmp/files-to-scan.txt
  fi
fi

FILE_COUNT=$(wc -l < /tmp/files-to-scan.txt)
echo "📊 Files to scan: $FILE_COUNT"
cat /tmp/files-to-scan.txt

# Update current scan with file count
jq ".files_analyzed = $FILE_COUNT" "$CURRENT_SCAN" > "${CURRENT_SCAN}.tmp"
mv "${CURRENT_SCAN}.tmp" "$CURRENT_SCAN"
```

## Phase 4: Execute Security Analysis

Based on the selected technique, perform targeted analysis:

### Technique 1: Pattern Analysis

Look for known malicious patterns using regex:

```bash
#!/bin/bash

echo "🔍 Executing Pattern Analysis"

FINDINGS=()

while IFS= read -r file; do
  if [ ! -f "$file" ]; then continue; fi
  
  echo "Analyzing: $file"
  
  # Pattern 1: Secret exfiltration
  if grep -nE '(process\.env\.|os\.getenv|ENV\[)[^;]*\.(post|fetch|axios|request|curl|wget)' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Potential secret exfiltration in $file"
    FINDINGS+=("SECRET_EXFIL:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
  # Pattern 2: Eval/exec with user input
  if grep -nE '(eval|exec|Function)\s*\([^)]*(\$\{|process\.env|user|input|github\.)' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Dynamic code execution with external input in $file"
    FINDINGS+=("DYNAMIC_EXEC:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
  # Pattern 3: Obfuscated strings
  if grep -nE '(atob|btoa|Buffer\.from.*base64|String\.fromCharCode|\\x[0-9a-f]{2}.*\\x[0-9a-f]{2}.*\\x[0-9a-f]{2})' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Obfuscated content in $file"
    FINDINGS+=("OBFUSCATION:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
  # Pattern 4: Suspicious file operations
  if grep -nE '(rm\s+-rf|unlink.*\$|fs\.rmdir|fs\.unlink).*(\$\{|process\.env|user|input)' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Dangerous file operations in $file"
    FINDINGS+=("DANGEROUS_OPS:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
  # Pattern 5: Network calls to suspicious domains
  if grep -nE '(http://|https://)[^/]*(\.ru|\.cn|\.tk|pastebin|hastebin|ngrok|localtunnel)' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Suspicious network domain in $file"
    FINDINGS+=("SUSPICIOUS_DOMAIN:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
  # Pattern 6: Backdoor keywords
  if grep -niE '(backdoor|malware|rootkit|keylog|ransomware|trojan|c2|command.?and.?control)' "$file" > /tmp/pattern.txt; then
    echo "⚠️  Suspicious keywords in $file"
    FINDINGS+=("SUSPICIOUS_KEYWORDS:$file:$(head -1 /tmp/pattern.txt | cut -d: -f1)")
  fi
  
done < /tmp/files-to-scan.txt

echo "✅ Pattern analysis complete: ${#FINDINGS[@]} findings"
```

### Technique 2: AST Inspection

Analyze code structure for suspicious patterns:

```bash
#!/bin/bash

echo "🔍 Executing AST Inspection"

# For JavaScript files, check for suspicious structures
while IFS= read -r file; do
  if [[ "$file" == *.cjs ]]; then
    echo "Analyzing AST: $file"
    
    # Check for suspicious constructs (simplified - in production use proper JS parser)
    # Look for: deeply nested callbacks, unusual async patterns, hidden side effects
    
    # Pattern: Excessive nesting (potential obfuscation)
    NESTING=$(grep -o '(function\|=>\|{' "$file" | wc -l)
    if [ "$NESTING" -gt 200 ]; then
      echo "⚠️  Excessive nesting/complexity in $file"
      FINDINGS+=("HIGH_COMPLEXITY:$file:0")
    fi
    
    # Pattern: Suspicious function names
    if grep -nE 'function\s+(hack|pwn|exploit|backdoor|inject|payload)' "$file" > /tmp/ast.txt; then
      echo "⚠️  Suspicious function names in $file"
      FINDINGS+=("SUSPICIOUS_NAMES:$file:$(head -1 /tmp/ast.txt | cut -d: -f1)")
    fi
    
    # Pattern: Unusual module.exports or global assignments
    if grep -nE '(global\.|window\.|process\.)[a-zA-Z_$].*=.*require|module\.exports\s*=\s*require' "$file" > /tmp/ast.txt; then
      echo "⚠️  Suspicious global/export patterns in $file"
      FINDINGS+=("SUSPICIOUS_EXPORTS:$file:$(head -1 /tmp/ast.txt | cut -d: -f1)")
    fi
  fi
done < /tmp/files-to-scan.txt

echo "✅ AST inspection complete"
```

### Technique 3: Entropy Analysis

Detect obfuscated code by measuring entropy:

```bash
#!/bin/bash

echo "🔍 Executing Entropy Analysis"

# Function to calculate entropy (simplified)
calculate_entropy() {
  local file=$1
  local content=$(cat "$file" | tr -d '[:space:]')
  local length=${#content}
  
  # Count unique characters
  local unique=$(echo "$content" | grep -o . | sort -u | wc -l)
  
  # Simple entropy estimate (higher = more random/obfuscated)
  if [ "$length" -gt 0 ]; then
    echo "$((unique * 100 / length))"
  else
    echo "0"
  fi
}

while IFS= read -r file; do
  if [ ! -f "$file" ]; then continue; fi
  
  echo "Analyzing entropy: $file"
  
  ENTROPY=$(calculate_entropy "$file")
  
  # High entropy might indicate obfuscation
  if [ "$ENTROPY" -gt 70 ]; then
    echo "⚠️  High entropy detected in $file (entropy: $ENTROPY)"
    FINDINGS+=("HIGH_ENTROPY:$file:0:entropy=$ENTROPY")
  fi
  
  # Check for long hex/base64 strings
  if grep -E '[A-Za-z0-9+/=]{200,}|[0-9a-fA-F]{100,}' "$file" > /dev/null; then
    echo "⚠️  Long encoded strings in $file"
    FINDINGS+=("LONG_ENCODED:$file:0")
  fi
  
done < /tmp/files-to-scan.txt

echo "✅ Entropy analysis complete"
```

### Technique 4: Network Analysis

Track and analyze network-related code:

```bash
#!/bin/bash

echo "🔍 Executing Network Analysis"

while IFS= read -r file; do
  if [ ! -f "$file" ]; then continue; fi
  
  echo "Analyzing network patterns: $file"
  
  # Extract all URLs/domains
  grep -oE '(http|https|ftp)://[a-zA-Z0-9./?=_-]*' "$file" > /tmp/urls.txt || true
  
  if [ -s /tmp/urls.txt ]; then
    while IFS= read -r url; do
      # Check if URL is to unexpected domains
      if ! echo "$url" | grep -qE '(github\.com|githubusercontent\.com|microsoft\.com|npmjs\.org|api\.github\.com)'; then
        echo "⚠️  External network call to $url in $file"
        FINDINGS+=("EXTERNAL_NETWORK:$file:0:url=$url")
      fi
    done < /tmp/urls.txt
  fi
  
  # Check for IP addresses (often suspicious)
  if grep -nE '[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' "$file" > /tmp/ips.txt; then
    echo "⚠️  Hardcoded IP addresses in $file"
    FINDINGS+=("HARDCODED_IP:$file:$(head -1 /tmp/ips.txt | cut -d: -f1)")
  fi
  
done < /tmp/files-to-scan.txt

echo "✅ Network analysis complete"
```

### Technique 5: Behavioral Analysis

Analyze code execution patterns:

```bash
#!/bin/bash

echo "🔍 Executing Behavioral Analysis"

while IFS= read -r file; do
  if [ ! -f "$file" ]; then continue; fi
  
  echo "Analyzing behavior: $file"
  
  # Check for time-based logic (time bombs)
  if grep -nE '(new Date\(\)|Date\.now\(\)|getTime\(\)).*[<>]=?\s*[0-9]' "$file" > /tmp/time.txt; then
    if grep -E '(if|while).*Date' "$file" | grep -qE '(exit|throw|delete|destroy)'; then
      echo "⚠️  Time-based conditional with destructive action in $file"
      FINDINGS+=("TIME_BOMB:$file:$(head -1 /tmp/time.txt | cut -d: -f1)")
    fi
  fi
  
  # Check for persistence mechanisms
  if grep -nE '(cron|setInterval|setTimeout.*[0-9]{6,}|while.*true)' "$file" > /tmp/persist.txt; then
    echo "⚠️  Persistence mechanism in $file"
    FINDINGS+=("PERSISTENCE:$file:$(head -1 /tmp/persist.txt | cut -d: -f1)")
  fi
  
  # Check for anti-debugging
  if grep -nE '(debugger|isDebugger|chrome|devtools)' "$file" > /tmp/debug.txt; then
    echo "⚠️  Anti-debugging code in $file"
    FINDINGS+=("ANTI_DEBUG:$file:$(head -1 /tmp/debug.txt | cut -d: -f1)")
  fi
  
done < /tmp/files-to-scan.txt

echo "✅ Behavioral analysis complete"
```

### Technique 6: Dependency Audit

Check for vulnerable dependencies:

```bash
#!/bin/bash

echo "🔍 Executing Dependency Audit"

# Check package.json for known vulnerable packages
if [ -f ".github/workflows/package.json" ]; then
  echo "Auditing Node.js dependencies"
  
  # Look for suspicious or deprecated packages
  if grep -E '"(colors|faker|event-stream|flatmap-stream|getcookies)"' .github/workflows/package.json; then
    echo "⚠️  Potentially compromised package detected"
    FINDINGS+=("VULNERABLE_DEP:.github/workflows/package.json:0")
  fi
fi

# Check for suspicious require() patterns
while IFS= read -r file; do
  if [[ "$file" == *.cjs ]]; then
    # Check for requires to unusual paths
    if grep -nE 'require\(["\x27]\.\.\/\.\.\/\.\.\/' "$file" > /tmp/require.txt; then
      echo "⚠️  Suspicious require path traversal in $file"
      FINDINGS+=("PATH_TRAVERSAL:$file:$(head -1 /tmp/require.txt | cut -d: -f1)")
    fi
  fi
done < /tmp/files-to-scan.txt

echo "✅ Dependency audit complete"
```

### Technique 7: Full Comprehensive (Weekly)

Execute all techniques for complete coverage:

```bash
#!/bin/bash

echo "🔍 Executing FULL COMPREHENSIVE SCAN"
echo "Running all 6 techniques..."

# Execute all techniques above in sequence
# (Pattern Analysis, AST Inspection, Entropy Analysis, Network Analysis, Behavioral Analysis, Dependency Audit)

echo "✅ Full comprehensive scan complete"
```

## Phase 5: Analyze and Report Findings

```bash
#!/bin/bash

echo "📊 Analyzing findings..."

# Save findings to current scan
FINDINGS_JSON=$(printf '%s\n' "${FINDINGS[@]}" | jq -R -s -c 'split("\n") | map(select(length > 0))')

jq ".findings = $FINDINGS_JSON | .status = \"complete\"" "$CURRENT_SCAN" > "${CURRENT_SCAN}.tmp"
mv "${CURRENT_SCAN}.tmp" "$CURRENT_SCAN"

# Update scan history
TOTAL_SCANS=$(jq -r '.total_scans' "$SCAN_HISTORY")
TOTAL_FINDINGS=$(jq -r '.total_findings' "$SCAN_HISTORY")

NEW_TOTAL_SCANS=$((TOTAL_SCANS + 1))
NEW_TOTAL_FINDINGS=$((TOTAL_FINDINGS + ${#FINDINGS[@]}))

jq ".total_scans = $NEW_TOTAL_SCANS | .total_findings = $NEW_TOTAL_FINDINGS | .last_incremental_scan = \"$(date +%Y-%m-%d)\"" "$SCAN_HISTORY" > "${SCAN_HISTORY}.tmp"

if [ "$IS_FULL_SCAN" = "true" ]; then
  jq ".last_full_scan = \"$(date +%Y-%m-%d)\"" "${SCAN_HISTORY}.tmp" > "${SCAN_HISTORY}.tmp2"
  mv "${SCAN_HISTORY}.tmp2" "${SCAN_HISTORY}.tmp"
fi

mv "${SCAN_HISTORY}.tmp" "$SCAN_HISTORY"

echo "✅ Scan history updated"
echo "📊 Total scans: $NEW_TOTAL_SCANS"
echo "📊 Findings this scan: ${#FINDINGS[@]}"
echo "📊 Total findings all time: $NEW_TOTAL_FINDINGS"
```

## Phase 6: Forensics Analysis

For each finding, perform forensics to identify when the problematic code was introduced:

```bash
#!/bin/bash

if [ ${#FINDINGS[@]} -gt 0 ]; then
  echo "🔍 Performing forensics analysis..."
  
  # Perform git blame and commit analysis for each finding
  FORENSICS_DATA=()
  
  for finding in "${FINDINGS[@]}"; do
    TYPE=$(echo "$finding" | cut -d: -f1)
    FILE=$(echo "$finding" | cut -d: -f2)
    LINE=$(echo "$finding" | cut -d: -f3)
    EXTRA=$(echo "$finding" | cut -d: -f4-)
    
    if [ -f "$FILE" ] && [ "$LINE" != "0" ]; then
      echo "Analyzing: $FILE:$LINE"
      
      # Get commit that introduced this line
      BLAME_OUTPUT=$(git blame -L "$LINE,$LINE" --porcelain "$FILE" 2>/dev/null || echo "")
      
      if [ -n "$BLAME_OUTPUT" ]; then
        COMMIT_SHA=$(echo "$BLAME_OUTPUT" | grep "^[0-9a-f]\{40\}" | head -1 | cut -d' ' -f1)
        AUTHOR=$(git log -1 --format="%an" "$COMMIT_SHA" 2>/dev/null || echo "Unknown")
        COMMIT_DATE=$(git log -1 --format="%ai" "$COMMIT_SHA" 2>/dev/null || echo "Unknown")
        COMMIT_MSG=$(git log -1 --format="%s" "$COMMIT_SHA" 2>/dev/null || echo "Unknown")
        SHORT_SHA=$(echo "$COMMIT_SHA" | cut -c1-7)
        
        FORENSICS_DATA+=("$finding|$SHORT_SHA|$AUTHOR|$COMMIT_DATE|$COMMIT_MSG")
        
        echo "  ✓ Found: commit $SHORT_SHA by $AUTHOR on $COMMIT_DATE"
      else
        FORENSICS_DATA+=("$finding|unknown||||")
        echo "  ⚠ Could not determine origin commit"
      fi
    else
      FORENSICS_DATA+=("$finding|unknown||||")
    fi
  done
  
  echo "✅ Forensics analysis complete"
fi
```

## Phase 7: Generate Agentic Fix Tasks

Create actionable remediation tasks for each finding:

```bash
#!/bin/bash

if [ ${#FINDINGS[@]} -gt 0 ]; then
  echo "🛠️  Generating agentic fix tasks..."
  
  # Prepare detailed findings with forensics and fix tasks
  FINDINGS_DETAILS=""
  FIX_TASKS=""
  
  for i in "${!FORENSICS_DATA[@]}"; do
    FORENSICS="${FORENSICS_DATA[$i]}"
    
    # Parse forensics data
    FINDING=$(echo "$FORENSICS" | cut -d'|' -f1)
    TYPE=$(echo "$FINDING" | cut -d: -f1)
    FILE=$(echo "$FINDING" | cut -d: -f2)
    LINE=$(echo "$FINDING" | cut -d: -f3)
    EXTRA=$(echo "$FINDING" | cut -d: -f4-)
    
    COMMIT=$(echo "$FORENSICS" | cut -d'|' -f2)
    AUTHOR=$(echo "$FORENSICS" | cut -d'|' -f3)
    DATE=$(echo "$FORENSICS" | cut -d'|' -f4)
    MSG=$(echo "$FORENSICS" | cut -d'|' -f5)
    
    # Generate finding details
    FINDINGS_DETAILS+="### Finding $((i+1)): $TYPE\n\n"
    FINDINGS_DETAILS+="**Location**: \`$FILE:$LINE\`\n\n"
    
    if [ -n "$EXTRA" ]; then
      FINDINGS_DETAILS+="**Details**: $EXTRA\n\n"
    fi
    
    # Add forensics information
    FINDINGS_DETAILS+="**Forensics Analysis**:\n"
    if [ "$COMMIT" != "unknown" ]; then
      FINDINGS_DETAILS+="- Introduced in commit: \`$COMMIT\`\n"
      FINDINGS_DETAILS+="- Author: $AUTHOR\n"
      FINDINGS_DETAILS+="- Date: $DATE\n"
      FINDINGS_DETAILS+="- Message: \"$MSG\"\n"
    else
      FINDINGS_DETAILS+="- Unable to determine origin commit (file may be new or line may have moved)\n"
    fi
    FINDINGS_DETAILS+="\n"
    
    # Generate fix task based on finding type
    case "$TYPE" in
      SECRET_EXFIL)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Review and remove secret exfiltration pattern in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Verify if external network call is legitimate\n"
        FIX_TASKS+="  - If malicious: Remove the code and rotate any exposed credentials\n"
        FIX_TASKS+="  - If legitimate: Add domain to approved network list and document why needed\n"
        ;;
      DYNAMIC_EXEC)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Audit dynamic code execution in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Replace eval/exec with safer alternatives\n"
        FIX_TASKS+="  - Sanitize all user inputs before use\n"
        FIX_TASKS+="  - Add input validation and consider allowlist approach\n"
        ;;
      OBFUSCATION)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Investigate obfuscated content in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Decode and review the obfuscated content\n"
        FIX_TASKS+="  - If legitimate: Add comment explaining purpose\n"
        FIX_TASKS+="  - If malicious: Remove and investigate how it was introduced\n"
        ;;
      DANGEROUS_OPS)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Secure dangerous file operations in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Validate all file paths before operations\n"
        FIX_TASKS+="  - Use safe file operation alternatives\n"
        FIX_TASKS+="  - Add explicit permission checks\n"
        ;;
      SUSPICIOUS_DOMAIN)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Verify network domain in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Check if domain is legitimate for this operation\n"
        FIX_TASKS+="  - If suspicious: Remove and investigate origin\n"
        FIX_TASKS+="  - Consider replacing with internal service\n"
        ;;
      SUSPICIOUS_KEYWORDS)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Review suspicious keywords in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Determine if code is intentionally malicious or just poorly named\n"
        FIX_TASKS+="  - Remove if malicious, rename if legitimate\n"
        FIX_TASKS+="  - Review commit history for context\n"
        ;;
      *)
        FIX_TASKS+="- [ ] **Task $((i+1))**: Investigate $TYPE pattern in \`$FILE:$LINE\`\n"
        FIX_TASKS+="  - Review the code and determine if it's a security risk\n"
        FIX_TASKS+="  - Remediate if confirmed malicious\n"
        FIX_TASKS+="  - Document findings and actions taken\n"
        ;;
    esac
    FIX_TASKS+="\n"
  done
  
  echo "✅ Generated ${#FINDINGS[@]} remediation tasks"
fi
```

## Phase 8: Create Security Issues with Actionable Tasks

If findings are detected, create detailed security issues with forensics and fix tasks:

```bash
#!/bin/bash

if [ ${#FINDINGS[@]} -gt 0 ]; then
  echo "🚨 SECURITY FINDINGS DETECTED!"
  echo "Creating security issue with actionable tasks..."
  
  # Create issue using safe-outputs
  cat > /tmp/security-issue.md <<EOF
# 🚨 Security Red Team Findings - $(date +%Y-%m-%d)

**Scan Mode**: $SCAN_MODE  
**Technique**: $TECHNIQUE  
**Files Analyzed**: $FILE_COUNT  
**Findings**: ${#FINDINGS[@]}

## 📋 Executive Summary

The daily security red team scan has detected **${#FINDINGS[@]}** potential security issues in the \`actions/setup/js\` and \`actions/setup/sh\` directories using the **$TECHNIQUE** technique.

## 🔍 Detailed Findings

$FINDINGS_DETAILS

## 🛠️ Remediation Tasks

@pelikhan The following tasks have been generated to address the security findings. Please review and execute as appropriate:

$FIX_TASKS

## 📊 Analysis Metadata

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Run URL**: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
- **Technique Used**: $TECHNIQUE
- **Scan Type**: $SCAN_MODE
- **Cache Location**: /tmp/gh-aw/cache-memory/security-red-team

## 🎯 Next Steps

1. **Triage**: Review each finding and determine if it's a true positive or false positive
2. **Prioritize**: Address high-severity issues first (secret exfiltration, backdoors)
3. **Execute**: Complete the remediation tasks in the checklist above
4. **Verify**: Re-run the security scan after fixes to confirm issues are resolved
5. **Investigate**: For any confirmed malicious code, investigate how it was introduced and by whom

---

*🤖 Generated by Daily Security Red Team Agent*  
*📅 Scan completed at $(date -u +"%Y-%m-%d %H:%M:%S UTC")*
EOF

  # Use create-issue safe output
  echo "Creating issue via safe-outputs..."
  
else
  echo "✅ No security findings detected"
  echo "Using noop safe output..."
fi
```

## Output Requirements

Your workflow MUST produce a safe output:

1. **If security findings detected**:
   - **PERFORM** forensics analysis using `git blame` to identify when code was introduced
   - **GENERATE** actionable remediation tasks for each finding
   - **CALL** the `create_issue` tool with:
     - Title: "Security Red Team Findings - [DATE]"
     - Body: Detailed findings with forensics data, fix tasks, and @pelikhan mention
     - Labels: ["security", "red-team"]

2. **If no findings detected**:
   - **CALL** the `noop` tool with completion message:
   ```json
   {
     "noop": {
       "message": "✅ Daily security red team scan completed. Analyzed [N] files using [TECHNIQUE] technique. No suspicious patterns detected."
     }
   }
   ```

## Important Guidelines

### Security Best Practices
- **Never execute suspicious code** - only analyze statically
- **Sanitize outputs** - ensure no secrets appear in issues
- **Document reasoning** - explain why patterns are flagged
- **Minimize false positives** - be confident before raising alerts

### Performance Optimization
- **Stay within 60-minute timeout**
- **Use cache-memory efficiently** - persist state between runs
- **Batch operations** - group similar analyses
- **Focus on changed files** - unless it's a weekly full scan

### Cache-Memory Usage
- **Rotate techniques daily** - avoid analysis fatigue
- **Track scan history** - monitor trends and patterns
- **Persist findings** - build knowledge over time
- **Use filesystem-safe timestamps** - ensure artifact compatibility (no colons)

## Success Criteria

A successful security scan:
- ✅ Initializes cache-memory and loads previous state
- ✅ Determines scan mode (daily incremental vs weekly full)
- ✅ Selects and executes appropriate technique
- ✅ Analyzes all target files for security issues
- ✅ Performs forensics analysis using git blame to identify origin commits
- ✅ Generates actionable remediation tasks for each finding
- ✅ Updates cache-memory with findings and progress
- ✅ Creates security issue with forensics data and fix tasks if findings detected (with @pelikhan mention)
- ✅ Calls noop tool if no findings detected
- ✅ Completes within 60-minute timeout
- ✅ Uses filesystem-safe timestamps for cache files

## Begin Your Security Analysis

Initialize your cache-memory, determine today's technique, and begin your comprehensive security scan of the `actions/setup/js` and `actions/setup/sh` directories!
