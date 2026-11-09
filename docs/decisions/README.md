# Decisions

This folder holds Architecture Decision Records (ADRs): short notes
that capture one decision, why we made it, and what it costs us.
They help future readers (including future us) avoid re-litigating
settled questions.

## Index

No ADRs recorded yet. When the first decision lands, list it here
as `ADR-0001` with a one-line summary and its status.

## Format

One file per decision, named `adr-NNNN-short-title.md`:

```markdown
# ADR-NNNN: Short title

- Status: proposed | accepted | superseded by ADR-XXXX
- Date: YYYY-MM-DD

## Context

What situation forced a choice? Name the constraints.

## Decision

What did we choose, in one or two sentences?

## Consequences

What gets easier, what gets harder, and what would make us revisit
this?
```

Write an ADR when a choice is expensive to reverse, surprising
without context, or likely to be debated again. Small, obvious, or
easily reversed choices don't need one; a clear commit message is
enough.
