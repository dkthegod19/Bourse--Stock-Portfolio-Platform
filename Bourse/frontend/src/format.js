// All money from the API is in paise (1 rupee = 100 paise). These helpers turn
// paise into Indian-formatted rupee strings (lakh/crore grouping via en-IN).

export function toRupees(paise) {
  return paise / 100;
}

// "₹1,23,456.78" — full precision with Indian digit grouping.
export function formatINR(paise, decimals = 2) {
  const v = toRupees(paise);
  return (
    "₹" +
    v.toLocaleString("en-IN", {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
    })
  );
}

// Compact form using lakh (L) / crore (Cr) for large headline numbers.
export function formatCompactINR(paise) {
  const v = toRupees(paise);
  const abs = Math.abs(v);
  if (abs >= 1e7) return "₹" + (v / 1e7).toFixed(2) + " Cr";
  if (abs >= 1e5) return "₹" + (v / 1e5).toFixed(2) + " L";
  return "₹" + v.toLocaleString("en-IN", { maximumFractionDigits: 0 });
}

export function formatPct(pct) {
  const sign = pct > 0 ? "+" : "";
  return sign + pct.toFixed(2) + "%";
}

export function formatSignedINR(paise) {
  const sign = paise > 0 ? "+" : paise < 0 ? "-" : "";
  return sign + formatINR(Math.abs(paise));
}
