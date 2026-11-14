export class APIError<TProblem = unknown> extends Error {
  readonly problem: TProblem;
  readonly requestId: string | null;
  readonly status: number;

  constructor(response: Response, problem: TProblem) {
    super(`Tacit API request failed with status ${response.status}`);
    this.name = "APIError";
    this.problem = problem;
    this.requestId = response.headers.get("X-Request-ID");
    this.status = response.status;
  }
}

export type ErrorType<TProblem> = APIError<TProblem>;
export type BodyType<TBody> = TBody;

export async function apiFetch<T>(url: string, options: RequestInit): Promise<T> {
  const headers = new Headers(options.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json, application/problem+json");
  const response = await fetch(url, {
    ...options,
    credentials: "same-origin",
    headers,
  });
  const body = response.status === 204 ? undefined : await readBody(response);
  if (!response.ok) throw new APIError(response, body);
  return body as T;
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text.length === 0) return undefined;
  const contentType = response.headers.get("Content-Type") ?? "";
  if (contentType.includes("json")) {
    const value: unknown = JSON.parse(text);
    return value;
  }
  return text;
}
