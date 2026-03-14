---
name: coder
description: Repo-specific architecture and design guidance for planning, reviewing, and refactoring code in this repository. Use when Codex needs to choose or reject a GoF design pattern, enforce clean architecture boundaries, map responsibilities across transport/use case/domain/infra layers, evaluate folder structure, or apply KISS, YAGNI, Pareto, SOLID, and separation-of-concerns rules to keep a change minimal and coherent.
---

# Base

## Overview

Use the smallest design that solves the current problem and keeps boundaries explicit. Favor simple language, inward dependency flow, and minimal new abstractions.

## Workflow

1. State the concrete problem, current constraint, and affected boundary before proposing structure.
2. Apply the principle filter in this order:
- Narrow scope with Pareto.
- Simplify with KISS.
- Reject speculative work with YAGNI.
- Split mixed responsibilities with separation of concerns.
- Refactor only enough to satisfy SOLID, especially dependency inversion.
3. Read [references/design-patterns-flowchart.md](references/design-patterns-flowchart.md) when the change needs a reusable abstraction, object-creation strategy, or behavior/structure pattern choice.
4. Read [references/principles-clean-architecture.md](references/principles-clean-architecture.md) when the change affects module boundaries, dependency direction, orchestration, folder layout, or layer ownership.
5. Recommend the smallest viable change and explain why it is simpler than the obvious alternatives.

## Decision Rules

- Name the problem before naming a pattern.
- Add a GoF pattern only when it removes present duplication, branching complexity, or boundary confusion.
- Keep transport and UI thin.
- Keep orchestration in use cases.
- Keep business rules in domain entities or domain services.
- Keep framework, storage, and integration details in adapters or infra.
- Keep dependencies pointing inward.
- Treat folder examples as guidance, not as stronger rules than clear boundaries.

## Review Checklist

- Ask whether the change can stay local instead of introducing a new abstraction.
- Check whether each module has one dominant reason to change.
- Check whether interfaces are owned by the inner layer rather than the framework layer.
- Check whether the proposal makes testing easier instead of harder.
- Call out over-engineering plainly when a pattern or extra layer is unnecessary.

## Response Shape

- Give a concrete recommendation first.
- Tie the recommendation to the smallest set of principles that actually matter.
- If rejecting a pattern or layer, name the simpler alternative to use instead.
- If uncertainty remains, state the assumption that would change the recommendation.

## References

- [references/design-patterns-flowchart.md](references/design-patterns-flowchart.md): Load when selecting a GoF pattern for creation, structure, or behavior problems.
- [references/principles-clean-architecture.md](references/principles-clean-architecture.md): Load when reviewing dependency direction, layers, responsibilities, or repo structure.
