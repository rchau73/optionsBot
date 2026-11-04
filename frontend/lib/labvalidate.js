// The lab's check of a proposed setting value (#586, epic E9): may it be
// tried, and who must approve a new default. Everything comes from
// content/lab-settings.json, which cmd/paramdocs generates from the Go
// catalogue and the shipped configs (tests/lab_settings_test.go loads every
// bound through config.Load), and the tip from content/tips.json. Pure: it
// reads no file, writes nothing and applies nothing.
import labSettings from "@/content/lab-settings.json";
import { findSettingTip } from "@/lib/tips";

/** What every answer says: the check applies nothing. */
export const APPLIES_NOTHING = "nothing was changed: a check only, no config file or bot setting is touched";

/** What the answer says when a setting's cross rules are listed but not checked. */
export const RULES_NOT_CHECKED =
  "these rules depend on the whole list, so one value is not checked against them: Validate checks them when the run's config loads";

const DEFAULT_UNDERLYING = "BTC";
// A value off the step by less than this is on it (0.1 + 0.2 is 0.30000000000000004).
const STEP_TOLERANCE = 1e-9;

const fmt = (v) => String(Number.isInteger(v) ? v : Number(v.toPrecision(10)));

/** The reasons a value breaks the setting's own range, step or kind. */
function rangeReasons(s, value) {
  if (s.kind === "enum") return s.values.includes(value) ? [] : [`must be one of ${s.values.join(", ")}`];
  if (s.kind === "bool") return typeof value === "boolean" ? [] : ["must be true or false"];
  if (s.kind === "text") return typeof value === "string" ? [] : ["must be text"];
  if (typeof value !== "number" || !Number.isFinite(value)) return ["must be a number"];
  const out = [];
  if (s.kind === "integer" && !Number.isInteger(value)) out.push("must be a whole number");
  if (s.min !== null && value < s.min) out.push(`must be at least ${fmt(s.min)}${s.unit ? ` ${s.unit}` : ""}`);
  if (s.max !== null && value > s.max) out.push(`must be at most ${fmt(s.max)}${s.unit ? ` ${s.unit}` : ""}`);
  if (s.step > 0 && s.kind !== "integer") {
    const steps = (value - (s.min ?? 0)) / s.step;
    if (Math.abs(steps - Math.round(steps)) > STEP_TOLERANCE * Math.max(1, Math.abs(steps))) out.push(`must be on a step of ${fmt(s.step)}`);
  }
  return out;
}

/** The reasons a number breaks a cross rule of the config (the rest of the file as it is). */
function crossReasons(config, key, value) {
  if (typeof value !== "number") return [];
  const out = [];
  const above = config.above[key];
  if (above && (above.inclusive ? value < above.value : value <= above.value)) out.push(above.rule);
  const below = config.below[key];
  if (below && (below.inclusive ? value > below.value : value >= below.value)) out.push(below.rule);
  return out;
}

/**
 * The answer to {key, value, underlying?}: { status, body }. 400 for a body
 * without a key or a value, a fixed key (not tunable) or an unknown
 * underlying; 404 for an unknown key; else 200 with `valid` and its reasons,
 * the range, the cross rules (and a note when they are not checked for one
 * value), the core-policy flag, the tip and the current
 * value in that underlying's shipped config.
 */
export function validateProposal(body, data = labSettings) {
  if (!body || typeof body !== "object" || typeof body.key !== "string" || !body.key || !("value" in body)) {
    return { status: 400, body: { error: "send {\"key\": \"<setting>\", \"value\": <proposed value>}" } };
  }
  const { key, value } = body;
  if (Object.hasOwn(data.fixed, key)) {
    return { status: 400, body: { error: `${key} is not tunable: it is fixed in code (${data.fixed[key]})` } };
  }
  const setting = data.settings.find((s) => s.key === key);
  if (!setting) return { status: 404, body: { error: "no such setting" } };
  const underlying = body.underlying === undefined ? DEFAULT_UNDERLYING : String(body.underlying).toUpperCase();
  const config = data.configs[underlying];
  if (!config) return { status: 400, body: { error: `underlying must be one of ${Object.keys(data.configs).sort().join(", ")}` } };

  const own = rangeReasons(setting, value);
  const reasons = own.length ? own : crossReasons(config, key, value);
  return {
    status: 200,
    body: {
      key,
      title: setting.title,
      value,
      underlying,
      config: config.file,
      valid: reasons.length === 0,
      reasons,
      range: { kind: setting.kind, min: setting.min, max: setting.max, step: setting.step, unit: setting.unit, ...(setting.values ? { values: setting.values } : {}) },
      cross_rules: setting.cross_rules,
      cross_rules_checked: setting.cross_rules_checked,
      cross_rules_note: setting.cross_rules.length && !setting.cross_rules_checked ? RULES_NOT_CHECKED : null,
      core_policy: setting.core_policy,
      policy: setting.policy,
      policy_note: setting.core_policy ? data.policy_note : null,
      tip: findSettingTip(key),
      current: config.current[key] ?? null,
      applied: APPLIES_NOTHING,
    },
  };
}
