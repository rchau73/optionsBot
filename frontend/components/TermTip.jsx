"use client";

import InfoTip from "./InfoTip";
import { findTermTip } from "@/lib/tips";

/** A glossary term's short text and its risk and income line, behind an "i" (#666). */
export default function TermTip({ term }) {
  const tip = findTermTip(term);
  if (!tip) return null;
  return (
    <InfoTip label={`About ${term}`}>
      {tip.short} {tip.risk_income}
    </InfoTip>
  );
}
