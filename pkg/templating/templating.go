package templating

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/flosch/pongo2/v4"
	"github.com/pelletier/go-toml"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Directory Mode filters are special filters which are activated during directory mode processing. They do things
// like set file permissions and ownership on the output file from the template file perspective.

const stdoutVal = "<stdout>"

// outputFile defines the current file being templated and is used by the filters below to provide the
// p2 specific functionality.
var outputFilePath string = ""

func FilterSetOwner(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if outputFilePath == stdoutVal {
		return nil, nil
	}

	var uid int
	if in.IsInteger() {
		uid = in.Integer()
	} else if in.IsString() {
		u, err := user.Lookup(in.String())
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetOwner",
				OrigError: err,
			}
		}
		uidraw, err := strconv.ParseInt(u.Uid, 10, 64)
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetOwner",
				OrigError: fmt.Errorf("Cannot convert UID value to int: %v %w", u.Uid, err),
			}
		}
		uid = int(uidraw)
	} else {
		return nil, &pongo2.Error{
			Sender:    "filter:SetOwner",
			OrigError: errors.New("Filter input must be of type 'string' or 'integer'."),
		}
	}

	if err := os.Chown(outputFilePath, uid, -1); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetOwner",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func FilterSetGroup(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if outputFilePath == stdoutVal {
		return nil, nil
	}

	var gid int
	if in.IsInteger() {
		gid = in.Integer()
	} else if in.IsString() {
		u, err := user.LookupGroup(in.String())
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetGroup",
				OrigError: err,
			}
		}
		gidraw, err := strconv.ParseInt(u.Gid, 10, 64)
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetGroup",
				OrigError: fmt.Errorf("Cannot convert UID value to int: %v %w", u.Gid, err),
			}
		}
		gid = int(gidraw)
	} else {
		return nil, &pongo2.Error{
			Sender:    "filter:SetGroup",
			OrigError: errors.New("Filter input must be of type 'string' or 'integer'."),
		}
	}

	if err := os.Chown(outputFilePath, -1, gid); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetGroup",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func FilterSetMode(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if outputFilePath == stdoutVal {
		return nil, nil
	}

	var mode os.FileMode

	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:SetMode",
			OrigError: errors.New("Filter input must be of type 'string' in octal format."),
		}
	}

	strmode := in.String()
	intmode, err := strconv.ParseUint(strmode, 8, 64)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetMode",
			OrigError: err,
		}
	}

	mode = os.FileMode(intmode)

	if err := os.Chmod(outputFilePath, mode); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetMode",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func FilterIndent(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:Indent",
			OrigError: errors.New("Filter input must be of type 'string'."),
		}
	}

	var indent string
	if param.IsString() {
		indent = param.String()
	} else if param.IsInteger() {
		indent = strings.Repeat(" ", param.Integer())
	} else {
		return nil, &pongo2.Error{
			Sender:    "filter:Indent",
			OrigError: errors.New("Filter param must be of type 'string'."),
		}
	}

	input := in.String()

	splitStr := strings.Split(input, "\n")
	for idx, v := range splitStr {
		splitStr[idx] = fmt.Sprintf("%s%s", indent, v)
	}
	return pongo2.AsValue(strings.Join(splitStr, "\n")), nil
}

func FilterToJson(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()

	useIndent := true
	indent := ""
	if param.IsInteger() {
		indent = strings.Repeat(" ", param.Integer())
	} else if param.IsBool() {
		indent = "    "
	} else if param.IsString() {
		indent = param.String()
	} else {
		// We will not be using the indent
		useIndent = false
	}

	var b []byte
	var err error
	if useIndent {
		b, err = json.MarshalIndent(intf, "", indent)
	} else {
		b, err = json.Marshal(intf)
	}

	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:ToJson",
			OrigError: err,
		}
	}

	return pongo2.AsValue(string(b)), nil
}

func FilterToYaml(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()

	b, err := yaml.Marshal(intf)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:ToJson",
			OrigError: err,
		}
	}
	return pongo2.AsValue(string(b)), nil
}

func FilterToToml(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()

	b, err := toml.Marshal(intf)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:ToToml",
			OrigError: err,
		}
	}
	return pongo2.AsValue(string(b)), nil
}

