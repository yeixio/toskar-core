# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.7.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.7.0/toskar-1.7.0-darwin-arm64-headless.tar.gz"
    sha256 "06037a640a124d892b5d0ebb1c66689057a0b29e8b6287dabbbe29d4ab9c842a"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.7.0/toskar-1.7.0-darwin-amd64-headless.tar.gz"
    sha256 "0c39604429ca4206d49a14bdc46e3fee14f88c4d05a3cd8d29d21da319fa17f6"
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
