class Ctx < Formula
  desc "Client context switcher for git, SSH, cloud CLIs, and per-client environments"
  homepage "https://github.com/Hei-Tech-Inc/ctx"
  url "https://github.com/Hei-Tech-Inc/ctx/archive/refs/tags/v3.1.1.tar.gz"
  sha256 "36a8f6d00282afb211746a6a319d0cbb4a8d9cd11f0ba42359b6d9101f28c868"
  license "MIT"
  version "3.1.1"

  depends_on "bash"
  depends_on "git"

  def install
    bin.install "bin/ctx"
    lib.install Dir["lib/*.sh"]
  end

  def caveats
    <<~EOS
      Only the ctx scripts are installed. Optional tools (mise, gum, gh, cloud CLIs)
      are not — use the full installer from the upstream README, or install tools
      separately and run `ctx doctor`.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ctx version")
  end
end
