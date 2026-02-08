# Firewall Escape Test - Run 21795620032

**Date**: 2026-02-08
**Status**: SANDBOX SECURE
**Techniques Tested**: 30
**Novelty Rate**: 100%
**Escapes Found**: 0

## Key Findings
- Gateway HTTP service (172.30.0.1:80) accessible but returns 404
- Port timing: 80/443 respond in 14ms (NAT'd to Squid), others timeout at 1003ms
- DNS CHAOS class queries not supported by Google DNS (SERVFAIL)
- Squid ACL is case-insensitive and handles Unicode/IDN normalization
- HTTP/1.0 legacy protocol also filtered by Squid
- Non-HTTP protocols (SMTP, SSH, FTP) blocked or timeout
- All application-layer bypasses (Ruby, Python, Go, Java, PHP) intercepted by iptables NAT

## Architecture Strengths
1. Kernel-level iptables NAT intercepts ALL TCP connections
2. Squid 6.13 enforces strict domain ACL (case-insensitive, Unicode-aware)
3. Capabilities properly dropped (NET_RAW, NET_ADMIN, SYS_PTRACE)
4. Seccomp mode 2 + AppArmor docker-default active
5. Chroot and filesystem restrictions effective

## Recommendations
- Monitor gateway HTTP service (172.30.0.1:80) behavior
- Consider DNS query logging for covert channel monitoring
- Continue regular security testing with novel techniques

## Cumulative Statistics
- Total techniques: 515 (21 runs)
- Historical escapes: 1 (patched in AWF v0.9.1)
- Success rate: 0.19% (1/515)
- Last 485 techniques: All blocked
