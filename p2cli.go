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
	"fmt"
	"github.com/alecthomas/kingpin"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/kballard/go-shellquote"
	"github.com/wrouesnel/go.log"
	"gopkg.in/yaml.v2"
)

var Version string = "development"

type SupportedType int

const (
	TypeUnknown SupportedType = iota
	TypeJSON    SupportedType = iota
	TypeYAML    SupportedType = iota
	TypeEnv     SupportedType = iota
)

type DataSource int

const (
	SourceEnv    DataSource = iota // Input comes from environment
	SourceEnvKey DataSource = iota // Input comes from environment key
	SourceStdin  DataSource = iota // Input comes from stdin
	SourceFile   DataSource = iota // Input comes from a file
)

var dataFormats map[string]SupportedType = map[string]SupportedType{
	"json": TypeJSON,
	"yaml": TypeYAML,
	"yml":  TypeYAML,
	"env":  TypeEnv,
}

// Map of custom filters p2 implements. These are gated behind the --enable-filter
// command line option as they can have unexpected or even unsafe behavior (i.e.
// templates gain the ability to make filesystem modifications).
// Disabled filters are stubbed out to allow for debugging.

type CustomFilterSpec struct {
	FilterFunc pongo2.FilterFunction
	NoopFunc   pongo2.FilterFunction
}

var customFilters = map[string]CustomFilterSpec{
	"write_file": {filterWriteFile, filterNoopPassthru},
	"make_dirs":  {filterMakeDirs, filterNoopPassthru},
}

var (
	inputData map[string]interface{} = make(map[string]interface{})
)

// Error raised when an environment variable is improperly formatted
type ErrorEnvironmentVariables struct {
	Reason    string
	RawEnvVar string
}

func (this ErrorEnvironmentVariables) Error() string {
	return fmt.Sprintf("%s: %s", this.Reason, this.RawEnvVar)
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

		CustomFilters     string
		CustomFilterNoops bool
	}{
		Format: "",
	}

	app := kingpin.New("p2cli", "Command line templating application based on pongo2")
	app.Version(Version)

	app.Flag("debug", "Print Go serialization to stderr and then exit").Short('d').BoolVar(&options.DumpInputData)
	app.Flag("format", "Input data format").Default("env").Short('f').EnumVar(&options.Format, "env", "envkey", "json", "yml", "yaml")

	app.Flag("use-env-key", "Treat --input as an environment key name to read.").BoolVar(&options.UseEnvKey)

	app.Flag("template", "Template file to process").Short('t').Required().StringVar(&options.TemplateFile)
	app.Flag("input", "Input data path. Leave blank for stdin.").Short('i').StringVar(&options.DataFile)
	app.Flag("output", "Output file. Leave blank for stdout.").Short('o').StringVar(&options.OutputFile)

	app.Flag("enable-filters", "Enable custom p2 filters.").StringVar(&options.CustomFilters)
	app.Flag("enable-noop-filters", "Enable all custom filters in noop mode. Supercedes --enable-filters").BoolVar(&options.CustomFilterNoops)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	if options.TemplateFile == "" {
		log.Errorln("Template file must be specified!")
		return 1
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

	// Determine mode of operations
	var fileFormat SupportedType = TypeUnknown
	var inputSource DataSource = SourceEnv
	if options.DataFile == "" && options.Format == "" {
		fileFormat = TypeEnv
		inputSource = SourceEnv
	} else if options.DataFile != "" && options.Format == "" {
		var ok bool
		fileFormat, ok = dataFormats[strings.TrimLeft(path.Ext(options.DataFile), ".")]
		if !ok {
			log.Errorln("Unrecognized file extension. If the file is in a supported format, try specifying it explicitely.")
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

	// Load template
	tmpl, err := pongo2.FromFile(options.TemplateFile)
	if err != nil {
		log.With("template", options.TemplateFile).
			Errorln("Could not template file:", err)
		return 1
	}

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

					inputData[splitKeyVal[0]] = splitKeyVal[1]
				}
			}
			return nil
		}(inputData)
	case TypeYAML:
		rawInput, err := readRawInput(options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = yaml.Unmarshal(rawInput, &inputData)
	case TypeJSON:
		rawInput, err := readRawInput(options.DataFile, inputSource)
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

	var outputWriter io.Writer
	if options.OutputFile != "" {
		fileOut, err := os.OpenFile(options.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0777))
		if err != nil {
			log.Errorln("Error opening output file for writing:", err)
			return 1
		}
		defer func() { _ = fileOut.Close() }()
		outputWriter = io.Writer(fileOut)
	} else {
		outputWriter = os.Stdout
	}

	// Everything loaded, so try rendering the template.
	err = tmpl.ExecuteWriter(pongo2.Context(inputData), outputWriter)
	if err != nil {
		log.With("template", options.TemplateFile).
			With("data", options.DataFile).
			Errorln("Error parsing input data:", err)
		return 1
	}
	return 0
}
