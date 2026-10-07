# Knot — Engineering Workflow

## Roles

| Role                       | Owns                                                              |
| -------------------------- | ----------------------------------------------------------------- |
| **Product / Project Manager** | Product direction, approved tasks, acceptance criteria, priorities |
| **Founder**                 | The repository, credentials, Git, machine, and final approvals     |
| **Engineer**                | Implementation of approved tasks against the local workspace       |

The engineer **does not** redefine scope or architecture, and **does not** commit or push
unless the founder explicitly asks.

## The loop

```
PM defines task  ─▶  Founder authorises  ─▶  Engineer implements  ─▶  Founder runs validation  ─▶  PM reviews
        ▲                                                                                              │
        └────────────────────────────  approve, or issue a correction task  ◀─────────────────────────┘
```

1. **PM** writes a task with an ID, objective, scope, non-goals, and acceptance criteria.
2. **Founder** authorises the task and controls Git.
3. **Engineer** implements only what the task approves, then reports.
4. **Founder** runs and reviews validation on the real machine.
5. **PM** approves or issues a **correction task**. Nothing is silently amended.

## Task format

Every task uses this shape:

```
TASK ID:      KNOT-<number>
TITLE:        <short name>
PHASE:        <roadmap phase>

OBJECTIVE     What outcome is wanted.
CONTEXT       Why now, and what to avoid.
SCOPE         Exactly what to build.
NON-GOALS     Explicitly what NOT to build.
ACCEPTANCE    Testable criteria.
VALIDATION    Exact commands to run.
REPORT FORMAT Required output structure.
```

## Rules of engagement

- **One task, one intent.** No opportunistic refactors.
- **No scope creep.** Anything unapproved becomes a new task.
- **Architecture changes require an ADR** in `docs/DECISIONS.md` before implementation.
- **No commits or pushes** by the engineer unless the founder explicitly asks.
- **Honest reporting.** Never claim validation that was not performed.
- **Clean room.** Legacy work is out of scope (see KNOT-ADR-001).
