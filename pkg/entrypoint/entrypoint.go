/*
A Golang replica of j2cli from Python. Designed for allowing easy templating
of files using Jinja2-like syntax (from the Pongo2 engine).

Extremely useful for building Docker files when you don't want to pull in all of
python.
*/

package entrypoint

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/flosch/pongo2/v4"
	"github.com/samber/lo"
	"github.com/wrouesnel/p2cli/pkg/errdefs"

	"github.com/kballard/go-shellquote"
	"github.com/wrouesnel/p2cli/pkg/templating"

	"gopkg.in/yaml.v2"

	"go.uber.org/zap"
)

// Version is populated by the build system.
var Version = "development"

const description = "Pongo2 based command line templating tool"

// Copied from pongo2.context.
var reIdentifiers = regexp.MustCompile("^[a-zA-Z0-9_]+$")

// SupportedType is an enumeration of data types we support.
type SupportedType int

const (
	// TypeUnknown is the default error type.
	TypeUnknown SupportedType = iota
	// TypeJSON is JSON.
	TypeJSON SupportedType = iota
	// TypeYAML is YAML.
	TypeYAML SupportedType = iota
	// TypeEnv is key=value pseudo environment files.
	TypeEnv SupportedType = iota
)

// DataSource is an enumeration of the sources of input data we can take.
type DataSource int

const (
	// SourceEnv means input comes from environment variables.
	SourceEnv DataSource = iota
	// SourceEnvKey means input comes from the value of a specific environment key.
	SourceEnvKey DataSource = iota
	// SourceStdin means input comes from stdin.
	SourceStdin DataSource = iota
	// SourceFile means input comes from a file.
	SourceFile DataSource = iota
)

var dataFormats = map[string]SupportedType{
	"json": TypeJSON,
	"yaml": TypeYAML,
	"yml":  TypeYAML,
	"env":  TypeEnv,
}

const (
	FormatAuto = "auto"
)

type Options struct {
	Logging struct {
		Level  string `help:"logging level" default:"warning"`
		Format string `help:"logging format (${enum})" enum:"console,json" default:"console"`
	} `embed:"" prefix:"logging."`

	DumpInputData bool `name:"debug" help:"Print Go serialization to stderr and then exit"`

	UseEnvKey    bool   `help:"Treat --input as an environment key name to read. This is equivalent to specifying --format=envkey"`
	Format       string `help:"Input data format (may specify multiple values)" enum:"auto,env,envkey,json,yml,yaml" default:"auto" short:"f"`
	IncludeEnv   bool   `help:"Implicitly include environment variables in addition to any supplied data"`
	TemplateFile string `name:"template" help:"Template file to process" short:"t" required:""`
	DataFile     string `name:"input" help:"Input data path. Leave blank for stdin." short:"i"`
	OutputFile   string `name:"output" help:"Output file. Leave blank for stdout." short:"o"`

	TarFile bool `name:"tar" help:"Output content as a tar file"`

	CustomFilters     string `name:"enable-filters" help:"Enable custom P2 filters"`
	CustomFilterNoops bool   `name:"enable-noop-filters" help:"Enable all custom filters in no-op mode. Supercedes --enable-filters."`

	Autoescape bool `help:"Enable autoescaping"`

	DirectoryMode     bool   `help:"Treat template path as directory-tree, output path as target directory"`
	FilenameSubstrDel string `name:"directory-mode-filename-substr-del" help:"Delete a given substring in the output filename (only applies to --directory-mode)"`

	Version bool `help:"Print the version and exit"`
}

// CustomFilterSpec is a map of custom filters p2 implements. These are gated
// behind the --enable-filter command line option as they can have unexpected
// or even unsafe behavior (i.e. templates gain the ability to make filesystem
// modifications). Disabled filters are stubbed out to allow for debugging.
type CustomFilterSpec struct {
	FilterFunc pongo2.FilterFunction
	NoopFunc   pongo2.FilterFunction
}

var customFilters = map[string]CustomFilterSpec{
	"write_file": {filterWriteFile, filterNoopPassthru},
	"make_dirs":  {filterMakeDirs, filterNoopPassthru},
}

