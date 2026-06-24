# `mm run` model selection — design + model eval (2026-06-24)

Decision record for how `mm run` picks a model, and the head-to-head that justified the default.

## How model selection works (as built)

`mm run --model <X>` resolves `X` then passes **explicit `--provider` + `--model`** to `hermes chat`:

- **Aliases:** `glm`→`zai/glm-5.2`, `gemini`/`flash`→`gemini/gemini-3.5-flash`, `deepseek`→`deepseek/deepseek-v4-pro`, `sonnet`→`anthropic/claude-sonnet-4.6`, `opus`→`anthropic/claude-opus-4.8`.
- **`provider/model`** or bare model also accepted.
- **Default** = `$MM_RUN_MODEL` (loaded from `~/.mm/.env`) → built-in `gemini/gemini-3.5-flash`. Currently set to `glm` (see decision).
- **`--max-turns N`** passes through to `hermes chat` (Hermes default 90 is *per conversation turn*; a `-q` one-shot is a single turn, so long sweeps need a higher cap — no global config edit needed).
- **Pre-flight auth guard:** hard-fails if the resolved provider isn't authed, rather than letting Hermes silently fall back to another model.

### Two bugs this fixed
1. `mm run --model` was a **no-op**: it set only `HERMES_INFERENCE_MODEL`, which the `chat` subcommand ignores (only `-z/--oneshot`/`--tui` read it). Every run silently used the config default. Now it passes real flags.
2. The `provider/model` prefix does **not** auto-resolve the provider on the `chat` path (`zai/glm-5.2` → silently ran on gemini→deepseek). `--provider` must be explicit. mm splits and passes it.

## The eval — GLM-5.2 vs Gemini-3.5-flash vs DeepSeek-v4-pro

Same task (sweep `specs/`, archive done / split partial, verify against code, commit), same tuned prompt, same repo (crm-v2, 12 specs). Verdicts adjudicated against the actual code.

| Spec | Truth | Gemini | DeepSeek | GLM |
|---|---|---|---|---|
| referrals | DONE | ✅ | ❌ false-open | ✅ |
| prospect-research | DONE | ✅ | ❌ false-open | ✅ |
| agent-first-capture | PARTIAL | ❌ over-archived | ✅ | ✅ |
| cli-capture-ergonomics | PARTIAL | ❌ over-archived | ✅ | ✅ |
| active-crm (master spec) | leave live | ❌ archived | ❌ archived | ✅ left live |
| **contested score** | | **2/5** | **2/5** | **5/5** |

- **GLM** got every contested call right, incl. refusing to archive the load-bearing master spec.
- **Gemini** errs by **over-archiving** (read deeply — 74 calls — but mis-judged specs whose own code says "phase 2.5e.1 spike, later phases pending"). Failure mode = precision/judgment, *not* lack of reading. Dangerous: hides open work.
- **DeepSeek** errs by **under-reading** (13–38 work-calls) → false-OPEN on done specs. Failure mode = depth/effort.

### Cost per run (crm-v2)
| Model | tokens in/out | calls | cost |
|---|---|---|---|
| DeepSeek-v4-pro | 3.70M / 18K | 38 | cheapest (pro price permanently reduced; ~pennies) |
| Gemini-3.5-flash | 8.69M / 8.8K | 74 | ~$0.5–1 (estimated) |
| GLM-5.2 | 6.55M / 30K | 56 | ~$1.7 (confirmed: $3.80 z.ai spend across 2 sweeps) |

Only GLM's cost is billing-confirmed (z.ai balance). Gemini/DeepSeek are public-rate estimates (separate accounts, no billing readout). DeepSeek is materially cheaper than the estimate above.

## Decision
**Default `MM_RUN_MODEL=glm`.** `mm run` is delegated, unbabysat work — reliability dominates, and GLM was the only model safe-ish without line-by-line review. The ~$1.5–2/run premium is trivial vs a confident wrong call. Override per-task: `--model gemini` (fast/cheap, will review), `--model deepseek` (bulk/narrow).

Caveat: still review any archiving/destructive sweep before merge — even GLM is one bad call away from hiding work. The reviewable-branch + SWEEP-REPORT workflow is load-bearing, not polish.

## Open questions / future work
- **Is the gap tunable or genuine? — ANSWERED (2026-06-24, knowledgebase-v1, strict conservative prompt, all 3 models).**
  - *Promptable:* Gemini's foundational-spec error. With explicit "never archive foundational specs," it correctly left `active-kb` LIVE (vs wrongly archiving crm-v2's master spec). Transferable.
  - *NOT promptable — Gemini's over-archiving:* even told "PARTIAL if ANY phase/sub-item is open" and reading 87 calls deep, it still archived `scholarly-retrieval` as DONE — the spec's own Phase 3.1 is "(next)", `CONVERT_URL` is in no `.env`, and no semantic re-rank exists (all confirmed in code). Reasoning ceiling, not a prompt gap.
  - *NOT promptable — DeepSeek's reliability:* despite explicit "your work is only saved if you commit," its kb run produced no committed branch at all. Nothing landed.
  - *GLM:* correct verdicts (incl. `scholarly-retrieval` PARTIAL→split) in 30 calls / 2.47M input — fewer/cheaper than Gemini's 87 / 7.36M. Accuracy is judgment, not brute-force reading.
  - **Conclusion:** prompt-tuning fixes gross errors (foundational specs, committing) but not the subtle precision gap. GLM genuinely reasons better on partial-completion nuance. Confirms `MM_RUN_MODEL=glm` for unbabysat work. (3 repos — infra/crm-v2/kb — all point the same way; robust but not exhaustive.)
- **Intuitive switching by requirement.** Proposed: semantic `--mode` sugar over the aliases —
  - `--mode quality` → glm (high-stakes, unattended)
  - `--mode fast` → gemini (quick, you'll review)
  - `--mode cheap` → deepseek (bulk, narrow)
  Or auto-pick from signals (task length, "audit/refactor/delete" keywords → quality; "list/rename/bump" → cheap). Not built — decide whether `--mode` sugar is worth it over just remembering 3 aliases.