func FilterToBase64(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		// encode string
		return pongo2.AsValue(base64.StdEncoding.EncodeToString([]byte(in.String()))), nil
	}

	intf := in.Interface()
	b, ok := intf.([]byte)
	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:toBase64",
			OrigError: fmt.Errorf("filter requires a []byte or string input"),
		}
	}

	// encode bytes
	return pongo2.AsValue(base64.StdEncoding.EncodeToString(b)), nil
}

func FilterFromBase64(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:FromBase64",
			OrigError: errors.New("Filter input must be of type 'string'."),
		}
	}

	output, err := base64.StdEncoding.DecodeString(in.String())
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:FromBase64",
			OrigError: err,
		}
	}

	// decode as bytes
	return pongo2.AsValue(output), nil
}

func FilterString(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		return pongo2.AsValue(in.String()), nil
	}

	intf := in.Interface()

	b, ok := intf.([]byte)
	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:string",
			OrigError: fmt.Errorf("filter requires a []byte or string input"),
		}
	}
	return pongo2.AsValue(string(b)), nil
}

func FilterBytes(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		return pongo2.AsValue([]byte(in.String())), nil
	}

	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:string",
			OrigError: fmt.Errorf("filter requires a []byte or string input"),
		}
	}

	return pongo2.AsValue(b), nil
}

func FilterToGzip(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	level := 9
	if param.IsInteger() {
		level = param.Integer()
	}

	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:to_gzip",
			OrigError: fmt.Errorf("filter requires a []byte input"),
		}
	}

	buf := bytes.NewBuffer(nil)
	wr, err := gzip.NewWriterLevel(buf, level)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:to_gzip",
			OrigError: err,
		}
	}

	if _, err := wr.Write(b); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:to_gzip",
			OrigError: err,
		}
	}

	err = wr.Close()
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:to_gzip",
			OrigError: err,
		}
	}

	return pongo2.AsValue(buf.Bytes()), nil
}

func FilterFromGzip(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: fmt.Errorf("filter requires a []byte input"),
		}
	}

	rd, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: err,
		}
	}

	output, err := ioutil.ReadAll(rd)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: err,
		}
	}

	return pongo2.AsValue(output), nil
}

type TemplateEngine struct {
	StdOut io.Writer
}

func (te *TemplateEngine) ExecuteTemplate(tmpl *pongo2.Template, inputData pongo2.Context, outputPath string, rootDir string) error {
	logger := zap.L()
	cwd, err := os.Getwd()
	if err != nil {
		logger.Error("Could not get the current working directory", zap.Error(err))
	}

	ctx := make(pongo2.Context)
	p2cliCtx := make(map[string]string)

	var outputWriter io.Writer
	if outputPath != "" {
		fileOut, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0777))
		if err != nil {
			return fmt.Errorf("Error opening output file for writing: %w", err)
		}
		defer func() { _ = fileOut.Close() }()
		outputWriter = io.Writer(fileOut)
		outputFilePath = outputPath

		p2cliCtx["OutputPath"] = outputFilePath
		p2cliCtx["OutputName"] = filepath.Base(outputFilePath)
		p2cliCtx["OutputDir"] = filepath.Dir(outputFilePath)

		p2cliCtx["OutputRelPath"], err = filepath.Rel(rootDir, outputFilePath)
		if err != nil {
			return fmt.Errorf("Could not determine relative output path: %w", err)
		}

		p2cliCtx["OutputRelDir"], err = filepath.Rel(rootDir, filepath.Dir(outputFilePath))
		if err != nil {
			return fmt.Errorf("Could not determine relative output dir: %w", err)
		}

		if err := os.Chdir(filepath.Dir(outputPath)); err != nil {
			return fmt.Errorf("Could not change to template output path directory: %w", err)
		}
	} else {
		outputWriter = te.StdOut
		outputPath = stdoutVal

		p2cliCtx["OutputPath"] = stdoutVal
		p2cliCtx["OutputName"] = stdoutVal
		p2cliCtx["OutputDir"] = rootDir
		p2cliCtx["OutputRelPath"] = stdoutVal
		p2cliCtx["OutputRelDir"] = "."
	}

	ctx["p2"] = p2cliCtx
	ctx.Update(inputData)

	// Everything loaded, so try rendering the template.
	terr := tmpl.ExecuteWriter(ctx, outputWriter)

	if err := os.Chdir(cwd); err != nil {
		return fmt.Errorf("Could not change back to original working directory: %w", err)
	}

	return terr
}
