(function() {
  "use strict";

  function todayKST() {
    return new Date(Date.now() + 9 * 60 * 60 * 1000).toISOString().slice(0, 10);
  }

  function endDateKey(event) {
    return event.end_date || event.start_date || "";
  }

  function eventDateKey(event) {
    return event.start_date || "9999-12-31";
  }

  function eventBucket(event, today) {
    if (!event.start_date) return 1;
    if (endDateKey(event) < today) return 2;
    return 0;
  }

  function sort(events) {
    var today = todayKST();
    return events.slice().sort(function(a, b) {
      var byBucket = eventBucket(a, today) - eventBucket(b, today);
      if (byBucket) return byBucket;
      if (eventBucket(a, today) === 2) {
        var byCompletedDate = endDateKey(b).localeCompare(endDateKey(a));
        if (byCompletedDate) return byCompletedDate;
      }
      var byDate = eventDateKey(a).localeCompare(eventDateKey(b));
      if (byDate) return byDate;
      return (a.name || "").localeCompare(b.name || "", "ko");
    });
  }

  function hasCategory(event) {
    return !event.excluded && (event.categories || []).length > 0;
  }

  function scoped(events, scope) {
    if (scope === "all") return events;
    return events.filter(hasCategory);
  }

  function mergeUnique(existing, incoming) {
    var seen = {};
    var merged = [];
    var added = 0;
    existing.forEach(function(event) {
      seen[event.event_id] = true;
      merged.push(event);
    });
    incoming.forEach(function(event) {
      if (seen[event.event_id]) return;
      seen[event.event_id] = true;
      merged.push(event);
      added++;
    });
    return { events: merged, added: added };
  }

  window.EventIntelEvents = {
    mergeUnique: mergeUnique,
    scoped: scoped,
    sort: sort
  };
})();
