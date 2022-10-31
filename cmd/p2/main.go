/*
A Golang replica of j2cli from Python. Designed for allowing easy templating
of files using Jinja2-like syntax (from the Pongo2 engine).

Extremely useful for building Docker files when you don't want to pull in all of
python.
*/

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/flosch/pongo2/v4"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kballard/go-shellquote"
	"github.com/samber/lo"
	"github.com/wrouesnel/p2cli/pkg/templating"

	log "github.com/wrouesnel/go.log"
	"gopkg.in/yaml.v2"
)

// Version is populated by the build system.
var Version = "development"

const description = "Pongo2 based command line templating tool"

// Copied from pongo2.context
var reIdentifiers = regexp.MustCompile("^[a-zA-Z0-9_]+$")

// SupportedType is an enumeration of data types we support.
type SupportedType int

const (
	// TypeUnknown is the default error type
	TypeUnknown SupportedType = iota
	// TypeJSON is JSON
	TypeJSON SupportedType = iota
	// TypeYAML is YAML
	TypeYAML SupportedType = iota
	// TypeEnv is key=value pseudo environment files.
	TypeEnv SupportedType = iota
)

// DataSource is an enumeration of the sources of input data we can take.
type DataSource int

const (
	// SourceEnv means input comes from environment variables
	SourceEnv DataSource = iota
	// SourceEnvKey means input comes from the value of a specific environment key
	SourceEnvKey DataSource = iota
	// SourceStdin means input comes from stdin
	SourceStdin DataSource = iota
	// SourceFile means input comes from a file
	SourceFile DataSource = iota
)

var dataFormats = map[string]SupportedType{
	"json": TypeJSON,
	"yaml": TypeYAML,
	"yml":  TypeYAML,
	"env":  TypeEnv,
}

