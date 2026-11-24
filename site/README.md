# ctx documentation on Cloudflare Pages

The public site serves static assets from `dist/`. Its Pages Function at `/mcp` uses Streamable HTTP and has two read-only tools, `search_guides` and `read_guide`. Both read the same published guide as the website. The function has no account secrets, client profiles, or Kubernetes bindings.

From this directory, install dependencies with `npm ci`. Run locally with `npx wrangler@4 pages dev dist`. Deploy a preview with `npx wrangler@4 pages deploy dist --project-name ctx-workspace-guide --branch preview`. Publish to production by selecting `--branch main`. Use an authenticated Cloudflare account with Pages write access.

To test a local or deployed service, run `node test-mcp.js https://ctx-workspace-guide.pages.dev/mcp`.

The separate Go-based operations server is registered locally using `ctx mcp install claude` or `ctx mcp install cursor`. It needs a client workspace, local tools and a configured credential provider.