func readRawInput(env map[string]string, stdIn io.Reader, name string, source DataSource) ([]byte, error) {
	logger := zap.L()
	var data []byte
	var err error
	switch source {
	case SourceStdin:
		// Read from stdin
		name = "-"
		data, err = ioutil.ReadAll(stdIn)
	case SourceFile:
		// Read from file
		data, err = ioutil.ReadFile(name)
	case SourceEnvKey:
		// Read from environment key
		data = []byte(env[name])
	default:
		logger.Error("Invalid data source specified.", zap.String("filename", name))
		return []byte{}, err
	}

	if err != nil {
		logger.Error("Could not read data", zap.Error(err), zap.String("filename", name))
		return []byte{}, err
	}
	return data, nil
}

type EntrypointArgs struct {
	StdIn  io.Reader
	StdOut io.Writer
	StdErr io.Writer
	Env    map[string]string
	Args   []string
}

// Entrypoint implements the actual functionality of the program so it can be called inline from testing.
// env is normally passed the environment variable array.
func Entrypoint(args EntrypointArgs) int {
	var err error
	options := Options{}

	deferredLogs := []string{}

	// Filter invalid environment variables.
	args.Env = lo.OmitBy(args.Env, func(key string, value string) bool {
		return !reIdentifiers.MatchString(key)
	})

	// Command line parsing can now happen
	parser := lo.Must(kong.New(&options, kong.Description(description)))
	_, err = parser.Parse(args.Args)
	if err != nil {
		fmt.Fprintf(args.StdErr, "Argument error: %s", err.Error())
		return 1
	}

	// Initialize logging as soon as possible
	logConfig := zap.NewProductionConfig()
	if err := logConfig.Level.UnmarshalText([]byte(options.Logging.Level)); err != nil {
		deferredLogs = append(deferredLogs, err.Error())
	}
	logConfig.Encoding = options.Logging.Format

	logger, err := logConfig.Build()
	if err != nil {
		// Error unhandled since this is a very early failure
		_, _ = io.WriteString(args.StdErr, "Failure while building logger")
		return 1
	}

	// Install as the global logger
	zap.ReplaceGlobals(logger)

	if options.Version {
		lo.Must(fmt.Fprintf(args.StdOut, "%s", Version))
		return 0
	}

	if options.DirectoryMode {
		tst, _ := os.Stat(options.TemplateFile)
		if !tst.IsDir() {
			logger.Error("Template path must be a directory in directory mode", zap.String("template_file", options.TemplateFile))
			return 1
		}

		ost, err := os.Stat(options.OutputFile)
		if err == nil {
			if !ost.IsDir() {
				logger.Error("Output path must be an existing directory in directory mode", zap.String("template_file", options.TemplateFile))
				return 1
			}
		} else {
			logger.Error("Error calling stat on output path", zap.Error(err))
			return 1
		}
	}

	// Register custom filter functions.
	if options.CustomFilterNoops {
		for filter, spec := range customFilters {
			pongo2.RegisterFilter(filter, spec.NoopFunc)
		}
	} else {
		// Register enabled custom-filters
		if options.CustomFilters != "" {
			for _, filter := range strings.Split(options.CustomFilters, ",") {
				spec, found := customFilters[filter]
				if !found {
					logger.Error("This version of p2 does not support the specified custom filter", zap.String("filter_name", filter))
					return 1
				}

				pongo2.RegisterFilter(filter, spec.FilterFunc)
			}
		}
	}

	// Register the default custom filters. These are replaced each file execution later, but we
	// need the names in-scope here.
	pongo2.RegisterFilter("SetOwner", templating.FilterSetOwner)
	pongo2.RegisterFilter("SetGroup", templating.FilterSetGroup)
	pongo2.RegisterFilter("SetMode", templating.FilterSetMode)

	// Standard suite of custom helpers
	pongo2.RegisterFilter("indent", templating.FilterIndent)

	pongo2.RegisterFilter("to_json", templating.FilterToJson)
	pongo2.RegisterFilter("to_yaml", templating.FilterToYaml)
	pongo2.RegisterFilter("to_toml", templating.FilterToToml)

	pongo2.RegisterFilter("to_base64", templating.FilterToBase64)
	pongo2.RegisterFilter("from_base64", templating.FilterFromBase64)

	pongo2.RegisterFilter("string", templating.FilterString)
	pongo2.RegisterFilter("bytes", templating.FilterBytes)

	pongo2.RegisterFilter("to_gzip", templating.FilterToGzip)
	pongo2.RegisterFilter("from_gzip", templating.FilterFromGzip)

	// Determine mode of operations
	var fileFormat SupportedType
	inputSource := SourceEnv

	if options.Format == FormatAuto && options.DataFile == "" {
		fileFormat = TypeEnv
		inputSource = SourceEnv
	} else if options.Format == FormatAuto && options.DataFile != "" {
		var ok bool
		fileFormat, ok = dataFormats[strings.TrimLeft(path.Ext(options.DataFile), ".")]
		if !ok {
			logger.Error("Unrecognized file extension. If the file is in a supported format, try specifying it explicitly.")
			return 1
		}
		inputSource = SourceFile
	} else if options.Format != "" && options.DataFile == "" {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			logger.Error("Unsupported input format", zap.String("format", options.Format))
			return 1
		}
		inputSource = SourceStdin
	} else {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			logger.Error("Unsupported input format:", zap.String("format", options.Format))
			return 1
		}
		inputSource = SourceFile
	}

	if options.UseEnvKey && options.DataFile == "" {
		logger.Error("--use-env-key is incompatible with stdin file input.")
		return 1
	} else if options.UseEnvKey {
		inputSource = SourceEnvKey
	}

	// Get the input context
	inputData := make(map[string]interface{})

	switch fileFormat {
	case TypeEnv:
		if options.IncludeEnv {
			logger.Warn("--include-env has no effect when data source is already the environment")
		}
		err = func(inputData map[string]interface{}) error {
			if inputSource != SourceEnv {
				rawInput, err := readRawInput(args.Env, args.StdIn, options.DataFile, inputSource)
				if err != nil {
					return err
				}
				lineScanner := bufio.NewScanner(bytes.NewReader(rawInput))
				for lineScanner.Scan() {
					keyval := lineScanner.Text()
					splitKeyVal := strings.SplitN(lineScanner.Text(), "=", 2)
					if len(splitKeyVal) != 2 {
						return error(errdefs.ErrorEnvironmentVariables{
							Reason:    "Could not find an equals value to split on",
							RawEnvVar: keyval,
						})
					}
					// File values should support sh-escaped strings, whereas the
					// raw environment will accept *anything* after the = sign.
					values, err := shellquote.Split(splitKeyVal[1])
					if err != nil {
						return error(errdefs.ErrorEnvironmentVariables{
							Reason:    err.Error(),
							RawEnvVar: keyval,
						})
					}

					// Detect if more than 1 values was parsed - this is invalid in
					// sourced files, and we don't want to try parsing shell arrays.
					if len(values) > 1 {
						return error(errdefs.ErrorEnvironmentVariables{
							Reason:    "Improperly escaped environment variable. p2 does not parse arrays.",
							RawEnvVar: keyval,
						})
					}

					inputData[splitKeyVal[0]] = values[0]
				}
			} else {
				for k, v := range args.Env {
					inputData[k] = v
				}
			}
			return nil
		}(inputData)
	case TypeYAML:
		var rawInput []byte
		rawInput, err = readRawInput(args.Env, args.StdIn, options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = yaml.Unmarshal(rawInput, &inputData)
	case TypeJSON:
		var rawInput []byte
		rawInput, err = readRawInput(args.Env, args.StdIn, options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = json.Unmarshal(rawInput, &inputData)
	default:
		logger.Error("Unknown input format.")
		return 1
	}

	if err != nil {
		logger.Error("Error parsing input data:", zap.Error(err), zap.String("template", options.TemplateFile), zap.String("data", options.DataFile))
		return 1
	}

	if options.IncludeEnv {
		logger.Info("Including environment variables")
		for k, v := range args.Env {
			inputData[k] = v
		}
	}

	if options.DumpInputData {
		_, _ = fmt.Fprintln(args.StdErr, inputData)
	}

	if !options.Autoescape {
		pongo2.SetAutoescape(false)
	}

	// Load all templates and their relative paths
	templates := make(map[string]*pongo2.Template)
	inputMaps := make(map[string]string)

	if options.DirectoryMode {
		err := filepath.Walk(options.TemplateFile, func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(options.TemplateFile, path)
			if err != nil {
				return err
			}

			logger.Debug("Template file", zap.String("template_file", relPath))
			tmpl := LoadTemplate(path)
			if tmpl == nil {
				logger.Error("Error loading template", zap.String("path", path))
				return errors.New("Error loading template")
			}

			newRelPath := transformFileName(relPath, options)
			outputPath, err := filepath.Abs(filepath.Join(options.OutputFile, newRelPath))
			if err != nil {
				logger.Error("Could not determine absolute path of output file", zap.String("output_file", options.OutputFile), zap.String("new_rel_path", newRelPath))
				return errors.New("Error determining output path")
			}

			templates[outputPath] = tmpl
			inputMaps[outputPath] = path
			return nil
		})
		if err != nil {
			logger.Error("Error while walking input directory path", zap.String("template_file", options.TemplateFile))
			return 1
		}
	} else {
		// Just load the template as the output file
		tmpl := LoadTemplate(options.TemplateFile)
		if tmpl == nil {
			logger.Error("Error loading template:", zap.String("template_file", options.TemplateFile))
			return 1
		}

		absOutputFile := ""
		if options.OutputFile != "" {
			absOutputFile, err = filepath.Abs(options.OutputFile)
			if err != nil {
				logger.Error("Could not determine absolute path of output file", zap.Error(err))
				return 1
			}
		}

		templates[absOutputFile] = tmpl
	}

	// If we're in directory mode then we'll create a directory tree. If we're not, then we won't.
	if options.DirectoryMode {
		for outputPath := range templates {
			if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(0777)); err != nil {
				logger.Error("Error while creating directory for output")
			}
		}
	}

	rootDir := options.OutputFile
	if !options.DirectoryMode {
		rootDir, err = os.Getwd()
		if err != nil {
			logger.Error("Error getting working directory", zap.Error(err))
			return 1
		}
	}

	rootDir, err = filepath.Abs(rootDir)
	if err != nil {
		logger.Error("Could not determine absolute path of root dir", zap.Error(err))
		return 1
	}

	templateEngine := templating.TemplateEngine{StdOut: args.StdOut}

	failed := false
	for outputPath, tmpl := range templates {
		if err := templateEngine.ExecuteTemplate(tmpl, inputData, outputPath, rootDir); err != nil {
			logger.Error("Failed to execute template", zap.Error(err), zap.String("template_path", inputMaps[outputPath]), zap.String("output_path", outputPath))
			failed = true
		}

	}

	if failed {
		logger.Error("Errors encountered during template processing")
		return 1
	}

	return 0
}

func LoadTemplate(templatePath string) *pongo2.Template {
	logger := zap.L()
	// Load template
	templateBytes, err := ioutil.ReadFile(templatePath)
	if err != nil {
		logger.Error("Could not read template file", zap.Error(err))
		return nil
	}

	templateString := string(templateBytes)

	tmpl, err := pongo2.FromString(templateString)
	if err != nil {
		logger.Error("Could not template file", zap.Error(err), zap.String("template", templatePath))
		return nil
	}

	return tmpl
}

// transformFileName applies modifications specified by the user to the resulting output filename
// This function is only invoked in Directory Mode.
func transformFileName(relPath string, options Options) string {
	filename := filepath.Base(relPath)
	transformedFileName := strings.Replace(filename, options.FilenameSubstrDel, "", -1)
	return filepath.Join(filepath.Dir(relPath), transformedFileName)
}
