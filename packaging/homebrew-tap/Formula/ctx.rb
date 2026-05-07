class Ctx < Formula
  desc "Client context switcher for git, SSH, cloud CLIs, and per-client environments"
  homepage "https://github.com/Hei-Tech-Inc/ctx"
  url "https://github.com/Hei-Tech-Inc/ctx/archive/refs/tags/v3.2.0.tar.gz"
  sha256 "eefc86e28c5b6c9700dd25f0a38e5b01ab4b1f2c1a2788b0741d7e2e157291c2"
  license "MIT"
  version "3.2.0"

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
