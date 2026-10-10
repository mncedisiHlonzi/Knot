# Knot — Project

## What Knot is

Knot is a **multilingual social knowledge and cultural storytelling platform**. It is a
global, visual library of stories, micro-facts, and heritage in which people adapt and
retell content for their own communities — so that the people closest to a place or a
topic are heard first, without anyone else being silenced.

Knot is not a broadcast feed and not a machine-translation layer. It is a network of
people exchanging knowledge across languages.

## Mission

Tie human stories, languages, and truths together by making cross-language knowledge
exchange a first-class, human activity.

## North Star

> **The number of conversations that cross a language boundary and come back.**

Success is not raw reach or impressions. It is a completed loop: content created in one
language, adapted by a human in another language, and responded to by people in both.

## Core loop

Knot has one core loop, described at two levels: a **public** four-verb description that
anyone can repeat, and the **internal** step-by-step view that product and engineering
work against. Both describe the same loop.

### Public loop — four verbs

> **Share → Adapt → Connect → Build Understanding**

1. **Share** — someone contributes a story, micro-fact, or piece of heritage in their own
   language.
2. **Adapt** — someone fluent in both languages retells it for their own community.
3. **Connect** — people in both languages respond to and talk with each other.
4. **Build Understanding** — the conversation crosses the language boundary and comes
   back, leaving both sides knowing more than they did.

### Internal loop — the canonical eight steps

1. Create Story
2. Discover
3. Human Adaptation
4. Language-specific Conversation
5. Comment Bridging
6. Cross-language Conversation
7. Return to Original Language
8. Deeper Understanding

This is the full canonical loop. The public-facing four-verb loop above is a simplification of the same journey, not a different process.

Every feature Knot builds must strengthen this loop.

### A second loop — asking

> **Ask → Answer → Know**

Curious Inquiries adds a second loop that runs beside the storytelling one rather than
replacing it:

1. **Ask** — someone asks a question about a place, naming it.
2. **Answer** — the people Rooted in that place answer publicly, in their own words and
   under their own names.
3. **Know** — an answer is not a conclusion. Questions stay open, so a place keeps
   accumulating what its people know.

It strengthens the same principle the four verbs do — local voices first, conversation
rather than broadcast — by making the question the unit of exchange. The two loops do not
merge: a story is authored content about a place, an inquiry is a question asked *of* the
people there, and the composer for each stays separate (KNOT-ADR-055, KNOT-ADR-058).

## Guiding principles

- **Human adaptation over machine translation.** People, not models, carry meaning.
- **Local voices first.** The people closest to a place are heard first.
- **No silencing.** Adapting a story never erases the original.
- **Conversation, not broadcast.**
- **Build order: Human Network First → Data Foundation Second → Intelligence Third.**

## Scope of this repository

This repository is the engineering home of Knot. It currently contains the foundation:
project layout, tooling, CI, and documentation. Product features are implemented only
through approved tasks (see `docs/ENGINEERING_WORKFLOW.md`).
