package entrypoint_test

import (
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/samber/lo"
	"github.com/wrouesnel/p2cli/pkg/entrypoint"
	"github.com/wrouesnel/p2cli/pkg/envutil"

	. "gopkg.in/check.v1"
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

// Open a file or panic the test.
func MustOpenFile(filepath string) *os.File {
	f, err := os.OpenFile(filepath, os.O_RDONLY, os.FileMode(0777))
	if err != nil {
		panic(err)
	}
	return f
}

// TestInputDataProducesIdenticalOutput tests basic input/output and extension
// inference works.
func (s *p2Integration) TestInputDataProducesIdenticalOutput(c *C) {
	// Template files
	const templateFile string = "tests/data.p2"
	expectedOutput := MustReadFile("tests/data.out")

	testDatas := []string{
		"tests/data.env",
		"tests/data.json",
		"tests/data.yml",
	}

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{},
	}

	for _, td := range testDatas {
		var exit int
		fileTofileOutput := fmt.Sprintf("%s.1.test", td)

		// Check reading and writing to a file works across data types
		entrypointArgs.Args = []string{"-i", td, "-t", templateFile, "-o", fileTofileOutput}
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(fileTofileOutput), DeepEquals, expectedOutput)

		// Check stdin to file works across data types
		stdinToFileOutput := fmt.Sprintf("%s.2.test", td)
		entrypointArgs.StdIn = MustOpenFile(td)
		entrypointArgs.Args = []string{"-f", strings.Split(td, ".")[1], "-t", templateFile, "-o", stdinToFileOutput}
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(stdinToFileOutput), DeepEquals, expectedOutput)

		// Check stdin to stdout works internally
		stdinToStdoutOutput := fmt.Sprintf("%s.3.test", td)
		entrypointArgs.StdIn = MustOpenFile(td)
		entrypointArgs.StdOut = lo.Must(os.OpenFile(stdinToStdoutOutput, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0777)))
		entrypointArgs.Args = []string{"-f", strings.Split(td, ".")[1], "-t", templateFile}
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(stdinToStdoutOutput), DeepEquals, expectedOutput)

		// Check we can read the environment files
		const EnvKey string = "P2_TEST_ENV_KEY"
		envkeyOutput := fmt.Sprintf("%s.4.test", td)
		entrypointArgs.Env[EnvKey] = string(MustReadFile(td))
		entrypointArgs.Args = []string{"-f", strings.Split(td, ".")[1], "-t", templateFile, "--use-env-key", "-i", EnvKey, "-o", envkeyOutput}
		// Dump the data into an environment key
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(MustReadFile(envkeyOutput), DeepEquals, expectedOutput)
	}
}

func (s *p2Integration) TestOnNoTemplateExitFail(c *C) {
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"--template=\"\""},
	}
	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Check(exit, Not(Equals), 0, Commentf("Exit code for command line: %v", os.Args))
}

func (s *p2Integration) TestIncludingEnvironmentVariablsOverridesDataVariables(c *C) {
	env := lo.Must(envutil.FromEnvironment(os.Environ()))
	env["simple_value1"] = "InTheEnvironment"
	env["simple_value2"] = "a value"

	// Template files
	const templateFile string = "tests/data.p2"
	expectedOutput := MustReadFile("tests/data.out")

	testDatas := []string{
		"tests/data.env",
		"tests/data.json",
		"tests/data.yml",
	}

	for _, td := range testDatas {
		var exit int
		fileTofileOutput := fmt.Sprintf("%s.1.test", td)

		entrypointArgs := entrypoint.LaunchArgs{
			StdIn:  os.Stdin,
			StdOut: os.Stdout,
			StdErr: os.Stderr,
			Env:    env,
			Args:   []string{},
		}

		// Check reading and writing to a file works across data types
		entrypointArgs.Args = []string{"-i", td, "-t", templateFile, "-o", fileTofileOutput}
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(string(MustReadFile(fileTofileOutput)), Equals, string(expectedOutput))

		// Check stdin to file works across data types
		stdinToFileOutput := fmt.Sprintf("%s.2.test", td)
		entrypointArgs.StdIn = MustOpenFile(td)
		entrypointArgs.Args = []string{"-t", templateFile, "-o", stdinToFileOutput}
		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(string(MustReadFile(stdinToFileOutput)), Equals, string(MustReadFile("tests/data.out.overridden")))

		// Check stdin to stdout works internally
		stdinToStdoutOutput := fmt.Sprintf("%s.3.test", td)
		entrypointArgs.StdIn = MustOpenFile(td)
		entrypointArgs.StdOut = lo.Must(os.OpenFile(stdinToStdoutOutput, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0777)))
		entrypointArgs.Args = []string{"-f", strings.Split(td, ".")[1], "-t", templateFile}

		exit = entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0)
		c.Check(string(MustReadFile(stdinToStdoutOutput)), Equals, string(expectedOutput), Commentf("failed with %s", td))
	}
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

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{},
	}

	// Test dump input data
	for _, td := range testDatas {
		entrypointArgs.Args = []string{"-t", templateFile, "-i", td, "--debug"}
		exit := entrypoint.Entrypoint(entrypointArgs)
		c.Check(exit, Equals, 0, Commentf("Exit code for input %s != 0", td))
	}
}

