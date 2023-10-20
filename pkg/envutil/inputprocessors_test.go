package envutil_test

import (
	"os"
	"testing"

	"github.com/wrouesnel/p2cli/pkg/envutil"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type testSuite struct{}

var _ = Suite(&testSuite{})

func (s *testSuite) TestFromEnvironment(c *C) {
	result, _ := envutil.FromEnvironment([]string{"TESTKEY=1"})
	c.Check(result["TESTKEY"], Equals, "1")
}

func (s *testSuite) TestFromEnvironmentUsingEnvironment(c *C) {
	os.Setenv("TESTKEY", "1")
	result, _ := envutil.FromEnvironment(nil)
	os.Unsetenv("TESTKEY")
	c.Check(result["TESTKEY"], Equals, "1")
}
