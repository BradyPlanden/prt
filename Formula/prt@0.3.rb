class PrtAT03 < Formula
  desc "Open GitHub pull requests in isolated git worktrees"
  homepage "https://github.com/BradyPlanden/prt"
  url "https://github.com/BradyPlanden/prt/archive/refs/tags/v0.3.0.tar.gz"
  sha256 "021cd6c190e19183e0a5b86b6e97ee81707efe9757d2e91801da6de4cf5aea74"
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
