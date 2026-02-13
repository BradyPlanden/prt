class PrtAT01 < Formula
  desc "Open GitHub pull requests in isolated git worktrees"
  homepage "https://github.com/BradyPlanden/prt"
  url "https://github.com/BradyPlanden/prt/archive/refs/tags/v0.1.1.tar.gz"
  sha256 "4db2e61f19fa4d82361b3b08278d146e822d5a23416b2f1571e8ad66305d0457"
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
