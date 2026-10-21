# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.10.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.10.0/toskar-1.10.0-darwin-arm64-headless.tar.gz"
    sha256 "e6dbfc392a21a75611ee9950cccf65c6157799f73d9f64e6e29c5f6095d1a84d"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.10.0/toskar-1.10.0-darwin-amd64-headless.tar.gz"
    sha256 "ed4f60e35c356dfa887d0bbcbb6c5c73a7a40d7b2e6c38af991a1c7a63807591"
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
