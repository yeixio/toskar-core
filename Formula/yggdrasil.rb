# Homebrew formula written by the core release workflow.
class Yggdrasil < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.3.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.0/yggdrasil-1.3.0-darwin-arm64-headless.tar.gz"
    sha256 "240f5484381349de4dd9a036a5976d9f5f1ae356b018a8529c259384c2c2e8f3"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.3.0/yggdrasil-1.3.0-darwin-amd64-headless.tar.gz"
    sha256 "ae36103f0527e9d30f33f79ea4e4c26c5fdd437878dd81b2d6e1696f6f1cc30c"
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
