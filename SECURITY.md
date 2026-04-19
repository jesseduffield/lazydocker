# Security Policy for lazydocker

## ⚠️ Overview

This document describes how to report security vulnerabilities, how we prioritize and remediate them, and our supported versions and responsibilities. We aim to make lazydocker safer, and welcome responsible disclosures.

---

## Table of Contents

1. [Reporting a Vulnerability](#reporting-a-vulnerability)  
2. [Scope & Assets](#scope--assets)  
3. [Supported Versions & Maintenance](#supported-versions--maintenance)  
4. [Disclosure Policy](#disclosure-policy)  
5. [Handling & Remediation Process](#handling--remediation-process)  
6. [Severity Classification](#severity-classification)  
7. [Third‑Party Dependencies & Supply Chain](#third-party-dependencies--supply-chain)  
8. [Security Audits & External Review](#security-audits--external-review)  
9. [Acknowledgments & Hall of Fame](#acknowledgments--hall-of-fame)  
10. [Contact Information](#contact-information)  
11. [Legal & Safe Harbor](#legal--safe-harbor)  

---

## 1. Reporting a Vulnerability

If you discover a security issue in **lazydocker**, please do **not** open a public GitHub issue exposing the vulnerability. Instead:

1. Email the maintainers at **security@lazydocker.dev** (or another private contact if designated).  
2. In your report, include:
   - Your name/handle and preferred contact method  
   - A clear description of the issue (what is wrong, root cause, how it can be triggered)  
   - Version(s) of lazydocker affected  
   - Steps to reproduce (minimal if possible)  
   - Potential impact (e.g. code execution, data leak, container escape)  
   - Proof-of-concept exploit or patch (if available)  
   - Any constraints or environment details (OS, Docker version, configuration)  

We commit to acknowledging receipt of a valid report within **48 hours**.  

If you cannot use email, or you have confidentiality concerns, you may contact a maintainer privately (e.g. via GitHub private message) or use GitHub’s [security advisories] interface.

---

## 2. Scope & Assets

### In-Scope

- The **lazydocker** binary and source code, including all CLI/UI/interaction logic  
- Configuration files (e.g. in `~/.config/jesseduffield/lazydocker`)  
- Hooks, scripts, or extension mechanisms  
- Behavior when interacting with Docker daemon / API  
- Any parts of the codebase that process external input (logs, parsing, keybindings, plugins)  

### Out-of-Scope (typically)

- Docker daemon itself or vulnerabilities in external dependencies (though if vulnerable, it should be flagged)  
- Issues in third-party libraries unless they are exploited in lazydocker’s usage  
- Misconfiguration or usage in insecure environments (e.g. running with overly permissive permissions)  
- Physical or OS-level vulnerabilities unrelated to lazydocker logic  

---

## 3. Supported Versions & Maintenance

| Version Branch | Security Support | Backporting Policy |
|----------------|------------------|---------------------|
| `main` (latest) | Fully supported — all new security fixes go here | — |
| Latest **stable release** | Supported for a reasonable timeframe (e.g. 12–18 months) | Critical / High fixes may be backported |
| Older versions (>18 months out) | End-of-life for guaranteed support | Community or contributors may backport, but not guaranteed |

Users are encouraged to stay on recent stable releases or `main`.

When a release includes a security fix, it should include a **major**, **minor**, or **patch** version bump as appropriate, and release notes should clearly mention “Security fix: …”.

---

## 4. Disclosure Policy

We follow a **responsible disclosure** approach:

- We request that vulnerability details remain private until a fix is ready or an advisory is published.  
- If the reporter desires public acknowledgment (name/handle), we will credit appropriately (unless anonymity is requested).  
- Once a fix is available and users have a reasonable time to upgrade, we will publish a GitHub Security Advisory and/or release notes summarizing the issue, its impact, affected versions, and mitigation steps.

If a vulnerability is already public or being actively exploited, we may accelerate disclosure and fix timelines.

---

## 5. Handling & Remediation Process

1. **Triage & Acknowledge (within 48h)**  
   Assess scope and severity. Determine whether the issue is genuine, reproducible, and in-scope.

2. **Reproduce & Validate**  
   Confirm the issue in a controlled environment (minimized setup). Check consistency across versions/configurations.

3. **Root Cause & Fix Development**  
   Create a patch or fix. Include tests if possible (unit test, integration test, fuzzing). Ensure the fix addresses the root cause, not just symptoms.

4. **Internal Review / Security Review**  
   Let one or more project maintainers review the patch. For high/critical issues, consider bringing in a third-party reviewer or security consultant.

5. **Release & Backport**  
   Publish the fix in a new version. For critical/high severity vulnerabilities, attempt backporting to supported stable versions. Label releases clearly (e.g. `v0.25.1` with “Security fix” in changelog).

6. **Disclosure / Advisories**  
   After users have reasonable time, publish a public advisory (via GitHub, project website, mailing list). Include:  
   - Description of the issue  
   - Affected versions  
   - How to detect exploitation  
   - Steps to remediate / upgrade  

7. **Post-Mortem / Lessons Learnt**  
   Document the issue internally: what caused it, how the fix was validated, whether further fixes are needed (e.g. additional tests, fuzzing, CI checks).

### Timelines (target)

- Acknowledgment: within **48 hours**  
- Patch creation: ideally within **14 days** for high/critical issues  
- Release: as soon as fix is stabilized  
- Advisory publication: shortly after release, allowing user upgrade window  

These are target goals, not guarantees—delays may occur depending on complexity.

---

## 6. Severity Classification

We use these general categories. Severity may be adjusted based on context.

| Severity | Description / Examples |
|----------|--------------------------|
| **Critical** | Remote code execution, container escape, privilege escalation, or ability to manipulate lazydocker to compromise host or Docker daemon. Active exploit or high severity. |
| **High** | Data leak of credentials/tokens, unauthorized access to sensitive content, denial-of-service by resource exhaustion or panic conditions. |
| **Medium** | Non-trivial bugs that require specific conditions or limited context, moderate impact. |
| **Low** | Minor information disclosure (e.g., version banner), safe misconfigurations, UI bugs with minimal impact. |

Fixes for **Critical** & **High** vulnerabilities should be prioritized; **Medium/Low** should be fixed in normal maintenance cycles.

---

## 7. Third‑Party Dependencies & Supply Chain

- LazyDocker depends on external Go modules and possibly Docker API libraries. If a dependency has a known vulnerability, we should evaluate its impact and upgrade or patch as needed.  
- We encourage maintainers to run dependency vulnerability scanners (e.g. `go audit`, `go mod tidy`, `govulncheck`) and monitor upstream security announcements.  
- For CI or build scripts, we should validate the integrity of artifacts (e.g. verify checksums/signatures of dependencies, ensure reproducible builds where possible).

---

## 8. Security Audits & External Review

- We welcome external security audits, reviews, or bug bounties. If you’re a security researcher or auditor and wish to review the code, contact us under this security policy.  
- If a vulnerability is discovered by a third party audit, it will be handled through our standard reporting and remediation process.  
- We may periodically contract or solicit audits on modules that handle user input, parsing, UI, or Docker interaction logic.

---

## 9. Acknowledgments & Hall of Fame

We aim to recognize security researchers who responsibly disclose vulnerabilities. With permission, their name or handle may be credited in:

- GitHub Security Advisory  
- Release notes or changelog  
- A **SECURITY_HALL_OF_FAME.md** or dedicated section  

If anonymity is preferred, we will withhold the name.

---

## 10. Contact Information

- **Primary security email**: `security@lazydocker.dev`  
- Alternative contact: private GitHub message to **Jesse Duffield** or other maintainers  
- For special handling (e.g. legal/confidentiality), indicate in your submission, and we will respect those requirements  

---

## 11. Legal & Safe Harbor

By submitting a vulnerability report under this policy:

- You confirm that you have not violated laws, contracts, or confidentiality agreements in discovering it.  
- You agree to allow us to remediate and publish a public advisory (with or without attribution based on your preference).  
- We commit **not** to initiate legal action against you, to the extent you act in good faith and follow this policy's guidelines.  
- We reserve the right to refuse or ignore reports that do not follow responsible disclosure (e.g. publishing publicly without notice).

---

**Signed,**  
The lazydocker Project Team  
(GitHub: jesseduffield/lazydocker)  
License: MIT :contentReference[oaicite:0]{index=0}  
Repository: Go / Shell codebase :contentReference[oaicite:1]{index=1}  

