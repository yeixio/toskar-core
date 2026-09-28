# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.2.0-beta.3"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.0-beta.3/yggdrasil-1.2.0-beta.3-darwin-arm64-headless.tar.gz"
    sha256 "204a64a50d0ca7057ab4a26f93df5d21294dedf2f83e8f64655f65df5e3e2fc2"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.2.0-beta.3/yggdrasil-1.2.0-beta.3-darwin-amd64-headless.tar.gz"
    sha256 "da0c3f1352ed4de93c4b1b6b38a3c316c4e0d9c74b0183fd38057673ea36f4cb"
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
