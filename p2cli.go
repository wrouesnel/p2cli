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
	"github.com/flosch/pongo2"
	"github.com/kballard/go-shellquote"
	"github.com/voxelbrain/goptions"
	"github.com/wrouesnel/go.log"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
)

var Version string = "development"

type SupportedType int

const (
	UNKNOWN SupportedType = iota
	JSON    SupportedType = iota
	YAML    SupportedType = iota
	ENV     SupportedType = iota
)

type DataSource int

const (
	SOURCE_ENV    DataSource = iota // Input comes from environment
	SOURCE_ENVKEY DataSource = iota // Input comes from environment key
	SOURCE_STDIN  DataSource = iota // Input comes from stdin
	SOURCE_FILE   DataSource = iota // Input comes from a file
)

var dataFormats map[string]SupportedType = map[string]SupportedType{
	"json": JSON,
	"yaml": YAML,
	"yml":  YAML,
	"env":  ENV,
}

// Map of custom filters p2 implements. These are gated behind the --enable-filter
// command line option as they can have unexpected or even unsafe behavior (i.e.
// templates gain the ability to make filesystem modifications).
// Disabled filters are stubbed out to allow for debugging.

type CustomFilterSpec struct {
	FilterFunc pongo2.FilterFunction
	NoopFunc   pongo2.FilterFunction
}

var customFilters map[string]CustomFilterSpec = map[string]CustomFilterSpec{
	"write_file": CustomFilterSpec{filterWriteFile, filterNoopPassthru},
	"make_dirs":  CustomFilterSpec{filterMakeDirs, filterNoopPassthru},
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

func readRawInput(name string, source DataSource) ([]byte,error) {
	var data []byte
	var err error
	switch source {
	case SOURCE_STDIN:
		// Read from stdin
		name = "-"
		data, err = ioutil.ReadAll(os.Stdin)
	case SOURCE_FILE:
		// Read from file
		data, err = ioutil.ReadFile(name)
	case SOURCE_ENVKEY:
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
		Help          goptions.Help `goptions:"-h, --help, description='Show this help'"`
		PrintVersion  bool          `goptions:"-v, --version, description='Print version'"`
		DumpInputData bool          `goptions:"-d, --debug, description='Print Go serialization to stderr'"`

		Format       string `goptions:"-f, --format, description='Input data format [valid values: env,yaml,json]'"`
		UseEnvKey    bool   `goptions:"--use-env-key, description='Treat --input as an environment key name to read.'"`
		TemplateFile string `goptions:"-t, --template, description='Template file to process'"`
		DataFile     string `goptions:"-i, --input, description='Input data path. Leave blank for stdin.'"`
		OutputFile   string `goptions:"-o, --output, description='Output file. Leave blank for stdout.'"`

		CustomFilters     string `goptions:"--enable-filters, description='Enable custom p2 filters.'"`
		CustomFilterNoops bool   `goptions:"--enable-noop-filters, description='Enable all custom filters in noop mode. Supercedes --enable-filters'"`
	}{
		Format: "",
	}

	goptions.ParseAndFail(&options)

	if options.PrintVersion {
		fmt.Println(Version)
		return 0
	}

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
	var fileFormat SupportedType = UNKNOWN
	var inputSource DataSource = SOURCE_ENV
	if options.DataFile == "" && options.Format == "" {
		fileFormat = ENV
		inputSource = SOURCE_ENV
	} else if options.DataFile != "" && options.Format == "" {
		var ok bool
		fileFormat, ok = dataFormats[strings.TrimLeft(path.Ext(options.DataFile), ".")]
		if !ok {
			log.Errorln("Unrecognized file extension. If the file is in a supported format, try specifying it explicitely.")
			return 1
		}
		inputSource = SOURCE_FILE
	} else if options.DataFile == "" && options.Format != "" {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Errorln("Unsupported input format:", options.Format)
			return 1
		}
		inputSource = SOURCE_STDIN
	} else {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Errorln("Unsupported input format:", options.Format)
			return 1
		}
		inputSource = SOURCE_FILE
	}

	if options.UseEnvKey && options.DataFile == "" {
		log.Errorln("--use-env-key is incompatible with stdin file input.")
	} else if options.UseEnvKey {
		inputSource = SOURCE_ENVKEY
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
	case ENV:
		err = func(inputData map[string]interface{}) error {
			if inputSource != SOURCE_ENV {
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
	case YAML:
		rawInput, err := readRawInput(options.DataFile, inputSource)
		if err != nil {
			return 1
		}
		err = yaml.Unmarshal(rawInput, &inputData)
	case JSON:
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
		fmt.Fprintln(os.Stderr, inputData)
	}

	var outputWriter io.Writer
	if options.OutputFile != "" {
		fileOut, err := os.OpenFile(options.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0777))
		if err != nil {
			log.Errorln("Error opening output file for writing:", err)
			return 1
		}
		defer fileOut.Close()
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
