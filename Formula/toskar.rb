# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.11.1"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.1/toskar-1.11.1-darwin-arm64-headless.tar.gz"
    sha256 "4e955fbe4cc5ae3f883c5c9242fec5186fc2ed80d9837eddddd50bd719932d8d"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.11.1/toskar-1.11.1-darwin-amd64-headless.tar.gz"
    sha256 "3b04715843790a1c53a20a91c637f1959a3aae8b65fbee35e57c51d11ad53edb"
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
