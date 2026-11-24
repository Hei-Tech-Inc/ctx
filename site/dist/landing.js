const profiles = {
  acme: { email: "dev@acme.example", ssh: "github-acme", aws: "acme-dev" },
  orbit: { email: "dev@orbit.example", ssh: "github-orbit", aws: "orbit-prod" },
};
const switches = document.querySelectorAll("[data-profile]");
for (const button of switches) {
  button.addEventListener("click", () => {
    const name = button.dataset.profile;
    for (const control of switches)
      control.setAttribute("aria-pressed", String(control === button));
    document.querySelector("#folder").textContent = name;
    for (const [field, value] of Object.entries(profiles[name]))
      document.getElementById(field).textContent = value;
  });
}
document.addEventListener("keydown", (event) => {
  if (
    event.key === "/" &&
    !event.ctrlKey &&
    !event.metaKey &&
    event.target.tagName !== "INPUT"
  ) {
    event.preventDefault();
    document.querySelector("#home-q").focus();
  }
});
