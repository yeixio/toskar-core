# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.9.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.9.2/toskar-1.9.2-darwin-arm64-headless.tar.gz"
    sha256 "d5200e10a95828a256bf8f9fb6a836ecff4cec2e830f1e2ebeeef2411865140f"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.9.2/toskar-1.9.2-darwin-amd64-headless.tar.gz"
    sha256 "a3f08bd0430546c02b0e55b0478aa0609c15e33daf091c3a1f40f52227f52f12"
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
