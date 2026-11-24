# Reviewing Matches

Profile unification merges two profiles on its own only when the evidence is
clear. A pair that is likely, but not certainly, the same customer is held
back as a **review task** for an administrator to approve or reject. This is
the human in the loop: CDD proposes, a person decides.

How a pair ends up in review is in
[How Unification Works](../concepts/how-unification-works.md#why-a-pair-is-in-review).
This guide is about working the queue.

---

## Deciding what reaches the queue

Three settings of the organization, read and changed with `GET` and
`PATCH /config` (scopes `admin_config:view` / `admin_config:update`), decide
what merges, what waits and what is left alone:

| Setting | Default | Meaning |
|---|---|---|
| `auto_merge_enabled` | on | Whether a pair that scores high enough merges on its own. Off, every such pair becomes a review task |
| `auto_merge_threshold` | 1.0 | The score at or above which a pair merges automatically |
| `manual_review_threshold` | 0.75 | The score at or above which a pair becomes a review task; below it, nothing happens |

Even above `auto_merge_threshold`, a pair is held for review when the evidence
conflicts — two values that must agree and do not, most other rules
disagreeing, or a single weak or common value carrying the match. With
`auto_merge_enabled` off, every match is reviewed by a person.

A sub-organization uses its primary organization's settings.

---

## Listing the queue

```
GET /identity-resolution/review-tasks
GET /identity-resolution/review-tasks?profile_id=<profile ID>
```

Scope `identity_resolution:view`. Only pending tasks are listed, `page_size`
at a time (default 5, at most 200). Each task names the two profiles —
`incoming_profile_id` and `candidate_profile_id` — and carries:

- `score_breakdown`: what each rule scored for the pair.
- `explanation.rules`: every active rule, its score and its verdict (`AGREE`,
  `INCONCLUSIVE`, `DISAGREE` or `UNKNOWN`).
- `explanation.reasons`: why the pair was held, as codes such as
  `CONFLICTING_EVIDENCE`, `DISCRIMINATING_MISMATCH` or `AUTO_MERGE_DISABLED`.
  The [full list](../concepts/how-unification-works.md#why-a-pair-is-in-review)
  says what each means.

Fetch the two profiles with `GET /profiles/{profileId}` to compare them side by
side.

---

## Approving or rejecting

```
POST /identity-resolution/review-tasks/{taskId}/resolve
```

```json
{ "decision": "APPROVED", "notes": "Same customer, new email address" }
```

Scope `identity_resolution:update`. `decision` is `APPROVED` or `REJECTED`;
`notes` is optional and kept on the task with who resolved it and when.

| Decision | What happens |
|---|---|
| `APPROVED` | The two profiles are merged, as an automatic merge would have. The merge records the rule that drove it, so the profile shows why it was combined |
| `REJECTED` | The profiles stay apart, and CDD remembers that they are different people |

Resolving a task that is already resolved is a 409.

Approving a task is also a 409 when either of its profiles was merged with
another profile after the task was raised. The task is marked `CANCELLED`
rather than applied to the merged result, because that holds data the task did
not show. Both profiles are evaluated again, and a new task is raised if the
match still needs a person.

### Rejected pairs stay rejected

A rejection is a statement about identity, so the pair does not come back
because something unrelated on either profile changed. It returns to the queue
only when the evidence is genuinely stronger than what the administrator
rejected — a clearly higher similarity, or a rule agreeing that did not agree
before — and the task then carries the reason `REJECTION_RECONSIDERED`. See
[Rejections](../concepts/unification-rules.md#rejections).

### Tasks that resolve themselves

When a merge happens — automatically or by approving another task — pending
tasks involving the merged profile are marked `CANCELLED`, and the profiles
are evaluated again against the merged result. A new task is raised if the
match still needs a person.
