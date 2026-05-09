# POPIA Compliance for RafikiClaw Agents

> South Africa's **Protection of Personal Information Act (Act 4 of 2013)** governs how organisations collect, process, stored, and share personal information. AI agents that handle personal data must comply.

## Why It Matters for Rafiki OS

If a RafikiClaw agent processes personal information — even accidentally — POPIA applies. This includes:

- Storing names, emails, phone numbers, or ID numbers
- Responding to user queries with personal context
- Logging conversations that contain personal data
- Sending notifications or reports that include personal information

## Key POPIA Principles Relevant to AI Agents

| Principle | What It Means for Agents |
|-----------|--------------------------|
| **Accountability** | You must be able to demonstrate compliance |
| **Processing Limitation** | Only collect what you need |
| **Purpose Specification** | State why personal data is collected upfront |
| **Further Processing Limitation** | Don't repurpose data without consent |
| **Information Quality** | Keep personal data accurate and up to date |
| **Openness** | Be transparent about data handling |
| **Security Safeguards** | Protect personal data from unauthorised access |
| **Data Subject Participation** | Allow people to access and correct their data |

## What RafikiClaw Provides

- **Immutable audit logs** — `~/.rafikiclaw/logs/*.jsonl` are append-only and stored locally
- **Capability contracts** — Skills must declare what they access before execution
- **No external data transmission** — All logs stay on the local machine by default
- **Container isolation** — Agent code runs in containers, not on the host
- **Deny-by-default policies** — Network, mount, and env access blocked unless declared

## What You (The Operator) Must Do

1. Assess whether your agent will handle personal information
2. If yes, include `skills/popia-data-handler.skill.md` in the agent's skillset
3. Complete `legal/popia/assessment-checklist.md` before deployment
4. Register with the Information Regulator if required
5. Implement a data breach response plan

## Useful References

- [Information Regulator South Africa](https://popia.co.za/)
- [SA Law — POPIA Full Text](http://www.saflii.org/za/legis/act/2013/a4.html)
- [Department of Justice — POPIA Regulations](https://www.justice.gov.za/regulations/privacy/index.html)

> ⚠️ This is operational guidance, not legal advice. Consult a POPIA lawyer for your specific context.