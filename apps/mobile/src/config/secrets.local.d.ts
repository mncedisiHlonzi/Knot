/**
 * Type declaration for the gitignored `secrets.local.ts` (KNOT-ADR-027).
 *
 * This file is committed so TypeScript can resolve the `./secrets.local` module
 * on a fresh clone, before the developer has copied `secrets.example.ts` to
 * `secrets.local.ts`. It declares only the module's shape; the value is supplied
 * by the real local file at runtime. Metro still needs the real file to bundle,
 * so create it before running `npm start` (see docs/DEVELOPMENT.md, "Local
 * secrets setup (after cloning)").
 */
export declare const KNOT_MAPBOX_TOKEN: string;
