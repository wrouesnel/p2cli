/*
A Golang replica of j2cli from Python. Designed for allowing easy templating
of files using Jinja2-like syntax (from the Pongo2 engine).

Extremely useful for building Docker files when you don't want to pull in all of
python.
 */

package main

import (
	"github.com/kballard/go-shellquote"
	"github.com/voxelbrain/goptions"
	"github.com/wrouesnel/go.log"
	"gopkg.in/flosch/pongo2.v3"
	"fmt"
	"os"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"encoding/json"
	"strings"
	"bytes"
	"bufio"
	"path"
	"io"
)

var Version string = "development"

type SupportedType int
const (
	UNKNOWN SupportedType = iota
	JSON SupportedType = iota
	YAML SupportedType = iota
	ENV SupportedType = iota
)

type DataSource int
const (
	SOURCE_ENV		DataSource = iota	// Input comes from environment
	SOURCE_ENVKEY	DataSource = iota	// Input comes from environment key
	SOURCE_STDIN	DataSource = iota	// Input comes from stdin
	SOURCE_FILE		DataSource = iota	// Input comes from a file
)

var dataFormats map[string]SupportedType = map[string]SupportedType{
	"json" : JSON,
	"yaml" : YAML,
	"yml" : YAML,
	"env" : ENV,
}

var (
	inputData map[string]interface{} = make(map[string]interface{})
)

// Error raised when an environment variable is improperly formatted
type ErrorEnvironmentVariables struct {
	RawEnvVar string
}
func (this ErrorEnvironmentVariables) Error() string {
	return fmt.Sprintf("Unparseable environment variable string: %s", this.RawEnvVar)
}

func readRawInput(name string, source DataSource) []byte {
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
		log.With("filename", name).Fatalln("Invalid data source specified.")
	}

	if err != nil {
		log.With("filename", name).Fatalln("Could not read data:", err)
	}
	return data
}

func main() {
	options := struct {
		Help     goptions.Help `goptions:"-h, --help, description='Show this help'"`
		PrintVersion bool `goptions:"-v, --version, description='Print version'"`
		DumpInputData bool `goptions:"-d, --debug, description='Print Go serialization to stderr'"`

		Format string `goptions:"-f, --format, description='Input data format [valid values: env,yaml,json]'"`
		UseEnvKey bool `goptions:"--use-env-key, description='Treat --input as an environment key name to read.'"`
		TemplateFile string `goptions:"-t, --template, description='Template file to process'"`
		DataFile string `goptions:"-i, --input, description='Input data path. Leave blank for stdin.'"`
		OutputFile string `goptions:"--output, description='Output file. Leave blank for stdout.'"`
	}{
		Format : "",
	}

	goptions.ParseAndFail(&options)

	if options.PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
	
	if options.TemplateFile == "" {
	    log.Fatalln("Template file must be specified!")
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
			log.Fatalln("Unrecognized file extension. If the file is in a supported format, try specifying it explicitely.")
		}
		inputSource = SOURCE_FILE
	} else if options.DataFile == "" && options.Format != "" {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Fatalln("Unsupported input format:", options.Format)
		}
		inputSource = SOURCE_STDIN
	} else {
		var ok bool
		fileFormat, ok = dataFormats[options.Format]
		if !ok {
			log.Fatalln("Unsupported input format:", options.Format)
		}
		inputSource = SOURCE_FILE
	}

	if options.UseEnvKey && options.DataFile == "" {
		log.Fatalln("--use-env-key is incompatible with stdin file input.")
	} else if options.UseEnvKey {
		inputSource = SOURCE_ENVKEY
	}

	// Load template
	tmpl, err := pongo2.FromFile(options.TemplateFile)
	if err != nil {
		log.With("template", options.TemplateFile).
		Fatalln("Could not template file:", err)
	}

	// Get the input context
	switch fileFormat {
	case ENV:
		var environment []string

		if inputSource != SOURCE_ENV {
			lineScanner := bufio.NewScanner(bytes.NewReader(readRawInput(options.DataFile, inputSource)))
			for lineScanner.Scan() {
				environment = append(environment, lineScanner.Text())
			}
		} else {
			// Use literal environment
			environment = os.Environ()
		}

		// This is our bit of custom env var processing code
		err = func(environment []string, inputData map[string]interface{}) error {
			for _, keyval := range environment {
				splitKeyVal := strings.SplitN(keyval, "=", 2)
				if len(splitKeyVal) != 2 {
					return error(ErrorEnvironmentVariables{keyval})
				}
				inputData[splitKeyVal[0]] = splitKeyVal[1]
			}
			return nil
		}(environment, inputData)
	case YAML:
		err = yaml.Unmarshal(readRawInput(options.DataFile, inputSource), &inputData)
	case JSON:
		err = json.Unmarshal(readRawInput(options.DataFile, inputSource), &inputData)
	default:
		log.Fatalln("Unknown input format.")
	}

	if err != nil {
		log.With("template", options.TemplateFile).
			With("data", options.DataFile).
			Fatalln("Error parsing input data:", err)
	}

	if options.DumpInputData {
		fmt.Fprintln(os.Stderr, inputData)
	}

	var outputWriter io.Writer
	if options.OutputFile != "" {
		fileOut, err := os.OpenFile(options.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0777))
		if err != nil {
			log.Fatalln("Error opening output file for writing:", err)
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
			Fatalln("Error parsing input data:", err)
	}
	os.Exit(0)
}
