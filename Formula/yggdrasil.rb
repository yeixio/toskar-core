# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.2.1"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.1/yggdrasil-1.2.1-darwin-arm64-headless.tar.gz"
    sha256 "1c27124326f70dcb35e7de673ad21cb197d05cdfbcbb8b5b869054ca3ab406e2"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.1/yggdrasil-1.2.1-darwin-amd64-headless.tar.gz"
    sha256 "1a9ac2f4442f2f2fb0622efbf4a1c6dc0cbd0bd5035116c240a2695a0807fd80"
  end

  def install
    bin.install "yggdrasil-daemon"
    bin.install "yggctl"
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
