# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.15.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.15.2/toskar-1.15.2-darwin-arm64-headless.tar.gz"
    sha256 "d80a8e64fab9473c86580c2b17296ae0ed39d349daf870bb05452eed49f79bb3"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.15.2/toskar-1.15.2-darwin-amd64-headless.tar.gz"
    sha256 "5011891f606337a2a9e1d60c6eb53fad1c5e5af9b5d77248546fb90ece51acaf"
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
