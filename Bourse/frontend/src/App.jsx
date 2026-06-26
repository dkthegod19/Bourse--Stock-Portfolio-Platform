import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "./api.js";
import {
  formatINR,
  formatCompactINR,
  formatPct,
  formatSignedINR,
} from "./format.js";

const PORTFOLIO_KEY = "bourse.portfolioId";
const SEED_PAISE = 100000000; // ₹10,00,000
const REFRESH_MS = 8000;

/* ------------------------------------------------------------------ helpers */

function useToast() {
  const [toast, setToast] = useState(null);
  const timer = useRef(null);
  const show = useCallback((message, kind = "info") => {
    setToast({ message, kind });
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setToast(null), 4000);
  }, []);
  return { toast, show };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/* --------------------------------------------------------------------- App */

export default function App() {
  const [portfolioId, setPortfolioId] = useState(
    () => localStorage.getItem(PORTFOLIO_KEY) || ""
  );
  const [stocks, setStocks] = useState([]);
  const [trending, setTrending] = useState([]);
  const [portfolio, setPortfolio] = useState(null);
  const [booting, setBooting] = useState(true);
  const [query, setQuery] = useState("");
  const [sector, setSector] = useState("All");
  const [trade, setTrade] = useState(null); // { stock, side }
  const { toast, show } = useToast();

  // Create a portfolio on first run; remember it across reloads.
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        let id = portfolioId;
        if (!id) {
          const pf = await api.createPortfolio("My Portfolio", SEED_PAISE);
          id = pf.id;
          localStorage.setItem(PORTFOLIO_KEY, id);
          if (alive) setPortfolioId(id);
        }
      } catch (e) {
        show("Could not reach the server: " + e.message, "error");
      } finally {
        if (alive) setBooting(false);
      }
    })();
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [s, t] = await Promise.all([api.stocks(), api.trending(8)]);
      setStocks(s.stocks || []);
      setTrending(t.stocks || []);
      if (portfolioId) {
        const pf = await api.portfolio(portfolioId);
        setPortfolio(pf);
      }
    } catch {
      /* transient; next tick retries */
    }
  }, [portfolioId]);

  useEffect(() => {
    if (booting) return;
    refresh();
    const h = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(h);
  }, [booting, refresh]);

  const sectors = useMemo(() => {
    const set = new Set(stocks.map((s) => s.sector));
    return ["All", ...Array.from(set).sort()];
  }, [stocks]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return stocks.filter((s) => {
      const matchesQ =
        !q ||
        s.symbol.toLowerCase().includes(q) ||
        s.name.toLowerCase().includes(q);
      const matchesSector = sector === "All" || s.sector === sector;
      return matchesQ && matchesSector;
    });
  }, [stocks, query, sector]);

  const holdings = useMemo(() => {
    if (!portfolio) return [];
    const meta = Object.fromEntries(stocks.map((s) => [s.symbol, s]));
    return (portfolio.positions || []).map((p) => ({
      ...p,
      name: meta[p.instrument]?.name || p.instrument,
      sector: meta[p.instrument]?.sector || "",
    }));
  }, [portfolio, stocks]);

  async function submitTrade(side, stock, quantity) {
    try {
      const placed = await api.placeOrder({
        portfolio_id: portfolioId,
        side,
        instrument: stock.symbol,
        quantity,
        type: "market",
      });
      // Execution is async on the worker; poll the order briefly for its outcome.
      let final = placed;
      for (let i = 0; i < 12; i++) {
        await sleep(450);
        final = await api.order(placed.id);
        if (final.status !== "pending") break;
      }
      if (final.status === "rejected") {
        show(`Order rejected: ${final.reason || "unknown reason"}`, "error");
      } else if (final.status === "pending") {
        show("Order accepted — still processing.", "info");
      } else {
        const px = final.fill_price ? ` @ ${formatINR(final.fill_price)}` : "";
        show(
          `${side === "buy" ? "Bought" : "Sold"} ${quantity} ${stock.symbol}${px}`,
          "success"
        );
      }
      await refresh();
      return final;
    } catch (e) {
      show("Order failed: " + e.message, "error");
      throw e;
    }
  }

  const pnl = portfolio ? portfolio.total_value - SEED_PAISE : 0;

  if (booting) return <BootScreen />;

  return (
    <div className="min-h-full bg-[#0a0e1a] text-slate-100">
      <BackdropGlow />
      <div className="relative mx-auto max-w-7xl px-4 pb-24 pt-6 sm:px-6">
        <Header portfolio={portfolio} pnl={pnl} />

        <SummaryCards portfolio={portfolio} pnl={pnl} holdings={holdings} />

        {trending.length > 0 && (
          <Section
            title="Trending on NSE"
            subtitle="Today's biggest movers — live simulated prices"
          >
            <div className="flex gap-4 overflow-x-auto pb-2">
              {trending.map((s) => (
                <TrendingCard
                  key={s.symbol}
                  stock={s}
                  onTrade={(side) => setTrade({ stock: s, side })}
                />
              ))}
            </div>
          </Section>
        )}

        {holdings.length > 0 && (
          <Section title="Your Holdings">
            <HoldingsTable
              holdings={holdings}
              onTrade={(stock, side) => {
                const live = stocks.find((s) => s.symbol === stock.instrument);
                setTrade({ stock: live || stock, side });
              }}
            />
          </Section>
        )}

        <Section
          title="All Stocks"
          subtitle="Browse the NSE universe and place a trade"
        >
          <Controls
            query={query}
            setQuery={setQuery}
            sector={sector}
            setSector={setSector}
            sectors={sectors}
          />
          <StockGrid
            stocks={filtered}
            onTrade={(stock, side) => setTrade({ stock, side })}
          />
        </Section>
      </div>

      {trade && (
        <TradeModal
          stock={trade.stock}
          side={trade.side}
          cash={portfolio?.cash || 0}
          held={
            portfolio?.positions?.find(
              (p) => p.instrument === trade.stock.symbol
            )?.quantity || 0
          }
          onClose={() => setTrade(null)}
          onSubmit={submitTrade}
        />
      )}

      <Toast toast={toast} />
    </div>
  );
}

