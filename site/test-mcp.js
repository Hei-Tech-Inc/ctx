import assert from "node:assert/strict";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const endpoint = new URL(process.argv[2]);
const client = new Client({ name: "ctx-docs-smoke", version: "1.0.0" });
try {
  await client.connect(new StreamableHTTPClientTransport(endpoint));
  assert.deepEqual((await client.listTools()).tools.map((t) => t.name).sort(), [
    "read_guide",
    "search_guides",
  ]);
  const search = await client.callTool({
    name: "search_guides",
    arguments: { query: "1Password" },
  });
  assert.match(search.content[0].text, /guide.html#enterprise/);
  const page = await client.callTool({
    name: "read_guide",
    arguments: { id: "mcp" },
  });
  assert.match(page.content[0].text, /ctx knowledge init acme/);
  assert.match(page.content[0].text, /operator|confirmation/);
  const missing = await client.callTool({
    name: "read_guide",
    arguments: { id: "missing" },
  });
  assert.equal(missing.isError, true);
  console.log(
    "MCP initialization, tool listing, search, chapter retrieval and unknown chapter passed.",
  );
} finally {
  await client.close();
}