type Options struct {
	DumpInputData bool `name:"debug" help:"Print Go serialization to stderr and then exit"`

	Format       string `help:"Input data format (may specify multiple values)" enum:"auto,env,envkey,json,yml,yaml" default:"auto"`
	IncludeEnv   bool   `help:"Implicitly include environment variables in addition to any supplied data"`
	TemplateFile string `name:"template" help:"Template file to process" short:"t" required:""`
	DataFile     string `name:"input" help:"Input data path. Leave blank for stdin." short:"i"`
	OutputFile   string `name:"output" help:"Output file. Leave blank for stdout." short:"o"`

	TarFile bool `name:"tar" help:"Output content as a tar file"`

	CustomFilters     string `name:"enable-filters" help:"Enable custom P2 filters"`
	CustomFilterNoops bool   `name:"enable-noop-filters" help:"Enable all custom filters in no-op mode. Supercedes --enable-filters."`

	Autoescape bool `help:"Enable autoescaping"`

	DirectoryMode     bool   `help:"Treat template path as directory-tree, output path as target directory"`
	FilenameSubstrDel string `help:"Delete a given substring in the output filename (only applies to --directory-mode)"`
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

var (
	inputData = make(map[string]interface{})
)

// ErrorEnvironmentVariables is raised when an environment variable is improperly formatted
type ErrorEnvironmentVariables struct {
	Reason    string
	RawEnvVar string
}

// Error implements error
func (eev ErrorEnvironmentVariables) Error() string {
	return fmt.Sprintf("%s: %s", eev.Reason, eev.RawEnvVar)
}

func readRawInput(name string, source DataSource) ([]byte, error) {
	var data []byte
	var err error
	switch source {
	case SourceStdin:
		// Read from stdin
		name = "-"
		data, err = ioutil.ReadAll(os.Stdin)
	case SourceFile:
		// Read from file
		data, err = ioutil.ReadFile(name)
	case SourceEnvKey:
		// Read from environment key
		data = []byte(os.Getenv(name))
	default:
		log.With("filename", name).Errorln("Invalid data source specified.")
		return []byte{}, err
	}

	if err != nil {
		log.With("filename", name).Errorln("Could not read data:", err)
		return []byte{}, err
	}
	return data, nil
}

func main() {
	os.Exit(realMain(os.Environ()))
}

// realMain implements the actual functionality of the program so it can be called inline from testing.
// env is normally passed the environment variable array.
func realMain(env []string) int {
	options := Options{}

	// Command line parsing can now happen
	kong.Parse(&options,
		kong.Description(description))

	if options.DirectoryMode {
		tst, _ := os.Stat(options.TemplateFile)
		if !tst.IsDir() {
			log.Errorln("Template path must be a directory in directory mode:", options.TemplateFile)
			return 1
		}

		ost, err := os.Stat(options.OutputFile)
		if err == nil {
			if !ost.IsDir() {
				log.Errorln("Output path must be an existing directory in directory mode:", options.TemplateFile)
				return 1
			}
		} else {
			log.Errorln("Error calling stat on output path:", err.Error())
			return 1
		}
	}

	// Register custom filter functions.
	if options.CustomFilterNoops {
		for filter, spec := range customFilters {
			lo.Must0(pongo2.RegisterFilter(filter, spec.NoopFunc))
		}
	} else {
		// Register enabled custom-filters
		if options.CustomFilters != "" {
			for _, filter := range strings.Split(options.CustomFilters, ",") {
				spec, found := customFilters[filter]
				if !found {
					log.Errorln("This version of p2 does not support the", filter, "custom filter.")
					return 1
				}

				lo.Must0(pongo2.RegisterFilter(filter, spec.FilterFunc))
			}
		}
	}

	// Register the default custom filters. These are replaced each file execution later, but we
	// need the names in-scope here.
	lo.Must0(pongo2.RegisterFilter("SetOwner", templating.FilterSetOwner))
	lo.Must0(pongo2.RegisterFilter("SetGroup", templating.FilterSetGroup))
	lo.Must0(pongo2.RegisterFilter("SetMode", templating.FilterSetMode))

	// Standard suite of custom helpers
	lo.Must0(pongo2.RegisterFilter("indent", templating.FilterIndent))

	lo.Must0(pongo2.RegisterFilter("to_json", templating.FilterToJson))
	lo.Must0(pongo2.RegisterFilter("to_yaml", templating.FilterToYaml))
	lo.Must0(pongo2.RegisterFilter("to_toml", templating.FilterToToml))

	lo.Must0(pongo2.RegisterFilter("to_base64", templating.FilterToBase64))
	lo.Must0(pongo2.RegisterFilter("from_base64", templating.FilterFromBase64))

	lo.Must0(pongo2.RegisterFilter("string", templating.FilterString))
	lo.Must0(pongo2.RegisterFilter("bytes", templating.FilterBytes))

	lo.Must0(pongo2.RegisterFilter("to_gzip", templating.FilterToGzip))
	lo.Must0(pongo2.RegisterFilter("from_gzip", templating.FilterFromGzip))

	// Determine mode of operations
	var fileFormat SupportedType
	inputSource := SourceEnv

	if options.DataFile == "" && options.Format == "" {
		fileFormat = TypeEnv
		inputSource = SourceEnv
	} else if options.Format == "envkey" && options.DataFile == "" {
		log.Errorln("envkey is incompatible with stdin file input.")
		return 1
	} else if options.Format == "envkey" && options.DataFile != "" {
		inputSource = SourceEnvKey
	} else if options.DataFile != "" && options.Format == "" {
		var ok bool
		fileFormat, ok = dataFormats[strings.TrimLeft(path.Ext(options.DataFile), ".")]
		if !ok {
			log.Errorln("Unrecognized file extension. If the file is in a supported format, try specifying it explicitly.")
			return 1
		}
		inputSource = SourceFile
	} else if options.DataFile == "" && options.Format != "" {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Errorln("Unsupported input format:", options.Format)
			return 1
		}
		inputSource = SourceStdin
	} else {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Errorln("Unsupported input format:", options.Format)
			return 1
		}
		inputSource = SourceFile
	}

	var err error
	// Get the input context
	switch fileFormat {
	case TypeEnv:
		if options.IncludeEnv {
			log.Warnln("--include-env has no effect when data source is already the environment")
		}
		err = func(inputData map[string]interface{}) error {
			if inputSource != SourceEnv {
				rawInput, err := readRawInput(options.DataFile, inputSource)
				if err != nil {
					return err
				}
				lineScanner := bufio.NewScanner(bytes.NewReader(rawInput))
				for lineScanner.Scan() {
					keyval := lineScanner.Text()
					splitKeyVal := strings.SplitN(lineScanner.Text(), "=", 2)
					if len(splitKeyVal) != 2 {
						return error(ErrorEnvironmentVariables{
							Reason:    "Could not find an equals value to split on",
							RawEnvVar: keyval,
						})
					}
					// File values should support sh-escaped strings, whereas the
					// raw environment will accept *anything* after the = sign.
					values, err := shellquote.Split(splitKeyVal[1])
					if err != nil {
						return error(ErrorEnvironmentVariables{
							Reason:    err.Error(),
							RawEnvVar: keyval,
						})
					}

					// Detect if more then 1 values was parsed - this is invalid in
					// sourced files, and we don't want to try parsing shell arrays.
					if len(values) > 1 {
						return error(ErrorEnvironmentVariables{
							Reason:    "Improperly escaped environment variable. p2 does not parse arrays.",
							RawEnvVar: keyval,
						})
					}

					inputData[splitKeyVal[0]] = values[0]
				}
			} else {
				if err := fromEnvironment(inputData); err != nil {
					return err
				}
			}
			return nil
		}(inputData)
	case TypeYAML:
		var rawInput []byte
		rawInput, err = readRawInput(options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = yaml.Unmarshal(rawInput, &inputData)
	case TypeJSON:
		var rawInput []byte
		rawInput, err = readRawInput(options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = json.Unmarshal(rawInput, &inputData)
	default:
		log.Errorln("Unknown input format.")
		return 1
	}

	if options.IncludeEnv {
		log.Infoln("Including environment variables")
		if err := fromEnvironment(inputData); err != nil {
			log.Errorln("Error while including environment variables", err)
			return 1
		}
	}

	if err != nil {
		log.With("template", options.TemplateFile).
			With("data", options.DataFile).
			Errorln("Error parsing input data:", err)
		return 1
	}

	if options.DumpInputData {
		_, _ = fmt.Fprintln(os.Stderr, inputData)
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

			log.Debugf("Template file: %v", relPath)
			tmpl := LoadTemplate(path)
			if tmpl == nil {
				log.Errorln("Error loading template:", path)
				return errors.New("Error loading template")
			}

			newRelPath := transformFileName(relPath, options)
			outputPath, err := filepath.Abs(filepath.Join(options.OutputFile, newRelPath))
			if err != nil {
				log.Errorln("Could not determine absolute path of output file", options.OutputFile, newRelPath)
				return errors.New("Error determining output path")
			}

			templates[outputPath] = tmpl
			inputMaps[outputPath] = path
			return nil
		})
		if err != nil {
			log.Errorln("Error while walking input directory path:", options.TemplateFile)
			return 1
		}
	} else {
		// Just load the template as the output file
		tmpl := LoadTemplate(options.TemplateFile)
		if tmpl == nil {
			log.Errorln("Error loading template:", options.TemplateFile)
			return 1
		}

		absOutputFile := ""
		if options.OutputFile != "" {
			absOutputFile, err = filepath.Abs(options.OutputFile)
			if err != nil {
				log.Errorln("Could not determine absolute path of output file:", err)
				return 1
			}
		}

		templates[absOutputFile] = tmpl
	}

	// If we're in directory mode then we'll create a directory tree. If we're not, then we won't.
	if options.DirectoryMode {
		for outputPath := range templates {
			if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(0777)); err != nil {
				log.Errorln("Error while creating directory for output")
			}
		}
	}

	rootDir := options.OutputFile
	if !options.DirectoryMode {
		rootDir, err = os.Getwd()
		if err != nil {
			log.Errorln("Error getting working directory:", err)
			return 1
		}
	}

	rootDir, err = filepath.Abs(rootDir)
	if err != nil {
		log.Errorln("Could not determine absolute path of root dir", err)
		return 1
	}

	failed := false
	for outputPath, tmpl := range templates {
		if err := templating.ExecuteTemplate(tmpl, inputData, outputPath, rootDir); err != nil {
			log.
				With("template_path", inputMaps[outputPath]).
				With("output_path", outputPath).Errorln("Failed to execute template:", err)
			failed = true
		}

	}

	if failed {
		log.Errorln("Errors encountered during template processing")
		return 1
	}

	return 0
}

func LoadTemplate(templatePath string) *pongo2.Template {
	// Load template
	templateBytes, err := ioutil.ReadFile(templatePath)
	if err != nil {
		log.Errorln("Could not read template file:", err)
		return nil
	}

	templateString := string(templateBytes)

	tmpl, err := pongo2.FromString(templateString)
	if err != nil {
		log.With("template", templatePath).
			Errorln("Could not template file:", err)
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
