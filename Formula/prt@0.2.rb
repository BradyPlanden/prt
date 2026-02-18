class PrtAT02 < Formula
  desc "Open GitHub pull requests in isolated git worktrees"
  homepage "https://github.com/BradyPlanden/prt"
  url "https://github.com/BradyPlanden/prt/archive/refs/tags/v0.2.0.tar.gz"
  sha256 "e0b908a952b01defac323d3e6a0f9073562cbefbf312f91d72ed953142067162"
  license "MIT"

  keg_only :versioned_formula

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s
      -w
      -X
      main.version=v#{version}
    ]

    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/prt"
  end

  test do
    assert_match "Open a GitHub PR", shell_output("#{bin}/prt --help")
  end
end