/* ----------------------------------------------------------------- chrome */

function BackdropGlow() {
  return (
    <div className="pointer-events-none fixed inset-0 overflow-hidden">
      <div className="absolute -top-40 left-1/4 h-96 w-96 rounded-full bg-indigo-600/20 blur-[120px]" />
      <div className="absolute -top-20 right-10 h-80 w-80 rounded-full bg-emerald-500/10 blur-[120px]" />
    </div>
  );
}

function BootScreen() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[#0a0e1a] text-slate-300">
      <div className="text-center">
        <div className="mx-auto mb-4 h-10 w-10 animate-spin rounded-full border-2 border-slate-700 border-t-indigo-400" />
        <p className="text-sm tracking-wide text-slate-400">
          Setting up your trading desk…
        </p>
      </div>
    </div>
  );
}

function Header({ portfolio, pnl }) {
  const up = pnl >= 0;
  return (
    <header className="mb-8 flex flex-wrap items-center justify-between gap-4">
      <div className="flex items-center gap-3">
        <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-emerald-400 text-lg font-extrabold text-slate-900">
          ₹
        </div>
        <div>
          <h1 className="text-xl font-extrabold tracking-tight">Bourse</h1>
          <p className="text-xs text-slate-400">Indian markets · paper trading</p>
        </div>
      </div>
      {portfolio && (
        <div className="text-right">
          <p className="text-xs uppercase tracking-wide text-slate-400">
            Portfolio value
          </p>
          <p className="text-2xl font-bold tabular-nums">
            {formatINR(portfolio.total_value)}
          </p>
          <p
            className={`text-xs font-semibold tabular-nums ${
              up ? "text-emerald-400" : "text-rose-400"
            }`}
          >
            {formatSignedINR(pnl)} all-time
          </p>
        </div>
      )}
    </header>
  );
}

