// The one piece of javascript in the service: a copy button.
//
// Upstream gjfy proudly shipped none at all, and the pages here still work
// without it — the secret and the link are plain selectable text. This only
// saves a select-and-copy, which on a phone is the difference between "done"
// and "fighting the text handles".
(function () {
	"use strict";

	function textOf(el) {
		return el.value !== undefined ? el.value : el.textContent;
	}

	function selectIt(el) {
		if (el.select) {
			el.select();
			return;
		}
		var range = document.createRange();
		range.selectNodeContents(el);
		var sel = window.getSelection();
		sel.removeAllRanges();
		sel.addRange(range);
	}

	// execCommand is deprecated but is the only thing that works outside a
	// secure context and in older mobile browsers.
	function legacyCopy(el) {
		selectIt(el);
		try {
			return document.execCommand("copy");
		} catch (e) {
			return false;
		}
	}

	function flash(btn, label) {
		var was = btn.textContent;
		btn.textContent = label;
		btn.classList.add("done");
		setTimeout(function () {
			btn.textContent = was;
			btn.classList.remove("done");
		}, 1600);
	}

	document.addEventListener("click", function (ev) {
		var btn = ev.target.closest && ev.target.closest("[data-copy]");
		if (!btn) {
			return;
		}
		var src = document.getElementById(btn.getAttribute("data-copy"));
		if (!src) {
			return;
		}
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(textOf(src)).then(
				function () {
					flash(btn, "Скопировано");
				},
				function () {
					flash(btn, legacyCopy(src) ? "Скопировано" : "Выделено");
				}
			);
			return;
		}
		flash(btn, legacyCopy(src) ? "Скопировано" : "Выделено");
	});
})();
