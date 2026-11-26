type OpenUpstreamStream = {
  target: URL;
  headers: Headers;
  signal: AbortSignal;
  connectTimeout?: number;
};

export async function openUpstreamStream({
  target, headers, signal, connectTimeout = 15_000,
}: OpenUpstreamStream): Promise<Response> {
  const establishing = new AbortController();
  const timer = setTimeout(() => establishing.abort(), connectTimeout);
  try {
    return await fetch(target, {
      cache: "no-store",
      headers,
      method: "GET",
      redirect: "manual",
      signal: AbortSignal.any([signal, establishing.signal]),
    });
  } finally {
    clearTimeout(timer);
  }
}
