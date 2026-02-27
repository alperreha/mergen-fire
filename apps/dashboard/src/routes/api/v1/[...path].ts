type APIEventLike = {
  request: Request;
  params?: {
    path?: string;
  };
};

const DEFAULT_DAEMON_BASE = "http://127.0.0.1:8080";

function daemonBaseURL(): string {
  return (process.env.MERGEN_DAEMON_BASE || DEFAULT_DAEMON_BASE).trim();
}

function buildTargetURL(event: APIEventLike): string {
  const rawPath = event.params?.path || "";
  const normalizedPath = rawPath.split("/").filter(Boolean).join("/");
  const incomingURL = new URL(event.request.url);
  const query = incomingURL.search || "";
  return `${daemonBaseURL()}/v1/${normalizedPath}${query}`;
}

function forwardHeaders(input: Headers): Headers {
  const headers = new Headers();
  input.forEach((value, key) => {
    const lower = key.toLowerCase();
    if (lower === "host" || lower === "content-length" || lower === "connection") {
      return;
    }
    headers.set(key, value);
  });
  return headers;
}

async function proxyToDaemon(event: APIEventLike): Promise<Response> {
  const method = event.request.method.toUpperCase();
  const targetURL = buildTargetURL(event);
  const headers = forwardHeaders(event.request.headers);
  const body =
    method === "GET" || method === "HEAD" || method === "OPTIONS"
      ? undefined
      : await event.request.arrayBuffer();

  try {
    const upstream = await fetch(targetURL, {
      method,
      headers,
      body,
      redirect: "manual",
    });

    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: upstream.headers,
    });
  } catch (error) {
    return new Response(
      JSON.stringify({
        error: "daemon_unreachable",
        message: error instanceof Error ? error.message : "failed to reach daemon",
        target: targetURL,
      }),
      {
        status: 502,
        headers: {
          "content-type": "application/json; charset=utf-8",
          "cache-control": "no-store",
        },
      }
    );
  }
}

export async function GET(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function POST(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function PUT(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function PATCH(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function DELETE(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function OPTIONS(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}

export async function HEAD(event: APIEventLike): Promise<Response> {
  return proxyToDaemon(event);
}
