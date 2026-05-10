// hast-util-to-html ships JS only; keep a minimal module shape for tsc --noEmit (CI).
declare module "hast-util-to-html" {
  import type { Nodes } from "hast";
  export function toHtml(tree: Nodes, options?: Record<string, unknown>): string;
}
