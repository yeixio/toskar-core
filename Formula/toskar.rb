# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.14.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.14.0/toskar-1.14.0-darwin-arm64-headless.tar.gz"
    sha256 "b11a197d3c83e0c310c12e48bc119cd49f12d4d5407c0554450f84dbf6307661"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.14.0/toskar-1.14.0-darwin-amd64-headless.tar.gz"
    sha256 "8114bfa2d35430be91afe1eeb9e7e62a0e92f1b916531c7a53ec0f35a924c45d"
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
