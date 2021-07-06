package main

import (
	"fmt"
	"github.com/flosch/pongo2/v4"
	"os"
)

// This noop filter is registered in place of custom filters which otherwise
// passthru their input (our file filters). This allows debugging and testing
// without running file operations.
func filterNoopPassthru(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	return in, nil
}

// This noop filter is registered in place of custom filters which otherwise
// produce no output.
func filterNoop(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	return nil, nil
}

// This filter writes the content of its input to the filename specified as its
// argument. The templated content is returned verbatim.
func filterWriteFile(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:   "filter:write_file",
			Filename: "Filter input must be of type 'string'.",
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:write_file",
		//	ErrorMsg: "Filter input must be of type 'string'.",
		//}
	}

	if !param.IsString() {
		return nil, &pongo2.Error{
			Sender:   "filter:write_file",
			Filename: "Filter parameter must be of type 'string'.",
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:write_file",
		//	ErrorMsg: "Filter parameter must be of type 'string'.",
		//}
	}

	f, err := os.OpenFile(param.String(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0777))
	if err != nil {
		return nil, &pongo2.Error{
			Sender:   "filter:write_file",
			Filename: fmt.Sprintf("Could not open file for output: %s", err.Error()),
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:write_file",
		//	ErrorMsg: fmt.Sprintf("Could not open file for output: %s", err.Error()),
		//}
	}
	defer f.Close()

	_, werr := f.WriteString(in.String())
	if werr != nil {
		return nil, &pongo2.Error{
			Sender:   "filter:write_file",
			Filename: fmt.Sprintf("Could not write file for output: %s", werr.Error()),
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:write_file",
		//	ErrorMsg: fmt.Sprintf("Could not write file for output: %s", werr.Error()),
		//}
	}

	return in, nil
}

// This filter makes a directory based on the value of its argument. It passes
// through any content without alteration. This allows chaining with write-file.
func filterMakeDirs(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !param.IsString() {
		return nil, &pongo2.Error{
			Sender:   "filter:make_dirs",
			Filename: "Filter parameter must be of type 'string'.",
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:make_dirs",
		//	ErrorMsg: "Filter parameter must be of type 'string'.",
		//}
	}

	err := os.MkdirAll(param.String(), os.FileMode(0777))
	if err != nil {
		return nil, &pongo2.Error{
			Sender:   "filter:make_dirs",
			Filename: fmt.Sprintf("Could not create directories: %s %s", in.String(), err.Error()),
		}
		//return nil, &pongo2.Error{
		//	Sender:   "filter:make_dirs",
		//	ErrorMsg: fmt.Sprintf("Could not create directories: %s %s", in.String(), err.Error()),
		//}
	}

	return in, nil
}
