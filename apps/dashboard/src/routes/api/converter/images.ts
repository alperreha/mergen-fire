import { listConvertedImages } from "~/server/converter";

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}

export async function GET() {
  try {
    const payload = await listConvertedImages();
    return jsonResponse(payload);
  } catch (error) {
    return jsonResponse(
      {
        error: "list_failed",
        message: error instanceof Error ? error.message : "failed to list converted images",
      },
      500
    );
  }
}
