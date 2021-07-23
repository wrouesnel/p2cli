package main

import (
	"fmt"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type p2Integration struct{}

var _ = Suite(&p2Integration{})

func MustReadFile(filePath string) []byte {
	testResult, err := ioutil.ReadFile(filePath)
	if err != nil {
		panic(err)
	}
	return testResult
}

// Open a file or panic the test
func MustOpenFile(filepath string) *os.File {
	f, err := os.OpenFile(filepath, os.O_RDONLY, os.FileMode(0777))
	if err != nil {
		panic(err)
	}
	return f
}

// TestInputDataProducesIdenticalOutput tests basic input/output and extension
// inference works
func (s *p2Integration) TestInputDataProducesIdenticalOutput(c *C) {
	// Store the original origStdin
	var origStdin, origStdout *os.File
	origStdin = os.Stdin
	origStdout = os.Stdout

	// Template files
	const templateFile string = "tests/data.p2"
	expectedOutput := MustReadFile("tests/data.out")

	type tData struct {
		InputFile          string // Test input file
		ExpectedOutputFile string // Expected output file
	}

	testDatas := []string{
		"tests/data.env",
		"tests/data.json",
		"tests/data.yml",
	}

	for _, td := range testDatas {
		var exit int
		fileTofileOutput := fmt.Sprintf("%s.1.test", td)

		// Check reading and writing to a file works across data types
		os.Args = []string{"p2", "-i", td, "-t", templateFile, "-o", fileTofileOutput}
		exit = realMain()
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(fileTofileOutput), DeepEquals, expectedOutput)

		// Check stdin to file works across data types
		stdinToFileOutput := fmt.Sprintf("%s.2.test", td)
		os.Stdin = MustOpenFile(td)
		os.Args = []string{"p2", "-t", templateFile, "-o", stdinToFileOutput}
		exit = realMain()
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(stdinToFileOutput), DeepEquals, expectedOutput)

		// Check stdin to stdout works internally
		stdinToStdoutOutput := fmt.Sprintf("%s.3.test", td)
		os.Stdin = MustOpenFile(td)
		var err error
		os.Stdout, err = os.OpenFile(stdinToStdoutOutput, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0777))
		if err != nil {
			panic(err)
		}
		os.Args = []string{"p2", "-f", strings.Split(td, ".")[1], "-t", templateFile}
		exit = realMain()
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(stdinToStdoutOutput), DeepEquals, expectedOutput)

		// Check we can read the environment files
		const EnvKey string = "P2_TEST_ENV_KEY"
		envkeyOutput := fmt.Sprintf("%s.4.test", td)
		os.Args = []string{"p2", "-f", strings.Split(td, ".")[1], "-t", templateFile, "--use-env-key", "-i", EnvKey, "-o", envkeyOutput}
		// Dump the data into an environment key
		os.Setenv(EnvKey, string(MustReadFile(td)))
		exit = realMain()
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(envkeyOutput), DeepEquals, expectedOutput)
	}

	os.Stdin = origStdin
	os.Stdout = origStdout
}

func (s *p2Integration) TestOnNoTemplateExitFail(c *C) {
	os.Args = []string{"p2", "--template=\"\""}
	exit := realMain()
	c.Check(exit, Not(Equals), 0, Commentf("Exit code for command line: %v", os.Args))
}

// TestDebugCommandLineOptionsWork exercises the non-critical path command ine
// options to ensure they operate without crashing.
func (s *p2Integration) TestDebugCommandLineOptionsWork(c *C) {
	const templateFile string = "tests/data.p2"

	testDatas := []string{
		"tests/data.env",
		"tests/data.json",
		"tests/data.yml",
	}

	// Test dump input data
	for _, td := range testDatas {
		os.Args = []string{"p2", "-t", templateFile, "-i", td, "--debug"}
		exit := realMain()
		c.Check(exit, Equals, 0, Commentf("Exit code for input %s != 0", td))
	}
}

func (s *p2Integration) TestIndentFilter(c *C) {
	{
		const templateFile string = "tests/data.indent.p2"
		const emptyData string = "tests/data.indent.json"

		// This test uses the write_file filter to produce its output.
		outputFile := fmt.Sprintf("tests/data.indent.test")
		const expectedFile string = "tests/data.indent.out"
		os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "-o", outputFile}
		exit := realMain()
		c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
		c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
	}
}

