# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.3.1"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.1/yggdrasil-1.3.1-darwin-arm64-headless.tar.gz"
    sha256 "1778d664494af903110e6b53a1cd23910d32fd31ddfd84fe1a1bdaada4a772c3"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.1/yggdrasil-1.3.1-darwin-amd64-headless.tar.gz"
    sha256 "dfae2545d7599f8ab819cca156f588f83b8f4fa6077c8d9a82c3933a0596c70b"
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
