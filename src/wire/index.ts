/**
 * Wire types — single source of truth for every HTTP/WS shape mm-cli
 * sends or receives. Imported by commands; consumed by the Go port spec
 * as the codegen / hand-translation target.
 *
 * Out of scope (already centralised in their own modules):
 *   - AuthState              → src/auth.ts
 *   - AgentCard et al        → src/agent-card.ts
 *   - AppManifest et al      → src/manifest.ts
 *   - DispatchResult         → src/dispatcher.ts
 */

export * from './hub';
export * from './agent';
