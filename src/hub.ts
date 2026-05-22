/**
 * Legacy entry point for `import { hubApi } from '../hub'`. The real
 * implementation lives in `src/http/client.ts` `hub()`. Kept as a thin
 * re-export so existing call sites don't churn in the same commit as
 * the http/ consolidation. Callers can switch to `import { hub } from
 * '../http/client'` at their leisure.
 */

export { hub as hubApi } from './http/client';
