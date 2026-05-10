/// <reference types="vite/client" />

// Pulled in via @polybet/views; that package has its own .d.ts for tsc there, but
// desktop typecheck:web compiles workspace sources without views/tsconfig includes.
declare module "hast-util-to-html" {
  export function toHtml(tree: unknown, options?: Record<string, unknown>): string;
}
