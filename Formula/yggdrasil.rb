# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.2.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.0/yggdrasil-1.2.0-darwin-arm64-headless.tar.gz"
    sha256 "4074412f53d17769f8af8e5075865e127e230d925b6abad85b3cc2f62ed7af59"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.0/yggdrasil-1.2.0-darwin-amd64-headless.tar.gz"
    sha256 "090e9c79f41a275d183fae6cde0d88755b5bdb1d436045047b23af00b3af21ed"
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
