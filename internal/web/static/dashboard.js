// Tooltips for the chart's columns, on hover and on keyboard focus, and
// arrow keys to move between the columns, which take one tab stop. Text goes
// in with textContent, never as HTML.
(function () {
  "use strict";
  var tip = document.getElementById("tip");
  var plot = document.querySelector(".cols");
  if (!tip || !plot) {
    return;
  }
  var cols = Array.prototype.slice.call(plot.querySelectorAll(".col"));

  function show(col) {
    tip.textContent = col.getAttribute("aria-label");
    tip.hidden = false;
    var box = col.getBoundingClientRect();
    var bar = col.querySelector(".bar").getBoundingClientRect();
    var width = tip.offsetWidth;
    var height = tip.offsetHeight;
    var page = document.documentElement.clientWidth;
    var left = Math.min(Math.max(8, box.left + box.width / 2 - width / 2), page - width - 8);
    var top = Math.min(bar.top, box.bottom) - height - 8;
    tip.style.left = left + window.scrollX + "px";
    tip.style.top = Math.max(8, top) + window.scrollY + "px";
  }

  function hide() {
    tip.hidden = true;
  }

  cols.forEach(function (col) {
    col.addEventListener("mouseenter", function () { show(col); });
    col.addEventListener("focus", function () { show(col); });
    col.addEventListener("mouseleave", hide);
    col.addEventListener("blur", hide);
  });

  plot.addEventListener("keydown", function (e) {
    var i = cols.indexOf(document.activeElement);
    var next = {ArrowLeft: i - 1, ArrowRight: i + 1, Home: 0, End: cols.length - 1}[e.key];
    if (i < 0 || next === undefined || next < 0 || next >= cols.length) {
      if (e.key === "Escape") {
        hide();
      }
      return;
    }
    e.preventDefault();
    cols[i].tabIndex = -1;
    cols[next].tabIndex = 0;
    cols[next].focus();
  });
})();
