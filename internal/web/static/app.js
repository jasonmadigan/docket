const main = document.getElementById("sections");

async function refresh() {
  const open = new Set(
    Array.from(main.querySelectorAll("details[open]"), (d) => d.dataset.id),
  );
  const y = window.scrollY;
  const res = await fetch("/sections", { cache: "no-store" });
  if (!res.ok) return;
  main.innerHTML = await res.text();
  for (const d of main.querySelectorAll("details")) {
    if (open.has(d.dataset.id)) d.open = true;
  }
  const root = main.firstElementChild;
  if (root && root.dataset.title) document.title = root.dataset.title;
  window.scrollTo(0, y);
}

new EventSource("/events").addEventListener("snapshot", () => {
  refresh().catch(() => {});
});