func (s *p2Integration) TestStructuredFilters(c *C) {
	const templateFile string = "tests/data.structured.p2"
	const emptyData string = "tests/data.structured.json"

	// This test uses the write_file filter to produce its output.
	outputFile := fmt.Sprintf("tests/data.structured.test")
	const expectedFile string = "tests/data.structured.out"
	os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "-o", outputFile}
	exit := realMain()
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestBase64Filters(c *C) {
	const templateFile string = "tests/data.base64.p2"
	const emptyData string = "tests/data.base64.json"

	// This test uses the write_file filter to produce its output.
	outputFile := fmt.Sprintf("tests/data.base64.test")
	const expectedFile string = "tests/data.base64.out"
	os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "-o", outputFile}
	exit := realMain()
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestStringFilters(c *C) {
	const templateFile string = "tests/data.string_filters.p2"
	const emptyData string = "tests/data.string_filters.json"

	// This test uses the write_file filter to produce its output.
	outputFile := fmt.Sprintf("tests/data.string_filters.test")
	const expectedFile string = "tests/data.string_filters.out"
	os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "-o", outputFile}
	exit := realMain()
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestGzipFilters(c *C) {
	const templateFile string = "tests/data.gzip.p2"
	const emptyData string = "tests/data.gzip.json"

	// This test uses the write_file filter to produce its output.
	outputFile := fmt.Sprintf("tests/data.gzip.test")
	const expectedFile string = "tests/data.gzip.out"
	os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "-o", outputFile}
	exit := realMain()
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestCustomFilters(c *C) {
	{
		const templateFile string = "tests/data.write_file.p2"
		const emptyData string = "tests/data.write_file.json"

		// This test uses the write_file filter to produce its output.
		outputFile := fmt.Sprintf("tests/data.write_file.test")
		const expectedFile string = "tests/data.write_file.out"
		os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "--enable-filters=write_file"}
		exit := realMain()
		c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
		c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
	}
	// TestMakeDirsFilter tests that make dirs makes a named directory, and also
	// passes it's content through to write_file successfully.
	{
		const templateFile string = "tests/data.make_dirs.p2"
		const emptyData string = "tests/data.make_dirs.json"

		// This test uses the write_file filter to produce its output.
		outputFile := fmt.Sprintf("tests/data.make_dirs.test")
		const expectedFile string = "tests/data.make_dirs.out"
		os.Args = []string{"p2", "-t", templateFile, "-i", emptyData, "--enable-filters=make_dirs"}
		exit := realMain()
		c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
		c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))

		const createdDirectory string = "tests/make_dirs.test"
		st, err := os.Stat(createdDirectory)
		c.Assert(err, IsNil, Commentf("make_dirs didn't make a directory"))
		c.Assert(st.IsDir(), Equals, true, Commentf("didn't get a directory from make_dirs?"))
		// Remove the directory to avoid weird asseerts on subsequent runs
		os.RemoveAll(createdDirectory)
	}
}

func (s *p2Integration) TestDirectoryMode(c *C) {
	os.Args = []string{"p2", "--directory-mode", "-t", "tests/directory-mode/templates",
		"-o", "tests/directory-mode/output"}

	exit := realMain()
	c.Assert(exit, Equals, 0, Commentf("Exit code for dirextory mode != 0"))
}

// TestInvalidEnvironmentVariables tests that invalid environment variables in the input still allow the the template
// to be generated successfully.
func (s *p2Integration) TestInvalidEnvironmentVariables(c *C) {
	// Check that invalid OS identifiers are filtered out.
	os.Setenv("BASH_FUNC_x%%", "somevalue")
	os.Args = []string{"p2", "-t", "tests/data.p2", "-o", "data.invalid.env"}
	exit := realMain()
	c.Check(exit, Equals, 0)
	c.Assert(exit, Equals, 0, Commentf("Exit code with invalid data in environment != 0"))
	os.Unsetenv("BASH_FUNC_x%%")
}