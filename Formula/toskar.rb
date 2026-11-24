# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.14.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.14.2/toskar-1.14.2-darwin-arm64-headless.tar.gz"
    sha256 "e4533b333507c9419fb9ec7d92d22f70b3c37e6e54cbb87fe3eaf9a18f6afd58"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.14.2/toskar-1.14.2-darwin-amd64-headless.tar.gz"
    sha256 "f2928b9eb355627b369b732f9423986753321c78d6c20acc246ad10d3d000ba9"
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
