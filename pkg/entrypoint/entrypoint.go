/*
A Golang replica of j2cli from Python. Designed for allowing easy templating
of files using Jinja2-like syntax (from the Pongo2 engine).

Extremely useful for building Docker files when you don't want to pull in all of
python.
*/

package entrypoint

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wrouesnel/p2cli/pkg/fileconsts"

	"github.com/pkg/errors"
	"github.com/wrouesnel/p2cli/version"

	"github.com/alecthomas/kong"
	"github.com/flosch/pongo2/v6"
	"github.com/samber/lo"
	"github.com/wrouesnel/p2cli/pkg/errdefs"

	"github.com/kballard/go-shellquote"
	"github.com/wrouesnel/p2cli/pkg/templating"

	"gopkg.in/yaml.v2"

	"go.uber.org/zap"
)

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

//nolint:gochecknoglobals
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
		Level  string `default:"warning" help:"logging level"`
		Format string `default:"console" enum:"console,json"  help:"logging format (${enum})"`
	} `embed:"" prefix:"logging."`

	DumpInputData bool `help:"Print Go serialization to stderr and then exit" name:"debug"`

	UseEnvKey    bool   `help:"Treat --input as an environment key name to read. This is equivalent to specifying --format=envkey"`
	Format       string `default:"auto"                                                                                            enum:"auto,env,envkey,json,yml,yaml" help:"Input data format (may specify multiple values)" short:"f"`
	IncludeEnv   bool   `help:"Implicitly include environment variables in addition to any supplied data"`
	TemplateFile string `help:"Template file to process"                                                                           name:"template"                      required:""                                            short:"t"`
	DataFile     string `help:"Input data path. Leave blank for stdin."                                                            name:"input"                         short:"i"`
	OutputFile   string `help:"Output file. Leave blank for stdout."                                                               name:"output"                        short:"o"`

	TarFile string `default:"" help:"Output content as a tar file with the given name or to stdout (-)" name:"tar"`

	CustomFilters     string `help:"Enable custom P2 filters"                                              name:"enable-filters"`
	CustomFilterNoops bool   `help:"Enable all custom filters in no-op mode. Supercedes --enable-filters." name:"enable-noop-filters"`

	Autoescape bool `help:"Enable autoescaping"`

	DirectoryMode     bool   `help:"Treat template path as directory-tree, output path as target directory"`
	FilenameSubstrDel string `help:"Delete a given substring in the output filename (only applies to --directory-mode)" name:"directory-mode-filename-substr-del"`

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

//nolint:gochecknoglobals
var customFilters = map[string]CustomFilterSpec{
	"write_file": {filterWriteFile, filterNoopPassthru},
	"make_dirs":  {filterMakeDirs, filterNoopPassthru},
}

func readRawInput(env map[string]string, stdIn io.Reader, name string, source DataSource) ([]byte, error) {
	logger := zap.L()
	var data []byte
	var err error
	//nolint:exhaustive
	switch source {
	case SourceStdin:
		// Read from stdin
		name = "-"
		data, err = io.ReadAll(stdIn)
	case SourceFile:
		// Read from file
		data, err = os.ReadFile(name)
	case SourceEnvKey:
		// Read from environment key
		data = []byte(env[name])
	default:
		logger.Error("Invalid data source specified.", zap.String("filename", name))
		return []byte{}, errors.Wrap(err, "readRawInput")
	}

	if err != nil {
		logger.Error("Could not read data", zap.Error(err), zap.String("filename", name))
		return []byte{}, errors.Wrap(err, "readRawInput")
	}
	return data, nil
}

type LaunchArgs struct {
	StdIn  io.Reader
	StdOut io.Writer
	StdErr io.Writer
	Env    map[string]string
	Args   []string
}