function SummaryCards({ portfolio, pnl, holdings }) {
  if (!portfolio) return null;
  const up = pnl >= 0;
  const cards = [
    { label: "Total Value", value: formatCompactINR(portfolio.total_value) },
    { label: "Cash Available", value: formatCompactINR(portfolio.cash) },
    { label: "Invested", value: formatCompactINR(portfolio.market_value) },
    { label: "Holdings", value: String(holdings.length) },
  ];
  return (
    <div className="mb-10 grid grid-cols-2 gap-3 sm:grid-cols-4">
      {cards.map((c) => (
        <div
          key={c.label}
          className="rounded-2xl border border-slate-800 bg-slate-900/50 p-4 backdrop-blur"
        >
          <p className="text-[11px] uppercase tracking-wide text-slate-400">
            {c.label}
          </p>
          <p className="mt-1 text-lg font-bold tabular-nums">{c.value}</p>
        </div>
      ))}
      <div
        className={`col-span-2 rounded-2xl border p-4 backdrop-blur sm:col-span-4 ${
          up
            ? "border-emerald-500/30 bg-emerald-500/5"
            : "border-rose-500/30 bg-rose-500/5"
        }`}
      >
        <div className="flex items-center justify-between">
          <p className="text-[11px] uppercase tracking-wide text-slate-400">
            All-time P&amp;L
          </p>
          <p
            className={`text-lg font-bold tabular-nums ${
              up ? "text-emerald-400" : "text-rose-400"
            }`}
          >
            {formatSignedINR(pnl)}
          </p>
        </div>
      </div>
    </div>
  );
}

function Section({ title, subtitle, children }) {
  return (
    <section className="mb-10 animate-fade-in">
      <div className="mb-4">
        <h2 className="text-lg font-bold tracking-tight">{title}</h2>
        {subtitle && <p className="text-sm text-slate-400">{subtitle}</p>}
      </div>
      {children}
    </section>
  );
}

/* ----------------------------------------------------------------- pieces */

