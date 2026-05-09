# POPIA Self-Assessment Checklist

> For use before deploying any RafikiClaw agent in a context where personal information may be processed. Complete this checklist and retain it as part of your accountability record.

---

## Section A — Applicability Assessment

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| A1 | Does the agent collect any personal information (names, emails, IDs, phone numbers)? | ☐ | ☐ | ☐ | |
| A2 | Does the agent store personal information in logs, databases, or files? | ☐ | ☐ | ☐ | |
| A3 | Does the agent send personal information to any external system or API? | ☐ | ☐ | ☐ | |
| A4 | Will the agent interact with children or minors? | ☐ | ☐ | ☐ | |
| A5 | Does the agent handle special categories of data (health, financial, biometric)? | ☐ | ☐ | ☐ | |
| A6 | Is the agent deployed in a context where it processes data for third parties? | ☐ | ☐ | ☐ | |
| A7 | Are you/your organisation a "responsible party" under POPIA? | ☐ | ☐ | ☐ | |

> If all answers in Section A are "No" or "N/A", POPIA may not apply. Consult a lawyer before proceeding.

---

## Section B — Lawful Basis

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| B1 | Is there a lawful basis for processing (consent, contract, legal obligation)? | ☐ | ☐ | ☐ | |
| B2 | Has the purpose of processing been clearly stated? | ☐ | ☐ | ☐ | |
| B3 | Have data subjects been informed of their rights? | ☐ | ☐ | ☐ | |
| B4 | Is there a privacy notice or policy accessible to data subjects? | ☐ | ☐ | ☐ | |

---

## Section C — Data Minimisation & Security

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| C1 | Is only the minimum necessary personal data collected? | ☐ | ☐ | ☐ | |
| C2 | Is personal data masked or truncated in logs? | ☐ | ☐ | ☐ | |
| C3 | Are retention periods defined and enforced? | ☐ | ☐ | ☐ | |
| C4 | Is the agent running inside a container with restricted access? | ☐ | ☐ | ☐ | |
| C5 | Are network connections restricted to only what's necessary? | ☐ | ☐ | ☐ | |
| C6 | Are API keys and secrets injected at runtime, not hardcoded? | ☐ | ☐ | ☐ | |

---

## Section D — Data Subject Rights

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| D1 | Can data subjects request access to their personal data? | ☐ | ☐ | ☐ | |
| D2 | Can data subjects request correction of inaccurate data? | ☐ | ☐ | ☐ | |
| D3 | Is there a process to delete personal data on request? | ☐ | ☐ | ☐ | |
| D4 | Are SARs acknowledged within 72 hours? | ☐ | ☐ | ☐ | |

---

## Section E — Breach Readiness

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| E1 | Is there a breach detection mechanism? | ☐ | ☐ | ☐ | |
| E2 | Is there an alert/notification channel for breaches? | ☐ | ☐ | ☐ | |
| E3 | Has a breach response procedure been documented? | ☐ | ☐ | ☐ | |
| E4 | Is there a process to notify the Information Regulator within 72 hours of a breach? | ☐ | ☐ | ☐ | |

---

## Section F — Third-Party Disclosures

| # | Question | Yes | No | N/A | Notes |
|---|----------|-----|----|-----|-------|
| F1 | Are any third-party APIs called by the agent? | ☐ | ☐ | ☐ | |
| F2 | Is personal data stripped before being sent to third parties? | ☐ | ☐ | ☐ | |
| F3 | Are third-party transfers covered by a data processing agreement? | ☐ | ☐ | ☐ | |

---

## Sign-Off

| Role | Name | Signature | Date |
|------|------|-----------|------|
| Operator/Deployer | | | |
| POPIA Officer/Privacy Lead | | | |
| Technical Lead | | | |

---

## Notes & Exceptions

_Use this space to document any non-compliant items, mitigations, or exceptions granted._

---

*Retain this completed checklist for a minimum of 36 months.*
*RafikiClaw audit logs are stored at `~/.rafikiclaw/logs/`*