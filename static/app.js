(function() {
  "use strict";

  var ui = window.EventIntelUI;
  var events = window.EventIntelEvents;

  var state = {
    events: [],
    cursor: null,
    hasMore: false,
    loading: false,
    scope: "categorized",
    category: "",
    venue: "",
    search: ""
  };

  var el = {
    grid: document.getElementById("grid"),
    stateBox: document.getElementById("state"),
    loadMore: document.getElementById("load-more"),
    fScope: document.getElementById("f-scope"),
    fCategory: document.getElementById("f-category"),
    fVenue: document.getElementById("f-venue"),
    fSearch: document.getElementById("f-search"),
    count: document.getElementById("filter-count"),
    overlay: document.getElementById("modal-overlay")
  };

  function fmtDateRange(s, e) {
    if (!s) return "일정 미정";
    if (!e || s === e) return s;
    return s + " \u2013 " + e;
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
    return parts.length ? parts.join(" \u00b7 ") : "";
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
      return (e.name || "").toLowerCase().indexOf(q) !== -1;
    });
  }

  function updateCount(visible) {
    var total = state.events.length;
    var label = state.scope === "all" ? "개 행사" : "개 주요 행사";
    if (state.search && visible !== total) {
      el.count.textContent = total + "개 중 " + visible + "개 표시" + (state.hasMore ? "+" : "");
      return;
    }
    el.count.textContent = total + (state.hasMore ? "+" : "") + label;
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
  function showState(msg, isError) {
    el.grid.innerHTML = "";
    el.stateBox.textContent = msg;
    el.stateBox.className = "state" + (isError ? " error" : "");
    el.stateBox.classList.remove("hidden");
    el.loadMore.classList.add("hidden");
  }
  function showSkeletons(n) {
    el.stateBox.classList.add("hidden");
    var html = "";
    for (var i = 0; i < n; i++) html += '<div class="skeleton-card"></div>';
    el.grid.innerHTML = html;
  }
  function fetchJSON(url) {
    return fetch(url, { headers: { "Accept": "application/json" } }).then(function(r) {
      if (r.status === 429) throw new Error("잠시 후 다시 시도해주세요.");
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.json();
    });
  }

  function eventsURL(cursor) {
    var url = "/api/v1/events?limit=100&since=" + todayKST();
    if (state.category) url += "&category=" + encodeURIComponent(state.category);
    if (state.venue) url += "&venue=" + encodeURIComponent(state.venue);
    if (cursor) url += "&cursor=" + encodeURIComponent(cursor);
    return url;
  }

  function applyPage(d, append) {
    var data = events.scoped(d.data || [], state.scope);
    var merged = append ? events.mergeUnique(state.events, data) : { events: data, added: data.length };
    state.events = events.sort(merged.events);
    state.cursor = d.page && d.page.next_cursor;
    state.hasMore = !!(d.page && d.page.has_more);
    return merged.added;
  }

  function loadPageBatch(append, pagesLeft, added) {
    return fetchJSON(eventsURL(append ? state.cursor : null)).then(function(d) {
      var nextAdded = added + applyPage(d, append);
      var shouldLookAhead = state.scope === "categorized" && !state.category && !state.search;
      if (shouldLookAhead && state.hasMore && pagesLeft > 1) {
        return loadPageBatch(true, pagesLeft - 1, nextAdded);
      }
      return nextAdded;
    });
  }

  function loadEvents(append) {
    if (state.loading) return Promise.resolve();
    state.loading = true;
    if (!append) showSkeletons(6);
    el.loadMore.disabled = true;
    el.loadMore.textContent = "불러오는 중...";
    return loadPageBatch(append, 20, 0).then(function() {
      state.loading = false;
      if (state.events.length === 0 && !state.search) showState("조건에 맞는 행사가 없습니다.", false);
      else renderList();
      el.loadMore.textContent = "더 보기";
      el.loadMore.disabled = false;
    }).catch(function(err) {
      state.loading = false;
      el.loadMore.textContent = "더 보기";
      el.loadMore.disabled = false;
      showState(err.message || "행사를 불러오지 못했습니다.", true);
    });
  }

  function openDetail(id) {
    el.overlay.innerHTML = '<div class="modal"><button class="modal-close" aria-label="닫기">\u00d7</button><p class="modal-meta">불러오는 중...</p></div>';
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
      el.overlay.innerHTML = '<div class="modal"><button class="modal-close" aria-label="닫기">\u00d7</button><p style="color:var(--status-cancelled)">' + ui.escapeHtml(err.message || "행사를 불러오지 못했습니다.") + '</p></div>';
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
    (vocab.categories || []).forEach(function(slug) {
      var o = document.createElement("option");
      o.value = slug;
      o.textContent = ui.humanizeCat(slug);
      el.fCategory.appendChild(o);
    });
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

  el.fScope.addEventListener("change", function() {
    state.scope = el.fScope.value;
    state.cursor = null;
    state.events = [];
    loadEvents(false);
  });
  el.fCategory.addEventListener("change", function() {
    state.category = el.fCategory.value;
    state.cursor = null;
    state.events = [];
    loadEvents(false);
  });
  el.fVenue.addEventListener("change", function() {
    state.venue = el.fVenue.value;
    state.cursor = null;
    state.events = [];
    loadEvents(false);
  });
  el.fSearch.addEventListener("input", debounce(function() {
    state.search = el.fSearch.value.trim();
    renderList();
  }, 200));
  el.loadMore.addEventListener("click", function() { loadEvents(true); });
  document.addEventListener("keydown", function(e) {
    if (e.key === "Escape" && !el.overlay.classList.contains("hidden")) {
      var closeBtn = el.overlay.querySelector(".modal-close");
      if (closeBtn) closeBtn.click();
    }
  });
  Promise.all([fetchJSON("/api/v1"), loadEvents(false)]).then(function(res) {
    if (res[0] && res[0].vocabularies) populateFilters(res[0].vocabularies);
  }).catch(function() {});
})();
