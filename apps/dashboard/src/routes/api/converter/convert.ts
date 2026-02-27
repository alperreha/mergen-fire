import {
  ConverterBusyError,
  ConverterExecError,
  listConvertedImages,
  runConversion,
} from "~/server/converter";

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}

function parseImageField(body: unknown): string {
  if (!body || typeof body !== "object") {
    return "";
  }
  const maybeImage = (body as { image?: unknown }).image;
  if (typeof maybeImage !== "string") {
    return "";
  }
  return maybeImage.trim();
}

export async function POST(event: { request: Request }) {
  let payload: unknown;
  try {
    payload = await event.request.json();
  } catch {
    return jsonResponse(
      {
        error: "bad_request",
        message: "request body must be valid JSON",
      },
      400
    );
  }

  const image = parseImageField(payload);
  if (!image) {
    return jsonResponse(
      {
        error: "bad_request",
        message: "image is required",
      },
      400
    );
  }

  try {
    const conversion = await runConversion(image);
    const images = await listConvertedImages();
    return jsonResponse(
      {
        status: "completed",
        conversion,
        images,
      },
      202
    );
  } catch (error) {
    if (error instanceof ConverterBusyError) {
      return jsonResponse(
        {
          error: "converter_busy",
          message: error.message,
          activeConversion: error.activeConversion,
        },
        409
      );
    }

    if (error instanceof ConverterExecError) {
      return jsonResponse(
        {
          error: "conversion_failed",
          message: error.message,
          details: error.details,
        },
        500
      );
    }

    return jsonResponse(
      {
        error: "conversion_failed",
        message: error instanceof Error ? error.message : "converter command failed",
      },
      500
    );
  }
}
