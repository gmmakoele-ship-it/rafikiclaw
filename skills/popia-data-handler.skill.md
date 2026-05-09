# POPIA Data Handler Skill

> Applies to: Any RafikiClaw agent that handles personal information as defined by South Africa's Protection of Personal Information Act (POPIA).

## Skill Profile

- **ID:** rafiki-popia-data-handler
- **Version:** 1.0.0
- **Agent:** Any Rafiki OS agent
- **Required:** Yes for agents processing personal data

---

## Capabilities

This skill governs how the agent must handle personal information throughout its lifecycle.

---

## Rules

### 1. Consent Capture

Before collecting any personal data, the agent must:

- State the purpose for which data is being collected
- Not collect data beyond what is strictly necessary for the task
- Record that consent was given (timestamp + scope) in the agent log

```
When asked to store personal info:
  → Confirm purpose is stated and necessary
  → Log: CONSENT_RECORDED | purpose=<reason> | timestamp=<utc>
  → Do NOT store beyond the stated purpose
```

### 2. Data Minimisation

The agent must only collect the **minimum** personal data needed.

- Do NOT log full IC numbers, full credit card numbers, or full passwords
- Store only what is needed for the specific task
- Truncate or mask fields in logs and outputs

**Allowed patterns:**
```
Name: John S.          (first name + initial only)
Email: j***@example.com
Phone: +27 82 *** ***  (last 4 digits only)
ID: XXX XXX XXX []1234 (masked except last 4)
```

### 3. Retention Limits

| Data Type | Maximum Retention |
|-----------|-------------------|
| Session logs | 30 days |
| Task context | Until task completion + 7 days |
| Audit records | 12 months |
| Consent records | 36 months |

After the retention period, personal data must be deleted or anonymised.

### 4. Breach Notification

If the agent detects or suspects a data breach:

1. Stop the operation immediately
2. Log the breach event: `BREACH_DETECTED | scope=<what> | timestamp=<utc>`
3. Notify the operator via the configured alert channel
4. Do NOT attempt to fix or hide the breach

**Notification template:**
```
SUBJECT: [POPIA BREACH ALERT] Agent: <agent-name> | Time: <utc>
A personal data breach has been detected.
Scope: <what data, how many records>
Action taken: <stopped/notified>
```

### 5. Subject Access Request (SAR) Handling

When a person requests access to their personal data:

1. Acknowledge within 72 hours
2. Retrieve all records associated with the requester's identifier
3. Provide data in a human-readable format
4. Confirm whether data has been shared with third parties
5. If corrections are requested, update and re-confirm

**Agent response to SAR:**
```
I've received your request to access your personal data.
I will retrieve all data associated with your account and respond
within 72 hours. Your reference number is: SAR-<timestamp>.
```

### 6. Data Sharing Restrictions

- Never share personal data with third parties without explicit consent
- If an external API is called, strip personal data before transmission
- Log all third-party disclosures with: `DISCLOSURE | recipient=<who> | data_type=<what> | consent=<yes/no>`

---

## Log Format

All POPIA-related events must include the field:
```yaml
popia_compliant: true
compliance_skill: rafiki-popia-data-handler@v1.0.0
```

---

## Capability Contract Declaration

To use this skill, include in `agent.skills`:
```yaml
skills:
  - path: skills/popia-data-handler.skill.md
    version: "1.0.0"
```

And add to your `.claw` file under `agent.habitat.env`:
```yaml
POPIA_MODE: strict
LOG_RETENTION_DAYS: "30"
SAR_WINDOW_HOURS: "72"
```