func (s *p2Integration) TestIndentFilter(c *C) {
	{
		const templateFile string = "tests/data.indent.p2"
		const emptyData string = "tests/data.indent.json"

		// This test uses the write_file filter to produce its output.
		const outputFile string = "tests/data.indent.test"
		const expectedFile string = "tests/data.indent.out"

		entrypointArgs := entrypoint.LaunchArgs{
			StdIn:  os.Stdin,
			StdOut: os.Stdout,
			StdErr: os.Stderr,
			Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
			Args:   []string{"-t", templateFile, "-i", emptyData, "-o", outputFile},
		}

		exit := entrypoint.Entrypoint(entrypointArgs)
		c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
		c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
	}
}

func (s *p2Integration) TestStructuredFilters(c *C) {
	const templateFile string = "tests/data.structured.p2"
	const emptyData string = "tests/data.structured.json"

	// This test uses the write_file filter to produce its output.
	const outputFile string = "tests/data.structured.test"
	const expectedFile string = "tests/data.structured.out"

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"-t", templateFile, "-i", emptyData, "-o", outputFile},
	}

	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestBase64Filters(c *C) {
	const templateFile string = "tests/data.base64.p2"
	const emptyData string = "tests/data.base64.json"

	// This test uses the write_file filter to produce its output.
	const outputFile string = "tests/data.base64.test"
	const expectedFile string = "tests/data.base64.out"
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"-t", templateFile, "-i", emptyData, "-o", outputFile},
	}
	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestStringFilters(c *C) {
	const templateFile string = "tests/data.string_filters.p2"
	const emptyData string = "tests/data.string_filters.json"

	// This test uses the write_file filter to produce its output.
	const outputFile string = "tests/data.string_filters.test"
	const expectedFile string = "tests/data.string_filters.out"
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"-t", templateFile, "-i", emptyData, "-o", outputFile},
	}
	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestGzipFilters(c *C) {
	const templateFile string = "tests/data.gzip.p2"
	const emptyData string = "tests/data.gzip.json"

	// This test uses the write_file filter to produce its output.
	const outputFile string = "tests/data.gzip.test"
	const expectedFile string = "tests/data.gzip.out"
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"-t", templateFile, "-i", emptyData, "-o", outputFile},
	}
	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
	c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
}

