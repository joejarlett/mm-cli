/**
 * Legacy entry point for `import { dispatch } from '../dispatcher'`.
 * The real implementation lives in `src/http/client.ts` `v2()`. Kept
 * as a thin re-export so existing call sites don't churn in the same
 * commit as the http/ consolidation. Callers can switch to `import
 * { v2 } from '../http/client'` at their leisure.
 */

export { v2 as dispatch, type V2Result as DispatchResult } from './http/client';
