# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.12.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.0/toskar-1.12.0-darwin-arm64-headless.tar.gz"
    sha256 "37e1c94f9932c745ecbb7fec2fd6dd854ef270d7cdb220e649c54c70c1bed4ea"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.0/toskar-1.12.0-darwin-amd64-headless.tar.gz"
    sha256 "8c316ced04585f60f016594ca8ebecb58779d9c331a2fa1e0712a2c5e449e321"
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
