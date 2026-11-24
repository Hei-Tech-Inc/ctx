import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js";
import { z } from "zod";

// Only this published static asset is readable through the documentation service.
async function loadChapters(env, origin) {
  const response = await env.ASSETS.fetch(new URL("/guide.html", origin).href);
  if (!response.ok) throw new Error("The published guide is unavailable.");
  const chapters = [];
  let current;
  const rewriter = new HTMLRewriter().on("#reading article", {
    element(element) {
      current = { id: element.getAttribute("id"), title: "", blocks: [] };
      chapters.push(current);
    },
  });
  for (const tag of ["h2", "h3", "p", "code"]) {
    rewriter.on("#reading article " + tag, {
      element() {
        current.blocks.push({ tag, value: "" });
      },
      text(chunk) {
        const block = current.blocks[current.blocks.length - 1];
        block.value += chunk.text;
      },
    });
  }
  await rewriter.transform(response).arrayBuffer();
  for (const chapter of chapters) {
    for (const block of chapter.blocks) {
      block.value = block.value.replace(
        /&(lt|gt|quot|amp|#39);/g,
        (_, key) =>
          ({ lt: "<", gt: ">", quot: '"', amp: "&", "#39": "'" })[key],
      );
      if (block.tag !== "code")
        block.value = block.value.replace(/\s+/g, " ").trim();
      if (block.tag === "h2") chapter.title = block.value;
    }
  }
  return chapters;
}

export async function onRequest({ request, env }) {
  if (request.method !== "POST")
    return new Response("Use an MCP client with Streamable HTTP.", {
      status: 405,
      headers: { Allow: "POST" },
    });
  const origin = new URL(request.url).origin;
  const browserOrigin = request.headers.get("Origin");
  if (browserOrigin && browserOrigin !== origin)
    return new Response("Cross-origin browser request denied", { status: 403 });
  const server = new McpServer({ name: "ctx-docs", version: "1.0.0" });
  const readOnly = {
    readOnlyHint: true,
    destructiveHint: false,
    openWorldHint: false,
  };
  server.registerTool(
    "search_guides",
    {
      description:
        "Find public ctx guide chapters by command, feature, or keyword. Returns chapter IDs and links for read_guide.",
      inputSchema: { query: z.string().trim().min(1).max(200) },
      annotations: readOnly,
    },
    async ({ query }) => {
      const words = query.toLowerCase().split(/\s+/);
      const chapters = await loadChapters(env, origin);
      const matches = chapters.filter(({ blocks }) => {
        const text = blocks
          .map((block) => block.value)
          .join(" ")
          .toLowerCase();
        return words.every((word) => text.includes(word));
      });
      return {
        content: [
          {
            type: "text",
            text:
              matches
                .map(
                  (c) =>
                    c.id +
                    " — " +
                    c.title +
                    "\n" +
                    origin +
                    "/guide.html#" +
                    c.id,
                )
                .join("\n\n") || "No matching chapters.",
          },
        ],
      };
    },
  );
  server.registerTool(
    "read_guide",
    {
      description:
        "Read one published ctx documentation chapter by its ID from search_guides.",
      inputSchema: { id: z.string().regex(/^[a-z0-9-]{1,64}$/) },
      annotations: readOnly,
    },
    async ({ id }) => {
      const chapter = (await loadChapters(env, origin)).find(
        (c) => c.id === id,
      );
      if (!chapter)
        return {
          isError: true,
          content: [
            { type: "text", text: "Unknown chapter. Use search_guides." },
          ],
        };
      return {
        content: [
          {
            type: "text",
            text: chapter.blocks
              .map((b) =>
                b.tag === "code" ? "\n```sh\n" + b.value + "\n```" : b.value,
              )
              .join("\n\n"),
          },
        ],
      };
    },
  );
  const transport = new WebStandardStreamableHTTPServerTransport({
    sessionIdGenerator: undefined,
    enableJsonResponse: true,
  });
  try {
    await server.connect(transport);
    return await transport.handleRequest(request);
  } finally {
    await server.close();
  }
}
