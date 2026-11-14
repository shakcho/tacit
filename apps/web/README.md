# Tacit web

The web application is a Next.js App Router workspace with Tailwind CSS and an owned, shadcn-style component layer under `src/components/ui`. Product routes are server components by default; small interactive islands opt into client rendering explicitly.

## Commands

- `pnpm dev` starts the Turbopack development server.
- `pnpm lint`, `pnpm typecheck`, and `pnpm test` run source checks and component tests.
- `pnpm build` creates the production standalone server.
- `pnpm test:e2e` exercises desktop, mobile, keyboard, accessibility, and visual smoke behavior against that production build.
- `pnpm check:environment` inspects source boundaries and the built browser chunks for private configuration.

Only variables named in `src/env/public.ts` may be read by browser code. Server-only configuration belongs in `src/env/server.ts` and must never use the `NEXT_PUBLIC_` prefix.
