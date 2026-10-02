# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.4.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.4.0/yggdrasil-1.4.0-darwin-arm64-headless.tar.gz"
    sha256 "6381d47dfeecdc688be51e66949d6bfbad779a2196e3e35f0d6a323aa5fdb764"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.4.0/yggdrasil-1.4.0-darwin-amd64-headless.tar.gz"
    sha256 "902984fcc43d7c074fa78bfccd26e078dc07d6d7f1f3e1d9964baf0927f550f6"
  end

  def install
    bin.install "yggdrasil-daemon"
    bin.install "yggctl"
    bash_completion.install "completions/yggctl.bash" => "yggctl"
    zsh_completion.install "completions/_yggctl"
    fish_completion.install "completions/yggctl.fish"
    (share/"yggdrasil").install "web"
  end

  def caveats
    <<~EOS
      Start the daemon, then open the web UI:

        yggdrasil-daemon
        open http://127.0.0.1:7331
    EOS
  end
end