func (s *p2Integration) TestCustomFilters(c *C) {
	{
		const templateFile string = "tests/data.write_file.p2"
		const emptyData string = "tests/data.write_file.json"

		// This test uses the write_file filter to produce its output.
		const outputFile string = "tests/data.write_file.test"
		const expectedFile string = "tests/data.write_file.out"
		entrypointArgs := entrypoint.LaunchArgs{
			StdIn:  os.Stdin,
			StdOut: os.Stdout,
			StdErr: os.Stderr,
			Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
			Args:   []string{"-t", templateFile, "-i", emptyData, "--enable-filters=write_file"},
		}
		exit := entrypoint.Entrypoint(entrypointArgs)
		c.Assert(exit, Equals, 0, Commentf("Exit code for input %s != 0", emptyData))
		c.Check(string(MustReadFile(outputFile)), DeepEquals, string(MustReadFile(expectedFile)))
	}
	// TestMakeDirsFilter tests that make dirs makes a named directory, and also
	// passes it's content through to write_file successfully.
	{
		const templateFile string = "tests/data.make_dirs.p2"
		const emptyData string = "tests/data.make_dirs.json"

		// This test uses the write_file filter to produce its output.
		const outputFile string = "tests/data.make_dirs.test"
		const expectedFile string = "tests/data.make_dirs.out"
		entrypointArgs := entrypoint.LaunchArgs{
			StdIn:  os.Stdin,
			StdOut: os.Stdout,
			StdErr: os.Stderr,
			Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
			Args:   []string{"-t", templateFile, "-i", emptyData, "--enable-filters=make_dirs"},
		}
		exit := entrypoint.Entrypoint(entrypointArgs)
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
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args: []string{"--directory-mode", "-t", "tests/directory-mode/templates",
			"-o", "tests/directory-mode/output"},
	}

	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for directory mode != 0"))
}

func (s *p2Integration) TestTarFileDirectoryModeWithOutputPath(c *C) {
	const tarName = "tests/directory-mode/tar-outputpath.tar"

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args: []string{"--directory-mode", "-t", "tests/directory-mode/templates",
			"-o", "tests/directory-mode/output", "--tar", tarName},
	}

	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for directory mode with --tar != 0"))

	tarFile := lo.Must(os.Open(tarName))

	entries := map[string]struct{}{}

	tarReader := tar.NewReader(tarFile)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		entries[header.Name] = struct{}{}
	}

	for _, expected := range []string{"tests/directory-mode/output/dir1/dir2/template2",
		"tests/directory-mode/output/dir1/template1",
		"tests/directory-mode/output/dir3/template3"} {
		_, ok := entries[expected]
		c.Check(ok, Equals, true, Commentf("%s expected but not found in tar archive", expected))
	}
}

func (s *p2Integration) TestTarFileDirectoryModeWithDefaultOutputPath(c *C) {
	const tarName = "tests/directory-mode/tar-default.tar"

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"--directory-mode", "-t", "tests/directory-mode/templates", "--tar", tarName},
	}

	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for directory mode with --tar != 0"))

	tarFile := lo.Must(os.Open(tarName))

	entries := map[string]struct{}{}

	tarReader := tar.NewReader(tarFile)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		entries[header.Name] = struct{}{}
	}

	for _, expected := range []string{"dir1/dir2/template2", "dir1/template1", "dir3/template3"} {
		_, ok := entries[expected]
		c.Check(ok, Equals, true, Commentf("%s expected but not found in tar archive", expected))
	}
}

func (s *p2Integration) TestFilenameSubstringDeleteForDirectoryMode(c *C) {
	testOutputDir := c.MkDir()

	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args: []string{"--directory-mode", "--directory-mode-filename-substr-del", ".tmpl", "-t", "tests/directory-mode-filename-transform/templates",
			"-o", testOutputDir},
	}

	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Assert(exit, Equals, 0, Commentf("Exit code for deleting substring in filename when in directory mode != 0"))

	// Check output file exists
	expectedFilePath := path.Join(testOutputDir, "dir1/template1.txt")
	if _, err := os.Stat(expectedFilePath); err != nil {
		c.Logf("Expected file: %s does not exist\n", expectedFilePath)
		c.Fail()
	}
}

// TestInvalidEnvironmentVariables tests that invalid environment variables in the input still allow the the template
// to be generated successfully.
func (s *p2Integration) TestInvalidEnvironmentVariables(c *C) {
	entrypointArgs := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    lo.Must(envutil.FromEnvironment(os.Environ())),
		Args:   []string{"-t", "tests/data.p2", "-o", "data.invalid.env"},
	}

	entrypointArgs.Env["BASH_FUNC_x%%"] = "somevalue"

	// Check that invalid OS identifiers are filtered out.
	exit := entrypoint.Entrypoint(entrypointArgs)
	c.Check(exit, Equals, 0)
	c.Assert(exit, Equals, 0, Commentf("Exit code with invalid data in environment != 0"))
}