function ChangeBadge({ pct }) {
  const up = pct >= 0;
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold tabular-nums ${
        up
          ? "bg-emerald-500/10 text-emerald-400"
          : "bg-rose-500/10 text-rose-400"
      }`}
    >
      <span>{up ? "▲" : "▼"}</span>
      {formatPct(pct)}
    </span>
  );
}

function TrendingCard({ stock, onTrade }) {
  const up = stock.change_pct >= 0;
  return (
    <div className="min-w-[220px] flex-shrink-0 rounded-2xl border border-slate-800 bg-slate-900/60 p-4 backdrop-blur transition hover:border-slate-700">
      <div className="flex items-start justify-between">
        <div>
          <p className="font-bold">{stock.symbol}</p>
          <p className="max-w-[130px] truncate text-xs text-slate-400">
            {stock.name}
          </p>
        </div>
        <ChangeBadge pct={stock.change_pct} />
      </div>
      <p className="mt-3 text-xl font-bold tabular-nums">
        {formatINR(stock.price)}
      </p>
      <div className="mt-3 flex gap-2">
        <button
          onClick={() => onTrade("buy")}
          className="flex-1 rounded-lg bg-emerald-500 py-1.5 text-sm font-semibold text-slate-900 transition hover:bg-emerald-400"
        >
          Buy
        </button>
        <button
          onClick={() => onTrade("sell")}
          className="flex-1 rounded-lg border border-slate-700 py-1.5 text-sm font-semibold text-slate-200 transition hover:bg-slate-800"
        >
          Sell
        </button>
      </div>
      <span className="sr-only">{up ? "up" : "down"}</span>
    </div>
  );
}

function Controls({ query, setQuery, sector, setSector, sectors }) {
  return (
    <div className="mb-4 flex flex-wrap items-center gap-3">
      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search RELIANCE, Infosys…"
        className="w-full max-w-xs rounded-xl border border-slate-800 bg-slate-900/60 px-4 py-2 text-sm outline-none transition placeholder:text-slate-500 focus:border-indigo-500"
      />
      <div className="flex flex-wrap gap-2">
        {sectors.map((s) => (
          <button
            key={s}
            onClick={() => setSector(s)}
            className={`rounded-full px-3 py-1 text-xs font-medium transition ${
              sector === s
                ? "bg-indigo-500 text-white"
                : "border border-slate-800 text-slate-400 hover:text-slate-200"
            }`}
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

function StockGrid({ stocks, onTrade }) {
  if (stocks.length === 0) {
    return (
      <p className="rounded-xl border border-slate-800 bg-slate-900/40 p-8 text-center text-sm text-slate-400">
        No stocks match your search.
      </p>
    );
  }
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {stocks.map((s) => (
        <div
          key={s.symbol}
          className="group rounded-2xl border border-slate-800 bg-slate-900/50 p-4 backdrop-blur transition hover:border-slate-600"
        >
          <div className="flex items-start justify-between">
            <div>
              <div className="flex items-center gap-2">
                <p className="font-bold">{s.symbol}</p>
                <span className="rounded bg-slate-800 px-1.5 py-0.5 text-[10px] uppercase text-slate-400">
                  {s.exchange}
                </span>
              </div>
              <p className="max-w-[160px] truncate text-xs text-slate-400">
                {s.name}
              </p>
              <p className="mt-1 text-[11px] text-slate-500">{s.sector}</p>
            </div>
            <div className="text-right">
              <p className="text-lg font-bold tabular-nums">
                {formatINR(s.price)}
              </p>
              <div className="mt-1 flex justify-end">
                <ChangeBadge pct={s.change_pct} />
              </div>
            </div>
          </div>
          <div className="mt-4 flex gap-2 opacity-90">
            <button
              onClick={() => onTrade(s, "buy")}
              className="flex-1 rounded-lg bg-emerald-500 py-1.5 text-sm font-semibold text-slate-900 transition hover:bg-emerald-400"
            >
              Buy
            </button>
            <button
              onClick={() => onTrade(s, "sell")}
              className="flex-1 rounded-lg border border-slate-700 py-1.5 text-sm font-semibold text-slate-200 transition hover:bg-slate-800"
            >
              Sell
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}

function HoldingsTable({ holdings, onTrade }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/50 backdrop-blur">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-800 text-left text-xs uppercase tracking-wide text-slate-400">
            <th className="px-4 py-3">Stock</th>
            <th className="px-4 py-3 text-right">Qty</th>
            <th className="px-4 py-3 text-right">Price</th>
            <th className="px-4 py-3 text-right">Value</th>
            <th className="px-4 py-3 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {holdings.map((h) => (
            <tr
              key={h.instrument}
              className="border-b border-slate-800/60 last:border-0"
            >
              <td className="px-4 py-3">
                <p className="font-semibold">{h.instrument}</p>
                <p className="text-xs text-slate-400">{h.name}</p>
              </td>
              <td className="px-4 py-3 text-right tabular-nums">{h.quantity}</td>
              <td className="px-4 py-3 text-right tabular-nums">
                {formatINR(h.price)}
              </td>
              <td className="px-4 py-3 text-right font-semibold tabular-nums">
                {formatINR(h.market_value)}
              </td>
              <td className="px-4 py-3">
                <div className="flex justify-end gap-2">
                  <button
                    onClick={() => onTrade(h, "buy")}
                    className="rounded-lg bg-emerald-500/90 px-3 py-1 text-xs font-semibold text-slate-900 hover:bg-emerald-400"
                  >
                    Buy
                  </button>
                  <button
                    onClick={() => onTrade(h, "sell")}
                    className="rounded-lg border border-slate-700 px-3 py-1 text-xs font-semibold hover:bg-slate-800"
                  >
                    Sell
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/* ------------------------------------------------------------ trade modal */

function TradeModal({ stock, side: initialSide, cash, held, onClose, onSubmit }) {
  const [side, setSide] = useState(initialSide);
  const [qty, setQty] = useState(1);
  const [busy, setBusy] = useState(false);

  const price = stock.price;
  const cost = qty * price;
  const maxBuy = Math.floor(cash / price) || 0;
  const overBuy = side === "buy" && cost > cash;
  const overSell = side === "sell" && qty > held;
  const invalid = qty <= 0 || overBuy || overSell;

  async function go() {
    if (invalid || busy) return;
    setBusy(true);
    try {
      await onSubmit(side, stock, qty);
      onClose();
    } catch {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 p-0 backdrop-blur-sm sm:items-center sm:p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md animate-fade-in rounded-t-3xl border border-slate-800 bg-slate-900 p-6 shadow-2xl sm:rounded-3xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-5 flex items-start justify-between">
          <div>
            <p className="text-lg font-bold">{stock.symbol}</p>
            <p className="text-xs text-slate-400">{stock.name}</p>
          </div>
          <div className="text-right">
            <p className="text-lg font-bold tabular-nums">{formatINR(price)}</p>
            {typeof stock.change_pct === "number" && (
              <ChangeBadge pct={stock.change_pct} />
            )}
          </div>
        </div>

        <div className="mb-5 grid grid-cols-2 gap-2 rounded-xl bg-slate-800/60 p-1">
          {["buy", "sell"].map((s) => (
            <button
              key={s}
              onClick={() => setSide(s)}
              className={`rounded-lg py-2 text-sm font-semibold capitalize transition ${
                side === s
                  ? s === "buy"
                    ? "bg-emerald-500 text-slate-900"
                    : "bg-rose-500 text-white"
                  : "text-slate-400 hover:text-slate-200"
              }`}
            >
              {s}
            </button>
          ))}
        </div>

        <label className="mb-1 block text-xs uppercase tracking-wide text-slate-400">
          Quantity (shares)
        </label>
        <div className="mb-2 flex items-center gap-2">
          <button
            onClick={() => setQty((q) => Math.max(1, q - 1))}
            className="h-10 w-10 rounded-lg border border-slate-700 text-lg font-bold hover:bg-slate-800"
          >
            −
          </button>
          <input
            type="number"
            min="1"
            value={qty}
            onChange={(e) =>
              setQty(Math.max(1, parseInt(e.target.value || "1", 10)))
            }
            className="h-10 flex-1 rounded-lg border border-slate-700 bg-slate-800 px-3 text-center text-lg font-semibold tabular-nums outline-none focus:border-indigo-500"
          />
          <button
            onClick={() => setQty((q) => q + 1)}
            className="h-10 w-10 rounded-lg border border-slate-700 text-lg font-bold hover:bg-slate-800"
          >
            +
          </button>
        </div>

        <div className="mb-5 flex items-center justify-between text-xs text-slate-400">
          <span>
            {side === "buy"
              ? `Cash: ${formatINR(cash)} · max ${maxBuy}`
              : `Held: ${held} shares`}
          </span>
          <button
            onClick={() =>
              setQty(side === "buy" ? Math.max(1, maxBuy) : Math.max(1, held))
            }
            className="font-semibold text-indigo-400 hover:text-indigo-300"
          >
            Max
          </button>
        </div>

        <div className="mb-5 flex items-center justify-between rounded-xl border border-slate-800 bg-slate-800/40 px-4 py-3">
          <span className="text-sm text-slate-400">Estimated total</span>
          <span className="text-lg font-bold tabular-nums">
            {formatINR(cost)}
          </span>
        </div>

        {invalid && qty > 0 && (
          <p className="mb-3 text-xs font-medium text-rose-400">
            {overBuy
              ? "Not enough cash for this order."
              : "You don't hold that many shares."}
          </p>
        )}

        <div className="flex gap-3">
          <button
            onClick={onClose}
            className="flex-1 rounded-xl border border-slate-700 py-3 text-sm font-semibold hover:bg-slate-800"
          >
            Cancel
          </button>
          <button
            onClick={go}
            disabled={invalid || busy}
            className={`flex-1 rounded-xl py-3 text-sm font-bold transition disabled:opacity-40 ${
              side === "buy"
                ? "bg-emerald-500 text-slate-900 hover:bg-emerald-400"
                : "bg-rose-500 text-white hover:bg-rose-400"
            }`}
          >
            {busy
              ? "Placing…"
              : `${side === "buy" ? "Buy" : "Sell"} ${qty} ${stock.symbol}`}
          </button>
        </div>
        <p className="mt-3 text-center text-[11px] text-slate-500">
          Market order · executed asynchronously · T+1 settlement
        </p>
      </div>
    </div>
  );
}

/* ----------------------------------------------------------------- toast */

function Toast({ toast }) {
  if (!toast) return null;
  const styles = {
    success: "border-emerald-500/40 bg-emerald-500/10 text-emerald-300",
    error: "border-rose-500/40 bg-rose-500/10 text-rose-300",
    info: "border-slate-700 bg-slate-800 text-slate-200",
  };
  return (
    <div className="fixed bottom-6 left-1/2 z-[60] w-[90%] max-w-md -translate-x-1/2 animate-fade-in">
      <div
        className={`rounded-xl border px-4 py-3 text-sm font-medium shadow-xl backdrop-blur ${
          styles[toast.kind] || styles.info
        }`}
      >
        {toast.message}
      </div>
    </div>
  );
}
