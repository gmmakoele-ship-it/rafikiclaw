# Language Skill — Zulu (isiZulu)

> **Skill ID:** rafiki-lang-zulu | **Version:** 1.0.0 | **Agent:** Any Rafiki OS agent

## Overview

This skill gives the agent awareness of Zulu language conventions, greetings, and cultural context. It does **not** provide full translation — it ensures the agent responds respectfully and accurately when communicating in Zulu.

---

## When to Activate

Activate when:
- The user writes in Zulu (e.g. "Sawubona", "Ungaphi", "Ngiyabonga")
- The conversation context indicates Zulu-language preference
- A locale flag or user preference is set to `zu` or `zul`

---

## Greetings & Responses

| Scenario | Zulu | English |
|----------|------|---------|
| Morning greeting | Sawubona (lit. "I see you") | Hello |
| General greeting | Sawubona | Hello |
| Response to "Sawubona" | Yebo, sawubona | Yes, hello |
| How are you? | Ungaphi? | How are you? |
| I'm fine | Ngisaphila | I am well |
| Thank you | Ngiyabonga | Thank you |
| Thank you very much | Ngiyabonga kakhulu | Thank you very much |
| Goodbye | Sala kahle (stay well) | Goodbye |
| Until next time | Sizobonana | See you later |
| Yes | Yebo | Yes |
| No | Cha | No |

---

## Formality Register

Zulu has a distinction between **formal** and **informal** registers:

### Formal (used with elders, officials, strangers)
- Use "Ninja" or title + surname where known
- Avoid direct "you" — use indirect forms or titles
- Examples: "Ungabonga" (plural "you" polite), "Ngiyajabula ukukwazi" (pleased to meet you)

### Informal (used with peers, friends, children)
- First names are fine
- Direct "you" is acceptable

**Rule:** Default to **formal** unless the user indicates otherwise or the context is clearly casual.

---

## Place Names & Geography

Common Zulu place name conventions:
- Many end in **-i** or **-ini** (e.g., "Durban" → "eDurbanini")
- Mountains and rivers often have descriptive Zulu names
- When in doubt, use the standard English name with the Zulu qualifier

---

## Key Cultural Notes

| Concept | Meaning |
|---------|---------|
| Ubuntu | "I am because we are" — community over individual |
| Intsika | A symbol of respect; do not point feet at elders |
| Indoda | A respected man / warrior |
| Umfana | A young man / boy |
| Mama / Mama woMuntu | Respectful address for women |

---

## Translation Trigger Rules

When the user includes Zulu, respond as follows:

1. **Mixed language**: If user switches between English and Zulu, respond in the language they used for the last message. When in doubt, respond in the language of the most recent complete sentence.

2. **Zulu-only queries**: Respond in Zulu. If your Zulu is not confident, respond in English with a brief Zulu acknowledgement.

3. **Greetings in Zulu**: Always acknowledge in Zulu, even if the rest of the response is in English.

4. **Formal requests**: Use polite Zulu forms. Avoid casual slang unless clearly appropriate.

---

## Quick Reference Phrases for Agent

```
# Acknowledgement
Sawubona! / Yebo, sawubona!

# Polite request
Ngiyacela... (I request/please...)
Ngiyabonga. (Thank you)

# Confirmation
Kunjalo. (It is so / Yes, that's correct.)
Cha. (No.)

# Closing
Sizobonana. / Sala kahle.
```

---

## Capability Contract Declaration

```yaml
skills:
  - path: skills/lang-zulu.skill.md
    version: "1.0.0"
```