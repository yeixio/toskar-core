# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.11.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.0/toskar-1.11.0-darwin-arm64-headless.tar.gz"
    sha256 "c7dba8112faf75c6cb5a3ab3cdc45fdc3ad73b3b1523ff464ea851561c6f2998"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.0/toskar-1.11.0-darwin-amd64-headless.tar.gz"
    sha256 "683538ae66fcfde5cf7b34fe62cad442fd8a1e993ec4c9bd0d6a04d20364a140"
  end

  def install
    bin.install "toskar"
    bin.install "toskarctl"
    # The names from before the rename, for launchd agents and MCP settings
    # that run them by path.
    bin.install_symlink bin/"toskar" => "yggdrasil-daemon"
    bin.install_symlink bin/"toskarctl" => "yggctl"
    bash_completion.install "completions/toskarctl.bash" => "toskarctl"
    bash_completion.install_symlink bash_completion/"toskarctl" => "yggctl"
    zsh_completion.install "completions/_toskarctl"
    fish_completion.install "completions/toskarctl.fish"
    fish_completion.install_symlink fish_completion/"toskarctl.fish" => "yggctl.fish"
    (share/"yggdrasil").install "web"
  end

  def caveats
    <<~EOS
      Start the daemon, then open the web UI:

        toskar
        open http://127.0.0.1:7331
    EOS
  end
end
