import { requireRole } from "@/lib/auth/server";
import { allSettingTips, findSettingTip, findTermTip } from "@/lib/tips";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

/**
 * GET /api/help/tips (#588, epic E11): the plain-words tips, read-only, any
 * signed-in role. No query: every setting's tip, `{tips: [...]}`.
 * `?key=<setting>`: that setting's tip (meaning, unit, raise_risk,
 * raise_income, video or null, needs_product_owner), 404 when it has none.
 * `?term=<term>`: the glossary entry's short text and risk_income line, 404
 * when unknown. Both given: the key wins. Nothing personal is read or logged.
 */
export async function GET(request) {
  const { response } = await requireRole(request, "user");
  if (response) return response;

  const params = new URL(request.url).searchParams;
  const key = params.get("key");
  const term = params.get("term");
  if (key === null && term === null) return Response.json({ tips: allSettingTips() }, { headers: NO_STORE });

  const tip = key !== null ? findSettingTip(key) : findTermTip(term);
  if (!tip) {
    const what = key !== null ? "setting" : "term";
    return Response.json({ error: `no tip for this ${what}` }, { status: 404, headers: NO_STORE });
  }
  return Response.json(tip, { headers: NO_STORE });
}
