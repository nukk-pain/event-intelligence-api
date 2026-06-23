(function() {
  "use strict";

  var CAT_LABELS = {
    "ai": "AI",
    "humanoid-robotics": "로봇",
    "bio": "바이오",
    "medical-devices": "의료기기",
    "digital-health": "디지털 헬스"
  };
  var STATUS_LABELS = {
    "tentative": "일정 미정",
    "postponed": "연기",
    "cancelled": "취소",
    "ended": "종료"
  };
  var COST_LABELS = { "free": "무료", "paid": "유료", "mixed": "무료+유료" };
  var QUALITY_LABELS = { "high": "기회 높음", "medium": "기회 보통", "low": "기회 낮음" };

  function escapeHtml(s) {
    if (s == null) return "";
    return String(s).replace(/[&<>"']/g, function(c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function humanizeCat(slug) {
    return CAT_LABELS[slug] || slug;
  }

  function chipFor(cat) {
    return '<span class="chip cat-' + cat + '">' + escapeHtml(humanizeCat(cat)) + '</span>';
  }

  function statusChip(e) {
    if (!e.status || e.status === "scheduled") return "";
    return '<span class="chip status-' + e.status + '">' + (STATUS_LABELS[e.status] || e.status) + '</span>';
  }

  function costChip(e) {
    if (!e.cost_hint || e.cost_hint === "unknown") return "";
    return '<span class="chip cost-' + e.cost_hint + '">' + (COST_LABELS[e.cost_hint] || e.cost_hint) + '</span>';
  }

  function otherChip(e) {
    if (e.excluded || !(e.categories || []).length) return '<span class="chip cat-other">기타</span>';
    return "";
  }

  function opportunityChip(e) {
    if (!e.opportunity_quality || e.opportunity_quality === "low") return "";
    return '<span class="chip opportunity-' + e.opportunity_quality + '">' + (QUALITY_LABELS[e.opportunity_quality] || e.opportunity_quality) + '</span>';
  }

  function badgeHTML(e) {
    return opportunityChip(e) + (e.categories || []).map(chipFor).join("") + otherChip(e) + statusChip(e) + costChip(e);
  }

  window.EventIntelUI = {
    badgeHTML: badgeHTML,
    escapeHtml: escapeHtml,
    humanizeCat: humanizeCat
  };
})();
