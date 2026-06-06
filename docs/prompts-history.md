# Prompts History Log

## Prompt #1

You are Golang expert and tasked with creating a money transfer service among digital wallet. First, read assignment instructions, chalk down execution plan, and seek approval. 

## Prompt #2 - Feedback

Include SonarQube CE container with Uber Golang style guide.
Use UUID v7 and make sure it is generated in domain layer and passed on.
Focus on building the solution inside-out approach using clean architecture and SOLID principles. While building each layer, commit code changes with commit message summarizing the change. Before commit, make sure code is linted, formatted, gets success from SonarQube, and has working functional tests.
Introduce a Make file with commands to build and test inside Docker Compose containers.
Update the plan and seek approval.

## Prompt #3 - Feedback

Include indexes created in the plan as well as database migration scripts. Use integer based status codes with constants mapped in Domain models.
Moreover, use git branch name "waseem". 
Furthermore, "slog" standard package for logging and log state transitions with idempotency-key and relevant identifiers as log fields. Log errors with short layer name.
Furthermore, use Test Driven Development approach and before making code changes, write a test which should fail initially, commit only test mentioning why test is failing. Next, complete the implementation in suitable layer(s) and adjust the test accordingly before committing layer changes.
Lastly, read documentation-first workflow and make a markdown doc for every commit answering sought questions.
Update the plan and seek approval.

---

Plan approved to avoid hallucinations and 20% credits consumed

---

## Review 

Next, using file ".github/copilot-instructions.md" review code base and create suitable fixes, once again do not push any code changes yet.

>>>
All done. Here's a summary of what was fixed against the copilot-instructions review criteria:

---

### Fixes committed (`db61a04`)

| Category | Issue | Fix |
|----------|-------|-----|
| **Correctness** | No idempotency conflict detection — key reuse with different params returned cached response silently | SHA-256 hash of `(fromWallet\|toWallet\|amount)` stored in `idempotency_records`; mismatch → `ErrDuplicateIdempotencyKey` → HTTP 409 |
| **Correctness** | `domain.CanTransition` defined but never called — state machine guard was dead code | Service now calls it before `UpdateStatus`; `ErrInvalidTransition` is now reachable |
| **Layer hygiene** | Repo constructors took `interface{}` and discarded it | All 4 constructors now accept `*slog.Logger`; repos use injected logger (consistent with service) |
| **Layer hygiene** | `idempotencyRepo.Get` returned `domain.ErrTransferNotFound` for a missing cache entry | Added `domain.ErrIdempotencyKeyNotFound`; used in the repo |
| **API ergonomics** | `TransferStatus` serialized as integer (`2`) in JSON | Added `MarshalJSON`/`UnmarshalJSON` — API now returns `"PROCESSED"` etc. |
| **Handler mapping** | No HTTP 409 for duplicate-key conflict | `writeServiceError` maps `ErrDuplicateIdempotencyKey` → 409 Conflict |
| **Schema** | `request_hash` column missing from `idempotency_records` | Migration 000003 adds it |

**Test count: domain 9 + handler 14 + service 14 = 37 unit tests, all passing** ✅
>>>
