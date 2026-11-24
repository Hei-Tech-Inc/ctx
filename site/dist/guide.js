const input = document.querySelector("#q");
const articles = [...document.querySelectorAll("#reading article")];
const links = [...document.querySelectorAll(".chapters a")];
const panel = document.querySelector("#matches");
const results = document.querySelector("#results");
const count = document.querySelector("#count");

function showChapter(focus) {
  const chapter =
    articles.find((a) => "#" + a.id === location.hash) || articles[0];
  for (const article of articles) article.hidden = article !== chapter;
  for (const link of links) {
    if (link.hash === "#" + chapter.id)
      link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  }
  document.title = chapter.querySelector("h2").textContent + " · ctx guide";
  if (focus) {
    chapter.focus({ preventScroll: true });
    chapter.scrollIntoView({ block: "start" });
  }
}

function search() {
  const terms = input.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
  results.replaceChildren();
  panel.hidden = !terms.length;
  if (panel.hidden) return;
  for (const article of articles) {
    for (const section of article.querySelectorAll("section")) {
      const title = article.querySelector("h2").textContent;
      const text = (title + " " + section.textContent).toLowerCase();
      if (!terms.every((term) => text.includes(term))) continue;
      const link = document.createElement("a");
      link.href = "#" + article.id;
      link.textContent = section.querySelector("h3").textContent;
      const context = document.createElement("small");
      context.textContent = title;
      link.append(context);
      link.addEventListener("click", (event) => {
        event.preventDefault();
        history.pushState(null, "", "#" + article.id);
        input.value = "";
        search();
        showChapter(false);
        section.tabIndex = -1;
        section.focus({ preventScroll: true });
        section.scrollIntoView({ block: "start" });
      });
      const item = document.createElement("li");
      item.append(link);
      results.append(item);
    }
  }
  const n = results.children.length;
  count.textContent = n
    ? n + (n === 1 ? " result" : " results")
    : "No matches. Try “SSH” or “workspace”.";
}

for (const snippet of document.querySelectorAll(".snippet")) {
  const button = document.createElement("button");
  button.type = "button";
  button.textContent = "Copy";
  button.setAttribute(
    "aria-label",
    "Copy commands for " +
      snippet.parentElement.querySelector("h3").textContent,
  );
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(
        snippet.querySelector("code").textContent,
      );
      button.textContent = "Copied";
    } catch {
      const range = document.createRange();
      range.selectNodeContents(snippet.querySelector("code"));
      const selection = getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      button.textContent = "Text selected";
    }
    setTimeout(() => {
      button.textContent = "Copy";
    }, 1800);
  });
  snippet.append(button);
}

input.value = new URLSearchParams(location.search).get("q") || "";
input.addEventListener("input", search);
document.querySelector("#search").addEventListener("submit", (event) => {
  event.preventDefault();
  results.querySelector("a")?.click();
});
document.addEventListener("keydown", (event) => {
  if (
    event.key === "/" &&
    !event.ctrlKey &&
    !event.metaKey &&
    event.target !== input
  ) {
    event.preventDefault();
    input.focus();
  } else if (event.key === "Escape" && event.target === input) {
    input.value = "";
    search();
  }
});
window.addEventListener("hashchange", () => showChapter(true));
window.addEventListener("popstate", () => showChapter(false));
showChapter(false);
search();
