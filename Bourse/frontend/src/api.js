// Thin API client. In production the Go server serves this app and the API from
// the same origin, so paths are relative. In dev, Vite proxies /v1 to :8080.

const API_KEY = "demo";

async function req(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: {
      "Content-Type": "application/json",
      "X-API-Key": API_KEY,
      ...(opts.headers || {}),
    },
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body && body.error) msg = body.error;
    } catch {
      /* ignore non-JSON error bodies */
    }
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  if (res.status === 204) return null;
  return res.json();
}

export const api = {
  trending: (limit = 6) => req(`/v1/stocks/trending?limit=${limit}`),
  stocks: () => req("/v1/stocks"),
  createPortfolio: (name, seedPaise) =>
    req("/v1/portfolios", {
      method: "POST",
      body: JSON.stringify({ name, seed_paise: seedPaise }),
    }),
  portfolio: (id) => req(`/v1/portfolios/${id}`),
  history: (id) => req(`/v1/portfolios/${id}/history`),
  placeOrder: (order) =>
    req("/v1/orders", { method: "POST", body: JSON.stringify(order) }),
  order: (id) => req(`/v1/orders/${id}`),
};