// Entrypoint implements the actual functionality of the program so it can be called inline from testing.
// env is normally passed the environment variable array.
//
//nolint:funlen,gocognit,gocyclo,cyclop,maintidx
func Entrypoint(args LaunchArgs) int {
	var err error
	options := Options{}

	deferredLogs := []string{}

	// Filter invalid environment variables.
	args.Env = lo.OmitBy(args.Env, func(key string, value string) bool {
		return !reIdentifiers.MatchString(key)
	})

	// Command line parsing can now happen
	parser := lo.Must(kong.New(&options, kong.Description(version.Description)))
	_, err = parser.Parse(args.Args)
	if err != nil {
		_, _ = fmt.Fprintf(args.StdErr, "Argument error: %s", err.Error())
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
		for _, line := range deferredLogs {
			_, _ = io.WriteString(args.StdErr, line)
		}
		_, _ = io.WriteString(args.StdErr, "Failure while building logger")
		return 1
	}

	// Install as the global logger
	zap.ReplaceGlobals(logger)

	if options.Version {
		lo.Must(fmt.Fprintf(args.StdOut, "%s", version.Version))
		return 0
	}

	//nolint:nestif
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
		} else if options.TarFile == "" {
			// Allow non-existent output path if outputting to a tar file
			logger.Error("Error calling stat on output path", zap.Error(err))
			return 1
		}
	}

	// Register custom filter functions.
	if options.CustomFilterNoops {
		for filter, spec := range customFilters {
			_ = pongo2.RegisterFilter(filter, spec.NoopFunc)
		}
	} else if options.CustomFilters != "" {
		for _, filter := range strings.Split(options.CustomFilters, ",") {
			spec, found := customFilters[filter]
			if !found {
				logger.Error("This version of p2 does not support the specified custom filter", zap.String("filter_name", filter))
				return 1
			}

			_ = pongo2.RegisterFilter(filter, spec.FilterFunc)
		}
	}

	// filterSet is passed to executeTemplate so it can vary parameters within the filter space as it goes.
	filterSet := templating.FilterSet{OutputFileName: "", Chown: os.Chown, Chmod: os.Chmod}

	// Register the default custom filters. These are replaced each file execution later, but we
	// need the names in-scope here.
	_ = pongo2.RegisterFilter("SetOwner", filterSet.FilterSetOwner)
	_ = pongo2.RegisterFilter("SetGroup", filterSet.FilterSetGroup)
	_ = pongo2.RegisterFilter("SetMode", filterSet.FilterSetMode)

	// Standard suite of custom helpers
	_ = pongo2.RegisterFilter("indent", filterSet.FilterIndent)

	_ = pongo2.RegisterFilter("to_json", filterSet.FilterToJSON)
	_ = pongo2.RegisterFilter("to_yaml", filterSet.FilterToYAML)
	_ = pongo2.RegisterFilter("to_toml", filterSet.FilterToTOML)

	_ = pongo2.RegisterFilter("to_base64", filterSet.FilterToBase64)
	_ = pongo2.RegisterFilter("from_base64", filterSet.FilterFromBase64)

	_ = pongo2.RegisterFilter("string", filterSet.FilterString)
	_ = pongo2.RegisterFilter("bytes", filterSet.FilterBytes)

	_ = pongo2.RegisterFilter("to_gzip", filterSet.FilterToGzip)
	_ = pongo2.RegisterFilter("from_gzip", filterSet.FilterFromGzip)

	// Determine mode of operations
	var fileFormat SupportedType
	inputSource := SourceEnv

	switch {
	case options.Format == FormatAuto && options.DataFile == "":
		fileFormat = TypeEnv
		inputSource = SourceEnv
	case options.Format == FormatAuto && options.DataFile != "":
		var ok bool
		fileFormat, ok = dataFormats[strings.TrimLeft(path.Ext(options.DataFile), ".")]
		if !ok {
			logger.Error("Unrecognized file extension. If the file is in a supported format, try specifying it explicitly.")
			return 1
		}
		inputSource = SourceFile
	case options.Format != "" && options.DataFile == "":
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			logger.Error("Unsupported input format", zap.String("format", options.Format))
			return 1
		}
		inputSource = SourceStdin
	default:
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
			//nolint:nestif
			if inputSource != SourceEnv {
				rawInput, err := readRawInput(args.Env, args.StdIn, options.DataFile, inputSource)
				if err != nil {
					return err
				}
				lineScanner := bufio.NewScanner(bytes.NewReader(rawInput))
				for lineScanner.Scan() {
					keyval := lineScanner.Text()
					const expectedFragments = 2
					splitKeyVal := strings.SplitN(lineScanner.Text(), "=", expectedFragments)
					if len(splitKeyVal) != expectedFragments {
						return error(errdefs.EnvironmentVariablesError{
							Reason:    "Could not find an equals value to split on",
							RawEnvVar: keyval,
						})
					}
					// File values should support sh-escaped strings, whereas the
					// raw environment will accept *anything* after the = sign.
					values, err := shellquote.Split(splitKeyVal[1])
					if err != nil {
						return error(errdefs.EnvironmentVariablesError{
							Reason:    err.Error(),
							RawEnvVar: keyval,
						})
					}

					// Detect if more than 1 values was parsed - this is invalid in
					// sourced files, and we don't want to try parsing shell arrays.
					if len(values) > 1 {
						return error(errdefs.EnvironmentVariablesError{
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
	case TypeUnknown:
		logger.Error("Unknown input format.")
		return 1
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
	templates := make(map[string]*templating.LoadedTemplate)
	inputMaps := make(map[string]string)

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

	//nolint:nestif
	if options.DirectoryMode {
		err := filepath.Walk(options.TemplateFile, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				logger.Error("Error walking directory tree", zap.Error(err))
				return err
			}

			if info.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(options.TemplateFile, path)
			if err != nil {
				return errors.Wrap(err, "DirectoryMode")
			}

			logger.Debug("Template file", zap.String("template_file", relPath))
			tmpl := templating.LoadTemplate(path)
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

			p2cliCtx := make(map[string]string)
			// Set the global p2 variables on the template sets
			p2cliCtx["OutputPath"] = outputPath
			p2cliCtx["OutputName"] = filepath.Base(outputPath)
			p2cliCtx["OutputDir"] = filepath.Dir(outputPath)
			p2cliCtx["OutputRelPath"], err = filepath.Rel(rootDir, outputPath)
			if err != nil {
				return fmt.Errorf("could not determine relative output path: %w", err)
			}
			p2cliCtx["OutputRelDir"], err = filepath.Rel(rootDir, filepath.Dir(outputPath))
			if err != nil {
				return fmt.Errorf("could not determine relative output dir: %w", err)
			}

			ctx := make(pongo2.Context)
			ctx["p2"] = p2cliCtx
			templateSet := pongo2.NewSet(outputPath, pongo2.DefaultLoader)
			templateSet.Globals.Update(ctx)

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
		tmpl := templating.LoadTemplate(options.TemplateFile)
		if tmpl == nil {
			logger.Error("Error loading template:", zap.String("template_file", options.TemplateFile))
			return 1
		}

		outputPath := ""
		if options.OutputFile != "" {
			outputPath, err = filepath.Abs(options.OutputFile)
			if err != nil {
				logger.Error("Could not determine absolute path of output file", zap.Error(err))
				return 1
			}
		}

		p2cliCtx := make(map[string]string)
		p2cliCtx["OutputPath"] = templating.StdOutVal
		p2cliCtx["OutputName"] = templating.StdOutVal
		p2cliCtx["OutputDir"] = rootDir
		p2cliCtx["OutputRelPath"] = templating.StdOutVal
		p2cliCtx["OutputRelDir"] = "."

		ctx := make(pongo2.Context)
		ctx["p2"] = p2cliCtx

		tmpl.TemplateSet.Globals.Update(ctx)

		ctx["p2"] = p2cliCtx

		templates[outputPath] = tmpl
		inputMaps[outputPath] = templating.StdOutVal
	}

	// Configure output path
	var templateEngine *templating.TemplateEngine
	switch {
	case options.TarFile != "":
		var fileOut io.Writer
		if options.TarFile == "-" {
			fileOut = args.StdOut
		} else {
			fileOut, err = os.OpenFile(options.TarFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileconsts.OS_ALL_RWX))
			if err != nil {
				logger.Error("Error opening tar file for output", zap.Error(err))
				return 1
			}
		}
		tarWriter := tar.NewWriter(fileOut)
		templateEngine = &templating.TemplateEngine{
			PrepareOutput: func(inputData pongo2.Context, outputPath string) (io.Writer, func() error, error) {
				relPath, err := filepath.Rel(rootDir, outputPath)
				if err != nil {
					return nil, nil, fmt.Errorf("could not determine relative output path: %w", err)
				}

				// Setup a new header
				header := &tar.Header{
					Typeflag: tar.TypeReg,
					Name:     filepath.Join(options.OutputFile, relPath),
					//Linkname:   "",
					Size:       0,
					Mode:       fileconsts.OS_ALL_RWX,
					Uid:        0,
					Gid:        0,
					Uname:      "",
					Gname:      "",
					ModTime:    time.Time{},
					AccessTime: time.Time{},
					ChangeTime: time.Time{},
					//Devmajor:   0,
					//Devminor:   0,
					//Xattrs:     nil,
					//PAXRecords: nil,
					//Format:     0,
				}

				// Modify filterSet so we receive the Chown/Chmod operations
				filterSet.Chown = func(name string, uid, gid int) error {
					if uid != -1 {
						header.Uid = uid
					}
					if gid != -1 {
						header.Gid = gid
					}
					return nil
				}
				filterSet.Chmod = func(name string, mode os.FileMode) error {
					header.Mode = int64(mode)
					return nil
				}

				// Setup a buffer for the output
				buf := new(bytes.Buffer)

				finalizer := func() error {
					header.Size = int64(buf.Len())
					if err := tarWriter.WriteHeader(header); err != nil {
						return errors.Wrap(err, "entrypoint: write header for tar file failed")
					}
					if _, err := tarWriter.Write(buf.Bytes()); err != nil {
						return errors.Wrap(err, "entrypoint: write file body for tar file failed")
					}
					return nil
				}

				return buf, finalizer, nil
			},
		}

	case options.DirectoryMode:
		for outputPath := range templates {
			if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(fileconsts.OS_ALL_RWX)); err != nil {
				logger.Error("Error while creating directory for output")
			}
		}
		templateEngine = &templating.TemplateEngine{
			PrepareOutput: func(inputData pongo2.Context, outputPath string) (io.Writer, func() error, error) {
				origWorkDir, err := os.Getwd()
				if err != nil {
					return nil, nil, errors.Wrap(err, "DirectoryMode")
				}

				if err := os.Chdir(filepath.Dir(outputPath)); err != nil {
					return nil, nil, fmt.Errorf("could not change to template output path directory: %w", err)
				}

				fileOut, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileconsts.OS_ALL_RWX))
				if err != nil {
					return nil, nil, errors.Wrap(err, "entrypoint: error opening output file for writing")
				}

				finalizer := func() error {
					if err := os.Chdir(origWorkDir); err != nil {
						return fmt.Errorf("could not change back to original working directory: %w", err)
					}

					if err := fileOut.Close(); err != nil {
						return errors.Wrap(err, "entrypoint: error closing file after writing")
					}
					return nil
				}

				return fileOut, finalizer, nil
			},
		}

	case options.OutputFile != "-" && options.OutputFile != "":
		templateEngine = &templating.TemplateEngine{
			PrepareOutput: func(inputData pongo2.Context, outputPath string) (io.Writer, func() error, error) {
				fileOut, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileconsts.OS_ALL_RWX))
				if err != nil {
					return nil, nil, errors.Wrap(err, "entrypoint: error opening output file for writing")
				}

				finalizer := func() error {
					if err := fileOut.Close(); err != nil {
						return errors.Wrap(err, "entrypoint: error closing file after writing")
					}
					return nil
				}

				return fileOut, finalizer, nil
			},
		}

	case options.OutputFile == "-" || options.OutputFile == "":
		templateEngine = &templating.TemplateEngine{
			PrepareOutput: func(inputData pongo2.Context, outputPath string) (io.Writer, func() error, error) {
				return args.StdOut, nil, nil
			},
		}
	}

	failed := false
	for outputPath, tmpl := range templates {
		if err := templateEngine.ExecuteTemplate(&filterSet, tmpl, inputData, outputPath); err != nil {
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

// transformFileName applies modifications specified by the user to the resulting output filename
// This function is only invoked in Directory Mode.
func transformFileName(relPath string, options Options) string {
	filename := filepath.Base(relPath)
	transformedFileName := strings.ReplaceAll(filename, options.FilenameSubstrDel, "")
	return filepath.Join(filepath.Dir(relPath), transformedFileName)
}
