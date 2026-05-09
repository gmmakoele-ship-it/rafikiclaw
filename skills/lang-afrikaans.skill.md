# Language Skill — Afrikaans

> **Skill ID:** rafiki-lang-afrikaans | **Version:** 1.0.0 | **Agent:** Any Rafiki OS agent

## Overview

This skill gives the agent awareness of Afrikaans language conventions, greetings, and cultural context. It does **not** provide full translation — it ensures the agent responds respectfully and accurately when communicating in Afrikaans.

---

## When to Activate

Activate when:
- The user writes in Afrikaans (e.g., "Goeie dag", "Hoe gaan dit", "Baie dankie")
- The conversation context indicates Afrikaans-language preference
- A locale flag or user preference is set to `af` or `afr`

---

## Greetings & Responses

| Scenario | Afrikaans | English |
|----------|----------|---------|
| Good day (formal) | Goeie dag | Good day |
| Good morning | Goeie more | Good morning |
| Good evening | Goeie naand | Good evening |
| How are you? | Hoe gaan dit met jou? | How are you? |
| I'm fine | Dit gaan goed met my | I am well |
| Thank you | Dankie | Thank you |
| Thank you very much | Baie dankie | Thank you very much |
| Goodbye | Totsiens | Goodbye |
| See you later | Sien jou | See you |
| Yes | Ja | Yes |
| No | Nee | No |
| Please | Asseblief | Please |
| You're welcome | Dis niks | You're welcome |

---

## Formality Register

Afrikaans sits on a formality spectrum:

### Formal (used with elders, officials, strangers)
- Use titles: "Meneer" (Mr), "Mevrou" (Mrs), "Juffrou" (Ms)
- "U" (capital) for formal "you"
- More complete sentences, polite markers

### Informal (used with peers, friends)
- First names acceptable
- "Jy" (lowercase) for informal "you"
- Contractions more common: "hoe gaan dit" not "hoe gaan dit met jou"

**Rule:** Default to **formal** with strangers and elders. If unsure, err on the side of formality — it's easier to relax than to recover from being too casual.

---

## Common Afrikaans Expressions

| Expression | Meaning |
|------------|---------|
| Ag, nee! | Oh no! / Really? (surprise/disbelief) |
| Waarom nie? | Why not? |
| Dis reg so | That's right / It's correct |
| Ek dog... | I thought... |
| Natsiklik | Naturally / Of course |
| Dis mooi | That's nice / Beautiful |
| Lekker | Nice / Good / Enjoyable (very versatile word!) |

---

## South African Context Notes

- Afrikaans is one of 12 official languages — always acknowledge the user's language choice
- Many South Africans are bilingual or multilingual; code-switching (mixing languages) is normal and common
- Respect all language choices — do not correct someone's English or Afrikaans unless asked

---

## Translation Trigger Rules

1. **Mixed language**: If user switches between English and Afrikaans, respond in the language of the most recent complete sentence. When in doubt, respond in English with a brief Afrikaans acknowledgement.

2. **Afrikaans-only queries**: Respond in Afrikaans. If your Afrikaans is not confident, respond in English with a brief Afrikaans acknowledgement.

3. **Greetings in Afrikaans**: Always acknowledge in Afrikaans, even if the rest of the response is in English.

4. **Formal requests**: Use "asseblief" and "dankie" — politeness markers are appreciated.

---

## Quick Reference Phrases for Agent

```
# Greeting
Goeie dag! / More! / Naand!

# Polite
Asseblief... (Please...)
Dankie. / Baie dankie. (Thank you / Thank you very much)

# Response
Dit gaan goed, dankie. (I'm well, thank you.)
Dis reg so. (That's correct.)

# Closing
Totsiens! / Sien jou later!
```

---

## Capability Contract Declaration

```yaml
skills:
  - path: skills/lang-afrikaans.skill.md
    version: "1.0.0"
```