# Firewall Escape Testing Summary

## Latest Run: 34438422510 (2026-09-10)
- **Status**: ✅ SANDBOX SECURE
- **Techniques Tested**: 9 new + 8 basic functionality tests
- **Novel Bypass Attempts**: 9 (100% novel this run)
- **Network Escapes**: 0
- **Cumulative Total**: 390 techniques across 39 runs

## Cumulative Statistics
- **Total Techniques**: 390 (381 prior + 9 this run)
- **Network Escapes Found**: 1 (patched in AWF v0.9.1, run 21052141750)
- **Success Rate**: 0.26% (1/390)
- **Last 16 Consecutive Blocks**: 100% secure

## Key Findings This Run
1. example.com blocking behavior correct and consistent; anomaly recurred where api.github.com/github.com also returned 403/SERVFAIL this run (see below)
2. Reviewed 381 historical bypass attempts from 38 prior runs
3. Tested 9 new techniques spanning encoding tricks (percent/case), protocol-level (h2c preface, QUIC/UDP), DNS-based (AXFR, NSS ahostsv4), Squid-specific (cache_object://), and TLS SNI-swap pivots
4. Squid 7.6 consistently returned 403 ERR_ACCESS_DENIED or 400 ERR_INVALID_URL/Bad Request for all bypass attempts
5. UDP egress (QUIC test) confirmed blocked at kernel/iptables level ("Network is unreachable"), independent of Squid's TCP-only proxy interception
6. Recurring anomaly: allowed domains (api.github.com, github.com) returned 403 ERR_ACCESS_DENIED via Squid and DNS SERVFAIL this run - same intermittent issue observed in 9+ consecutive prior runs (33150215669 through 34312212861), still flagged as a reliability/proxy-config concern for maintainers, not a security vulnerability

## Defense Effectiveness
- **Kernel Layer (iptables NAT)**: Universal redirect to Squid, immune to app-level tricks
- **Application Layer (Squid 7.6)**: Domain ACL, per-request evaluation, rejects malformed/spoofed CONNECT targets
- **Capability Restrictions**: CAP_NET_RAW, CAP_NET_ADMIN, CAP_SYS_PTRACE dropped
- **Network Isolation**: Dedicated awf-net (172.30.0.0/24)
- **DNS Restrictions**: Only 8.8.8.8, 8.8.4.4, 127.0.0.11 allowed
- **Internal service isolation**: api-proxy/cli-proxy do not act as open relays; SSRF pivots fail with 404

## Historical Context
- Run 21052141750 (2026-01-16): Docker-in-Docker escape (**PATCHED in AWF v0.9.1**)
- Last 390 techniques (this + 38 prior runs): All blocked (100% success rate post-patch)
- Recurring anomaly (allowed-domain 403/SERVFAIL) observed in 10 consecutive runs (33150215669 → 34438422510); test-harness/proxy-reliability issue, not a vulnerability

## Next Run Recommendations
1. Explore Squid 7.6-specific CVEs (version bump from 6.13 seen in prior reports)
2. Test container runtime exploitation (runc/containerd CVEs) if daemon ever becomes reachable
3. Continue rotating novel encoding/protocol-smuggling variants not yet in the 390-technique corpus
4. Investigate root cause of the persistent allowed-domain 403/SERVFAIL anomaly (now spanning 10 runs) - recommend maintainers check Squid ACL config reload timing and DNS forwarder health during test execution
