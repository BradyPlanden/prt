class Prt < Formula
  desc "Open GitHub pull requests in isolated git worktrees"
  homepage "https://github.com/BradyPlanden/prt"
  url "https://github.com/BradyPlanden/prt/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0019dfc4b32d63c1392aa264aed2253c1e0c2fb09216f8e2cc269bbfb8bb49b5"
  license "MIT"

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
