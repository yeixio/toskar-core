# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.12.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.2/toskar-1.12.2-darwin-arm64-headless.tar.gz"
    sha256 "6cc5846aaf18673cb69a85b9f61e333ddd4979bd4e9884fe84d808552b801f80"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.2/toskar-1.12.2-darwin-amd64-headless.tar.gz"
    sha256 "5f08fd1db03b0ff4dd1a662382bc4213cc0f297d3d457dbede76545ebe2f67b4"
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
