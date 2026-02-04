# Run 21665406950 Summary - 2026-02-04

## Quick Stats
- **Outcome**: ✅ SANDBOX SECURE
- **Techniques**: 30 (100% novel)
- **Escapes**: 0
- **Duration**: ~6 minutes

## Key Findings
1. **iptables NAT is unbeatable**: Even Python ctypes raw syscalls are intercepted at kernel level
2. **Squid 6.13 is robust**: Blocks all HTTP smuggling variants (trailers, chunks, pollution, buffer overflow)
3. **Alternative protocols timeout**: SCTP, DCCP, UDP - none routed by iptables
4. **Container isolation strong**: No CAP_NET_RAW, no CAP_NET_ADMIN, eBPF disabled, /dev/mem inaccessible

## Novel Attack Categories
- Kernel-level: eBPF, VDSO, Netlink, modules, /dev/mem
- Protocol-level: SCTP, DCCP, UDP datagram
- HTTP smuggling: trailers, zero-chunks, parameter pollution, buffer overflow
- Language bypasses: Python ctypes, Perl, Ruby, Node.js
- Container escapes: /proc/1/root inspection
- Encoding: Unicode normalization

## Recommendations for Future Runs
- Explore: Time-of-check-time-of-use (TOCTOU) races
- Explore: Squid version-specific CVEs (if any released)
- Explore: IPv6 attacks (if IPv6 enabled in future)
- Explore: DNS covert channels (not escape but data exfil)
- Explore: HTTP/3 over QUIC (UDP-based, different from HTTP/2)

## Historical Context
- Total runs: 18
- Total techniques: 425
- Escapes found: 1 (patched in v0.9.1)
- Current status: **SECURE**
