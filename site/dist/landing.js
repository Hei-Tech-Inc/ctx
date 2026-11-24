const profiles = {
  acme: { email: "dev@acme.example", ssh: "github-acme", aws: "acme-dev" },
  orbit: { email: "dev@orbit.example", ssh: "github-orbit", aws: "orbit-prod" },
};
const switches = document.querySelectorAll("[data-profile]");
const guardResult = document.querySelector("#guard-result");
const guardAction = document.querySelector("#guard-action");
let activeProfile = "acme";
let blocked = false;

function resetGuard() {
  blocked = false;
  guardResult.dataset.state = "ready";
  guardResult.textContent = "The sample repository is ready for a commit.";
  guardAction.textContent = "Try the wrong email";
}

guardAction.addEventListener("click", () => {
  const expected = profiles[activeProfile].email;
  if (blocked) {
    guardResult.dataset.state = "passed";
    guardResult.textContent = `Guard passed. The author and committer now match ${expected}. The commit can proceed.`;
    guardAction.textContent = "Try the wrong email again";
    blocked = false;
    return;
  }

  const other = activeProfile === "acme" ? "orbit" : "acme";
  guardResult.dataset.state = "blocked";
  guardResult.textContent = `Commit blocked. This repository sets user.email to ${profiles[other].email}. Profile ${activeProfile} expects ${expected}.`;
  guardAction.textContent = "Use the profile email and retry";
  blocked = true;
});

for (const button of switches) {
  button.addEventListener("click", () => {
    const name = button.dataset.profile;
    activeProfile = name;
    resetGuard();
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
