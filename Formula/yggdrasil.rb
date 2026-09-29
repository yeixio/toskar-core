# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.3.1"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.1/yggdrasil-1.3.1-darwin-arm64-headless.tar.gz"
    sha256 "448adb2781cf4c17ac3514e0f8b00a73bd4294c63be12dc19dda9ecf536c9ee4"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.1/yggdrasil-1.3.1-darwin-amd64-headless.tar.gz"
    sha256 "1a6120c74d7483b6d97d0e73ea065c72ae34c5cc3850cb5cb97b7bb1bcd17a5f"
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
