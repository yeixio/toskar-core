# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.12.1"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.1/toskar-1.12.1-darwin-arm64-headless.tar.gz"
    sha256 "1e021c6d90ca203766ef600c16408a9d0366fdefa5c0ed904472e636b0d37bd2"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.1/toskar-1.12.1-darwin-amd64-headless.tar.gz"
    sha256 "182ef36a29d1f4b69e3d20c2988d70a33ce7bc113f613a9648f3aea69ec0ab1a"
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
