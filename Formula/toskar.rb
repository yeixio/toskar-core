# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.11.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.2/toskar-1.11.2-darwin-arm64-headless.tar.gz"
    sha256 "dfd8d603435d63f6cb768c9dc9673b8bc1c728d412c02f7f24d44c0aa8bf9676"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.2/toskar-1.11.2-darwin-amd64-headless.tar.gz"
    sha256 "7bccef86bb8f7b2d1be54e59033b68bd4b1722e7ba1f16890038c7bda14cd398"
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
