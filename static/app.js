(function() {
  "use strict";

  var ui = window.EventIntelUI;
  var events = window.EventIntelEvents;

  var state = {
    events: [],
    cursor: null,
    hasMore: false,
    loading: false,
    list: "venue",
    scope: "categorized",
    category: "",
    venue: "",
    search: "",
    dateFrom: "",
    dateTo: "",
    categoryCounts: [],
    view: "list",
    month: todayKST().slice(0, 7)
  };

  var el = {
    grid: document.getElementById("grid"),
    stateBox: document.getElementById("state"),
    loadMore: document.getElementById("load-more"),
    fList: document.getElementById("f-list"),
    fScope: document.getElementById("f-scope"),
    fCategoryChips: document.getElementById("f-category-chips"),
    fVenue: document.getElementById("f-venue"),
    fDateFrom: document.getElementById("f-date-from"),
    fDateTo: document.getElementById("f-date-to"),
    fSearch: document.getElementById("f-search"),
    count: document.getElementById("filter-count"),
    overlay: document.getElementById("modal-overlay"),
    viewList: document.getElementById("view-list"),
    viewCalendar: document.getElementById("view-calendar"),
    calendarView: document.getElementById("calendar-view"),
    calendarGrid: document.getElementById("calendar-grid"),
    calendarLabel: document.getElementById("calendar-label"),
    calendarUndated: document.getElementById("calendar-undated"),
    calendarPrev: document.getElementById("calendar-prev"),
    calendarToday: document.getElementById("calendar-today"),
    calendarNext: document.getElementById("calendar-next")
  };

  function fmtDateRange(s, e) {
    if (!s) return "일정 미정";
    if (!e || s === e) return s;
    return s + " – " + e;
  }
  function cleanText(s) {
    return String(s || "").replace(/\s+,/g, ",").replace(/,\s+/g, ", ");
  }
  function venueLine(e) {
    if (!e.venue) return "";
    var v = e.venue;
    var parts = [];
    if (v.name) parts.push(ui.escapeHtml(cleanText(v.name)));
    if (v.city) parts.push(ui.escapeHtml(cleanText(v.city)));
    if (v.hall) parts.push('<span class="hall">' + ui.escapeHtml(cleanText(v.hall)) + '</span>');
    return parts.length ? parts.join(" · ") : "";
  }

  function renderCard(e) {
    return (
      '<button class="card" type="button" data-id="' + ui.escapeHtml(e.event_id) + '" aria-label="' + ui.escapeHtml(e.name) + ' 상세 보기">' +
        '<div class="card-date num">' + ui.escapeHtml(fmtDateRange(e.start_date, e.end_date)) + '</div>' +
        '<div class="card-name">' + ui.escapeHtml(e.name) + '</div>' +
        (venueLine(e) ? '<div class="card-venue">' + venueLine(e) + '</div>' : "") +
        '<div class="card-badges">' + ui.badgeHTML(e) + '</div>' +
      '</button>'
    );
  }

  function todayKST() {
    return new Date(Date.now() + 9 * 60 * 60 * 1000).toISOString().slice(0, 10);
  }
  function visibleEvents() {
    if (!state.search) return state.events;
    var q = state.search.toLowerCase();
    return state.events.filter(function(e) {
      // 영문명은 수집 단계에서 보강되므로 영어 검색어도 같은 행사를 찾아야 한다.
      return (e.name || "").toLowerCase().indexOf(q) !== -1 ||
        (e.name_en || "").toLowerCase().indexOf(q) !== -1;
    });
  }

  function updateCount(visible) {
    var total = state.events.length;
    var label = state.list === "benchmark" ? "개 해외 주요 행사" : "개 국내 행사";
    if (state.list === "venue" && state.scope === "categorized") label = "개 주요 국내 행사";
    if (state.search && visible !== total) {
      el.count.textContent = total + "개 중 " + visible + "개 표시" + (state.hasMore ? "+" : "");
      return;
    }
    el.count.textContent = total + (state.hasMore ? "+" : "") + label;
  }

  // renderCategoryChips paints the category filter row: an "전체" reset chip plus
  // one chip per taxonomy category, each carrying its server-computed count badge.
  // Counts come from the events-list category_counts facet (state.categoryCounts).
  function renderCategoryChips() {
    var counts = {};
    (state.categoryCounts || []).forEach(function(c) { counts[c.category] = c.count; });
    var html = '<button type="button" class="chip chip-toggle chip-all" data-cat=""' +
      ' aria-pressed="' + (state.category === "" ? "true" : "false") + '">전체</button>';
    html += ui.categories.map(function(slug) {
      return ui.categoryChip(slug, counts[slug], state.category === slug);
    }).join("");
    el.fCategoryChips.innerHTML = html;
    el.fCategoryChips.querySelectorAll(".chip-toggle").forEach(function(btn) {
      btn.addEventListener("click", function() {
        var cat = btn.getAttribute("data-cat");
        selectCategory(cat === state.category ? "" : cat);
      });
    });
  }

  function selectCategory(cat) {
    state.category = cat;
    state.cursor = null;
    state.events = [];
    renderCategoryChips(); // immediate pressed-state feedback
    serializeFilters();
    loadEvents(false);
  }

  function renderList() {
    var evs = visibleEvents();
    el.grid.innerHTML = evs.map(renderCard).join("");
    updateCount(evs.length);
    el.loadMore.classList.toggle("hidden", !state.hasMore || state.search);
    el.grid.querySelectorAll(".card").forEach(function(btn) {
      btn.addEventListener("click", function() { openDetail(btn.getAttribute("data-id")); });
    });
  }

  function monthParts(month) {
    var m = /^(\d{4})-(\d{2})$/.exec(month || "");
    return m ? { year: Number(m[1]), month: Number(m[2]) } : monthParts(todayKST().slice(0, 7));
  }
  function monthBounds(month) {
    var p = monthParts(month);
    var last = new Date(Date.UTC(p.year, p.month, 0)).getUTCDate();
    return { from: month + "-01", before: month + "-" + String(last).padStart(2, "0"), days: last };
  }
  function shiftMonth(month, delta) {
    var p = monthParts(month);
    var d = new Date(Date.UTC(p.year, p.month - 1 + delta, 1));
    return d.getUTCFullYear() + "-" + String(d.getUTCMonth() + 1).padStart(2, "0");
  }
  function dateUTC(s) {
    var p = String(s).split("-").map(Number);
    return new Date(Date.UTC(p[0], p[1] - 1, p[2]));
  }
  function dateString(d) { return d.toISOString().slice(0, 10); }
  function renderCalendar() {
    var p = monthParts(state.month);
    var bounds = monthBounds(state.month);
    var weekdays = ["월", "화", "수", "목", "금", "토", "일"];
    var buckets = {};
    var undated = [];
    visibleEvents().forEach(function(e) {
      if (!e.start_date) { undated.push(e); return; }
      var start = e.start_date < bounds.from ? bounds.from : e.start_date;
      var end = (e.end_date || e.start_date) > bounds.before ? bounds.before : (e.end_date || e.start_date);
      if (start > end) return;
      for (var d = dateUTC(start), stop = dateUTC(end); d <= stop; d.setUTCDate(d.getUTCDate() + 1)) {
        var key = dateString(d);
        (buckets[key] = buckets[key] || []).push({ event: e, continues: key !== e.start_date });
      }
    });
    el.calendarLabel.textContent = p.year + "년 " + p.month + "월";
    var html = weekdays.map(function(w) { return '<div class="calendar-weekday" aria-hidden="true">' + w + '</div>'; }).join("");
    var first = new Date(Date.UTC(p.year, p.month - 1, 1));
    var offset = (first.getUTCDay() + 6) % 7;
    for (var i = 0; i < offset; i++) html += '<div class="calendar-day outside" aria-hidden="true"></div>';
    for (var day = 1; day <= bounds.days; day++) {
      var key = state.month + "-" + String(day).padStart(2, "0");
      var items = buckets[key] || [];
      var weekday = weekdays[(offset + day - 1) % 7];
      html += '<section class="calendar-day' + (items.length ? '' : ' empty') + '" data-date="' + key + '">' +
        '<time class="calendar-date" datetime="' + key + '"><span class="desktop-label">' + day + '</span><span class="mobile-label">' + p.month + '월 ' + day + '일 (' + weekday + ')</span></time>';
      items.slice(0, 3).forEach(function(item) {
        html += '<button class="calendar-event' + (item.continues ? ' continues' : '') + '" type="button" data-id="' + ui.escapeHtml(item.event.event_id) + '">' + ui.escapeHtml(item.event.name) + '</button>';
      });
      if (items.length > 3) html += '<button class="calendar-more" type="button">+' + (items.length - 3) + '개</button>';
      html += '</section>';
    }
    el.calendarGrid.innerHTML = html;
    el.calendarGrid.querySelectorAll(".calendar-event").forEach(function(btn) {
      btn.addEventListener("click", function() { openDetail(btn.getAttribute("data-id")); });
    });
    el.calendarGrid.querySelectorAll(".calendar-more").forEach(function(btn) {
      btn.addEventListener("click", function() {
        var cell = btn.closest(".calendar-day");
        var items = buckets[cell.getAttribute("data-date")] || [];
        btn.remove();
        items.slice(3).forEach(function(item) {
          var b = document.createElement("button"); b.type = "button"; b.className = "calendar-event"; b.textContent = item.event.name;
          b.onclick = function() { openDetail(item.event.event_id); }; cell.appendChild(b);
        });
      });
    });
    el.calendarUndated.classList.toggle("hidden", undated.length === 0);
    el.calendarUndated.innerHTML = undated.length ? '<h3>일정 미정</h3><p>날짜가 발표되면 달력에 자동으로 표시됩니다.</p><div class="calendar-undated-list">' + undated.map(renderCard).join("") + '</div>' : '';
    el.calendarUndated.querySelectorAll(".card").forEach(function(btn) { btn.onclick = function() { openDetail(btn.getAttribute("data-id")); }; });
    updateCount(visibleEvents().length);
  }
  function renderCurrent() { if (state.view === "calendar") renderCalendar(); else renderList(); }
  function syncViewControls() {
    var calendar = state.view === "calendar";
    el.viewList.setAttribute("aria-pressed", String(!calendar));
    el.viewCalendar.setAttribute("aria-pressed", String(calendar));
    el.calendarView.classList.toggle("hidden", !calendar);
    el.grid.classList.toggle("hidden", calendar);
    el.fDateFrom.disabled = calendar;
    el.fDateTo.disabled = calendar;
    if (calendar) el.loadMore.classList.add("hidden");
  }
  function showState(msg, isError) {
    el.grid.innerHTML = "";
    el.calendarGrid.innerHTML = "";
    el.stateBox.textContent = msg;
    el.stateBox.className = "state" + (isError ? " error" : "");
    el.stateBox.classList.remove("hidden");
    el.loadMore.classList.add("hidden");
  }
  function showSkeletons(n) {
    el.stateBox.classList.add("hidden");
    var html = "";
    for (var i = 0; i < n; i++) html += '<div class="skeleton-card"></div>';
    if (state.view === "calendar") el.calendarGrid.innerHTML = '<div class="state">달력을 불러오는 중...</div>';
    else el.grid.innerHTML = html;
  }
  function fetchJSON(url) {
    return fetch(url, { headers: { "Accept": "application/json" } }).then(function(r) {
      if (r.status === 429) throw new Error("잠시 후 다시 시도해주세요.");
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.json();
    });
  }

  function eventsURL(cursor) {
    var url = "/api/v1/events?limit=100&list=" + encodeURIComponent(state.list);
    // Default the venue list to upcoming events (since=today) unless the user set
    // an explicit start date; benchmark has no implicit date floor.
    if (state.view === "calendar") {
      var bounds = monthBounds(state.month);
      url += "&active_from=" + bounds.from + "&active_before=" + bounds.before + "&include_undated=1";
    } else {
      var since = state.dateFrom || (state.list === "venue" ? todayKST() : "");
      if (since) url += "&since=" + since;
      if (state.dateTo) url += "&before=" + state.dateTo;
    }
    if (state.category) url += "&category=" + encodeURIComponent(state.category);
    if (state.venue && state.list === "venue") url += "&venue=" + encodeURIComponent(state.venue);
    if (cursor) url += "&cursor=" + encodeURIComponent(cursor);
    return url;
  }

  function applyPage(d, append) {
    // The facet is identical on every page (it aggregates the full filtered set),
    // so capture it from the first page of a fresh load.
    if (!append && d.category_counts) state.categoryCounts = d.category_counts;
    var data = events.scoped(d.data || [], state.scope);
    var merged = append ? events.mergeUnique(state.events, data) : { events: data, added: data.length };
    state.events = events.sort(merged.events);
    state.cursor = d.page && d.page.next_cursor;
    state.hasMore = !!(d.page && d.page.has_more);
    return merged.added;
  }

  // loadSeq tags each fresh load so a filter change mid-flight (especially during
  // the categorized lookahead, which can fetch several pages) cannot let a stale
  // request append wrong-context events to the cleared list. Every continuation
  // checks seq against loadSeq and drops itself once superseded.
  var loadSeq = 0;

  function loadPageBatch(append, pagesLeft, added, seq) {
    return fetchJSON(eventsURL(append ? state.cursor : null)).then(function(d) {
      if (seq !== loadSeq) return added; // superseded by a newer load; drop this page
      var nextAdded = added + applyPage(d, append);
      var shouldLookAhead = state.view === "calendar" || (state.scope === "categorized" && !state.category && !state.search);
      if (shouldLookAhead && state.hasMore && pagesLeft > 1) {
        return loadPageBatch(true, pagesLeft - 1, nextAdded, seq);
      }
      return nextAdded;
    });
  }

  function loadEvents(append) {
    // A fresh load supersedes any in-flight load; only a duplicate load-more is
    // ignored while one is already running.
    if (append && state.loading) return Promise.resolve();
    var seq = ++loadSeq;
    state.loading = true;
    if (!append) showSkeletons(6);
    el.loadMore.disabled = true;
    el.loadMore.textContent = "불러오는 중...";
    return loadPageBatch(append, 20, 0, seq).then(function() {
      if (seq !== loadSeq) return; // a newer load started; let it own the UI/state
      state.loading = false;
      if (state.events.length === 0 && !state.search) showState("조건에 맞는 행사가 없습니다.", false);
      else renderCurrent();
      renderCategoryChips();
      el.loadMore.textContent = "더 보기";
      el.loadMore.disabled = false;
    }).catch(function(err) {
      if (seq !== loadSeq) return;
      state.loading = false;
      el.loadMore.textContent = "더 보기";
      el.loadMore.disabled = false;
      showState(err.message || "행사를 불러오지 못했습니다.", true);
    });
  }

  function openDetail(id) {
    el.overlay.innerHTML = '<div class="modal"><button class="modal-close" aria-label="닫기">×</button><p class="modal-meta">불러오는 중...</p></div>';
    el.overlay.classList.remove("hidden");
    document.body.style.overflow = "hidden";
    var prevFocus = document.activeElement;
    var close = closeModal(prevFocus);
    bindModalClose(close);
    el.overlay.onclick = function(ev) { if (ev.target === el.overlay) close(); };
    fetchJSON("/api/v1/events/" + encodeURIComponent(id)).then(function(res) {
      var event = (res && res.data) || res || {};
      el.overlay.innerHTML = window.EventIntelDetail.renderModal(event, ui.badgeHTML(event));
      bindModalClose(close);
      focusCloseButton();
    }).catch(function(err) {
      el.overlay.innerHTML = '<div class="modal"><button class="modal-close" aria-label="닫기">×</button><p style="color:var(--status-cancelled)">' + ui.escapeHtml(err.message || "행사를 불러오지 못했습니다.") + '</p></div>';
      bindModalClose(close);
    });
  }

  function closeModal(prevFocus) {
    return function() {
      el.overlay.classList.add("hidden");
      el.overlay.innerHTML = "";
      document.body.style.overflow = "";
      if (prevFocus && prevFocus.focus) prevFocus.focus();
    };
  }
  function bindModalClose(close) {
    var closeBtn = el.overlay.querySelector(".modal-close");
    if (closeBtn) closeBtn.onclick = close;
  }
  function focusCloseButton() {
    var closeBtn = el.overlay.querySelector(".modal-close");
    if (closeBtn) closeBtn.focus();
  }
  function populateFilters(vocab) {
    (vocab.venues || []).forEach(function(slug) {
      var o = document.createElement("option");
      o.value = slug;
      o.textContent = slug.toUpperCase();
      el.fVenue.appendChild(o);
    });
  }
  function debounce(fn, ms) {
    var t;
    return function() {
      var a = arguments;
      var ctx = this;
      clearTimeout(t);
      t = setTimeout(function() { fn.apply(ctx, a); }, ms);
    };
  }

  function syncListControls() {
    var venueList = state.list === "venue";
    el.fVenue.disabled = !venueList;
    if (!venueList) {
      el.fVenue.value = "";
      state.venue = "";
    }
    syncViewControls();
  }

  // restoreVenueControl reflects state.venue onto the select once its <option>
  // list has been populated from the vocabulary (deserialize runs earlier).
  function restoreVenueControl() {
    if (state.venue && state.list === "venue") el.fVenue.value = state.venue;
  }

  // serializeFilters mirrors the active filters into the URL query string so the
  // view is shareable and bookmarkable. Defaults are omitted to keep URLs short.
  function serializeFilters() {
    var p = new URLSearchParams();
    if (state.list !== "venue") p.set("list", state.list);
    if (state.scope !== "categorized") p.set("scope", state.scope);
    if (state.category) p.set("category", state.category);
    if (state.venue) p.set("venue", state.venue);
    if (state.dateFrom) p.set("from", state.dateFrom);
    if (state.dateTo) p.set("to", state.dateTo);
    if (state.search) p.set("q", state.search);
    if (state.view === "calendar") {
      p.set("view", "calendar");
      p.set("month", state.month);
    }
    var qs = p.toString();
    history.replaceState(null, "", qs ? "?" + qs : location.pathname);
  }

  // deserializeFilters restores filter state from the URL and reflects it onto
  // the controls that exist at parse time (selects/inputs). The venue select's
  // options are populated later, so its value is restored by restoreVenueControl.
  function deserializeFilters() {
    var p = new URLSearchParams(location.search);
    state.list = p.get("list") || "venue";
    state.scope = p.get("scope") || "categorized";
    state.category = p.get("category") || "";
    state.venue = p.get("venue") || "";
    state.dateFrom = p.get("from") || "";
    state.dateTo = p.get("to") || "";
    state.search = p.get("q") || "";
    state.view = p.get("view") === "calendar" ? "calendar" : "list";
    state.month = /^\d{4}-\d{2}$/.test(p.get("month") || "") ? p.get("month") : todayKST().slice(0, 7);
    el.fList.value = state.list;
    el.fScope.value = state.scope;
    el.fDateFrom.value = state.dateFrom;
    el.fDateTo.value = state.dateTo;
    el.fSearch.value = state.search;
  }

  function reloadFresh() {
    state.cursor = null;
    state.events = [];
    serializeFilters();
    loadEvents(false);
  }

  el.fList.addEventListener("change", function() {
    state.list = el.fList.value;
    syncListControls();
    reloadFresh();
  });
  el.fScope.addEventListener("change", function() {
    state.scope = el.fScope.value;
    reloadFresh();
  });
  el.fVenue.addEventListener("change", function() {
    state.venue = el.fVenue.value;
    reloadFresh();
  });
  function onDateChange() {
    state.dateFrom = el.fDateFrom.value;
    state.dateTo = el.fDateTo.value;
    reloadFresh();
  }
  el.fDateFrom.addEventListener("change", onDateChange);
  el.fDateTo.addEventListener("change", onDateChange);
  el.fSearch.addEventListener("input", debounce(function() {
    state.search = el.fSearch.value.trim();
    serializeFilters();
    renderCurrent();
  }, 200));
  function setView(view) {
    if (state.view === view) return;
    state.view = view;
    syncViewControls();
    reloadFresh();
  }
  el.viewList.addEventListener("click", function() { setView("list"); });
  el.viewCalendar.addEventListener("click", function() { setView("calendar"); });
  function moveMonth(delta) { state.month = shiftMonth(state.month, delta); serializeFilters(); reloadFresh(); }
  el.calendarPrev.addEventListener("click", function() { moveMonth(-1); });
  el.calendarNext.addEventListener("click", function() { moveMonth(1); });
  el.calendarToday.addEventListener("click", function() { state.month = todayKST().slice(0, 7); serializeFilters(); reloadFresh(); });
  el.loadMore.addEventListener("click", function() { loadEvents(true); });
  document.addEventListener("keydown", function(e) {
    if (e.key === "Escape" && !el.overlay.classList.contains("hidden")) {
      var closeBtn = el.overlay.querySelector(".modal-close");
      if (closeBtn) closeBtn.click();
    }
  });
  window.addEventListener("popstate", function() {
    deserializeFilters();
    syncListControls();
    renderCategoryChips();
    state.cursor = null;
    state.events = [];
    loadEvents(false).then(restoreVenueControl);
  });

  deserializeFilters();
  syncListControls();
  // Chips are first painted by loadEvents' completion, with their counts already
  // in hand — avoiding an initial badge-less flash.
  Promise.all([fetchJSON("/api/v1"), loadEvents(false)]).then(function(res) {
    if (res[0] && res[0].vocabularies) populateFilters(res[0].vocabularies);
    restoreVenueControl();
  }).catch(function() {});
})();
