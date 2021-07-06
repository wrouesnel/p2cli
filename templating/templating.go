package templating

import (
	"errors"
	"fmt"
	"github.com/flosch/pongo2/v4"
	log "github.com/wrouesnel/go.log"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
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

func ExecuteTemplate(tmpl *pongo2.Template, inputData pongo2.Context, outputPath string, rootDir string) error {
	cwd, err := os.Getwd()
	if err != nil {
		log.Errorln("Could not get the current working directory:", err)
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
		outputWriter = os.Stdout
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
