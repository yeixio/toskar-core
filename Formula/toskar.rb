# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.12.3"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.3/toskar-1.12.3-darwin-arm64-headless.tar.gz"
    sha256 "d9c9d1457e5ac2b1bde7e5280a34c994ed8d6d3d58700113994950ac188257ce"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.12.3/toskar-1.12.3-darwin-amd64-headless.tar.gz"
    sha256 "3ac0afcc379e5d23caf182b3c04fe2f6d0df419cea05de9e0cecc7610708b921"
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
