# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.8.2"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.8.2/toskar-1.8.2-darwin-arm64-headless.tar.gz"
    sha256 "71ba64b6a3b3063d9424f459c50ce72fab86cc5a148afbc2aebb5adb7890960b"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.8.2/toskar-1.8.2-darwin-amd64-headless.tar.gz"
    sha256 "910c9322fdd9270010ac79bdb11248c8e550e6955cb68be007da1b2aa7398809"
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
