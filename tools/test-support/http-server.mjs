import { once } from "node:events";
import { createServer } from "node:http";

// Serve real HTTP responses from an in-memory map; unmatched routes return 404.
export async function serveFixtures(t, responses = new Map()) {
  const requests = [];
  const server = createServer(async (request, response) => {
    requests.push(request.url);
    try {
      const fixture = responses.get(request.url);
      const body =
        typeof fixture === "function" ? await fixture(request) : fixture;
      response.writeHead(body === undefined ? 404 : 200);
      response.end(body ?? "Fixture not found");
    } catch (error) {
      response.writeHead(500);
      response.end(error.message);
    }
  });
  t.after(() => new Promise((resolve) => server.close(resolve)));
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  return {
    responses,
    requests,
    baseUrl: `http://127.0.0.1:${server.address().port}`,
  };
}
