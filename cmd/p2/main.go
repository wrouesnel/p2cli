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
	"github.com/alecthomas/kingpin"
	"github.com/wrouesnel/p2cli/pkg/templating"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flosch/pongo2/v4"
	"github.com/kballard/go-shellquote"
	"github.com/wrouesnel/go.log"
	"gopkg.in/yaml.v2"
)

// Version is populated by the build system.
var Version = "development"

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

// CustomFilterSpec is a map of custom filters p2 implements. These are gated
// behind the --enable-filter command line option as they can have unexpected
// or even unsafe behavior (i.e. templates gain the ability to make filesystem
//modifications). Disabled filters are stubbed out to allow for debugging.
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
	os.Exit(realMain())
}

func realMain() int {
	options := struct {
		DumpInputData bool

		Format       string
		UseEnvKey    bool
		TemplateFile string
		DataFile     string
		OutputFile   string

		TarFile bool

		CustomFilters     string
		CustomFilterNoops bool

		Autoescape bool

		DirectoryMode bool
	}{
		Format: "",
	}

	app := kingpin.New("p2cli", "Command line templating application based on pongo2")
	app.Version(Version)

	app.Flag("debug", "Print Go serialization to stderr and then exit").Short('d').BoolVar(&options.DumpInputData)
	app.Flag("format", "Input data format").Default("").Short('f').EnumVar(&options.Format, "", "env", "envkey", "json", "yml", "yaml")

	app.Flag("use-env-key", "Treat --input as an environment key name to read.").BoolVar(&options.UseEnvKey)

	app.Flag("template", "Template file to process").Short('t').Required().StringVar(&options.TemplateFile)
	app.Flag("directory-mode", "Treat template path as directory-tree, output path as target directory").BoolVar(&options.DirectoryMode)
	app.Flag("input", "Input data path. Leave blank for stdin.").Short('i').StringVar(&options.DataFile)
	app.Flag("output", "Output file. Leave blank for stdout.").Short('o').StringVar(&options.OutputFile)

	app.Flag("tar", "Output content as a tar file").BoolVar(&options.TarFile)

	app.Flag("enable-filters", "Enable custom p2 filters.").StringVar(&options.CustomFilters)
	app.Flag("enable-noop-filters", "Enable all custom filters in noop mode. Supercedes --enable-filters").BoolVar(&options.CustomFilterNoops)

	app.Flag("autoescape", "Enable autoescaping (disabled by default)").BoolVar(&options.Autoescape)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	if options.TemplateFile == "" {
		log.Errorln("Template file must be specified!")
		return 1
	}

	if options.DirectoryMode {
		tst, _ := os.Stat(options.TemplateFile)
		if !tst.IsDir() {
			log.Errorln("Template path must be a directory in directory mode:", options.TemplateFile)
			return 1
		}

		ost, _ := os.Stat(options.OutputFile)
		if !ost.IsDir() {
			log.Errorln("Output path must be an existing directory in directory mode:", options.TemplateFile)
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
					log.Errorln("This version of p2 does not support the", filter, "custom filter.")
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
	if options.DataFile == "" && options.Format == "" {
		fileFormat = TypeEnv
		inputSource = SourceEnv
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

	if options.UseEnvKey && options.DataFile == "" {
		log.Errorln("--use-env-key is incompatible with stdin file input.")
	} else if options.UseEnvKey {
		inputSource = SourceEnvKey
	}

	var err error
	// Get the input context
	switch fileFormat {
	case TypeEnv:
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
				for _, keyval := range os.Environ() {
					splitKeyVal := strings.SplitN(keyval, "=", 2)
					if len(splitKeyVal) != 2 {
						return error(ErrorEnvironmentVariables{
							Reason:    "Could not find an equals value to split on",
							RawEnvVar: keyval,
						})
					}

					// os.Environ consumption has special-case logic. Since there's all sorts of things
					// which can end up in the environment, we want to filter here only for keys which we
					// like.
					if reIdentifiers.MatchString(splitKeyVal[0]) {
						inputData[splitKeyVal[0]] = splitKeyVal[1]
					}
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

			outputPath, err := filepath.Abs(filepath.Join(options.OutputFile, relPath))
			if err != nil {
				log.Errorln("Could not determine absolute path of output file", options.OutputFile, relPath)
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
