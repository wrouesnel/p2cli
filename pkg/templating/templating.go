package templating

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/flosch/pongo2/v6"
	"github.com/pelletier/go-toml"
	"gopkg.in/yaml.v2"
)

// Directory Mode filters are special filters which are activated during directory mode processing. They do things
// like set file permissions and ownership on the output file from the template file perspective.

const StdOutVal = "<stdout>"

type FilterError struct {
	Reason string
}

func (e FilterError) Error() string {
	return e.Reason
}

// FilterSet implements filter-returning functions which can support context information such as the
// name of the output file.
type FilterSet struct {
	OutputFileName string
	Chown          func(name string, uid, gid int) error
	Chmod          func(name string, mode os.FileMode) error
}

func (fs *FilterSet) FilterSetOwner(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if fs.OutputFileName == StdOutVal {
		return nil, nil
	}

	var uid int
	switch {
	case in.IsInteger():
		uid = in.Integer()
	case in.IsString():
		userData, err := user.Lookup(in.String())
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetOwner",
				OrigError: err,
			}
		}
		uidraw, err := strconv.ParseInt(userData.Uid, 10, 64)
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetOwner",
				OrigError: fmt.Errorf("cannot convert UID value to int: %v %w", userData.Uid, err),
			}
		}
		uid = int(uidraw)
	default:
		return nil, &pongo2.Error{
			Sender:    "filter:SetOwner",
			OrigError: FilterError{Reason: "filter input must be of type 'string' or 'integer'."},
		}
	}

	if err := fs.Chown(fs.OutputFileName, uid, -1); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetOwner",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func (fs *FilterSet) FilterSetGroup(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if fs.OutputFileName == StdOutVal {
		return nil, nil
	}

	var gid int
	switch {
	case in.IsInteger():
		gid = in.Integer()
	case in.IsString():
		userData, err := user.LookupGroup(in.String())
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetGroup",
				OrigError: err,
			}
		}
		gidraw, err := strconv.ParseInt(userData.Gid, 10, 64)
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:SetGroup",
				OrigError: fmt.Errorf("cannot convert UID value to int: %v %w", userData.Gid, err),
			}
		}
		gid = int(gidraw)
	default:
		return nil, &pongo2.Error{
			Sender:    "filter:SetGroup",
			OrigError: FilterError{Reason: "filter input must be of type 'string' or 'integer'."},
		}
	}

	if err := os.Chown(fs.OutputFileName, -1, gid); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetGroup",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func (fs *FilterSet) FilterSetMode(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if fs.OutputFileName == StdOutVal {
		return nil, nil
	}

	var mode os.FileMode

	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:SetMode",
			OrigError: FilterError{Reason: "filter input must be of type 'string' in octal format."},
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

	if err := fs.Chmod(fs.OutputFileName, mode); err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:SetMode",
			OrigError: err,
		}
	}
	return pongo2.AsValue(""), nil
}

func (fs *FilterSet) FilterIndent(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:Indent",
			OrigError: FilterError{Reason: "filter input must be of type 'string'."},
		}
	}

	var indent string
	switch {
	case param.IsString():
		indent = param.String()
	case param.IsInteger():
		indent = strings.Repeat(" ", param.Integer())
	default:
		return nil, &pongo2.Error{
			Sender:    "filter:Indent",
			OrigError: FilterError{Reason: "filter param must be of type 'string'."},
		}
	}

	input := in.String()

	splitStr := strings.Split(input, "\n")
	for idx, v := range splitStr {
		splitStr[idx] = fmt.Sprintf("%s%s", indent, v)
	}
	return pongo2.AsValue(strings.Join(splitStr, "\n")), nil
}

//nolint:funlen,gomnd
func (fs *FilterSet) FilterReplace(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:Replace",
			OrigError: FilterError{Reason: "filter input must be of type 'string'."},
		}
	}

	if !param.CanSlice() {
		return nil, &pongo2.Error{
			Sender:    "filter:Replace",
			OrigError: FilterError{Reason: "filter param must be of type 'slice'."},
		}
	}

	if param.Len() != 2 && param.Len() != 3 {
		return nil, &pongo2.Error{
			Sender:    "filter:Replace",
			OrigError: FilterError{Reason: "filter param must be of type 'slice' containing 2 strings or 2 strings and int."},
		}
	}

	var matchParam *pongo2.Value
	var replaceParam *pongo2.Value
	var countParam *pongo2.Value

	matchParam = param.Index(0)
	replaceParam = param.Index(1)

	if param.Len() == 3 {
		countParam = param.Index(2)
	}

	// For some reason, IsString fails with our set args. But it's probably
	// fine to ignore that for now.
	//if !matchParam.IsString() {
	//	return nil, &pongo2.Error{
	//		Sender:    "filter:Replace",
	//		OrigError: FilterError{Reason: "element 0 of filter param must be a string"},
	//	}
	//}
	//
	//if !replaceParam.IsString() {
	//	return nil, &pongo2.Error{
	//		Sender:    "filter:Replace",
	//		OrigError: FilterError{Reason: "element 1 of filter param must be a string"},
	//	}
	//}
	//
	//if countParam != nil {
	//	if !countParam.IsInteger() {
	//		return nil, &pongo2.Error{
	//			Sender:    "filter:Replace",
	//			OrigError: FilterError{Reason: "element 2 of filter param must be an integer"},
	//		}
	//	}
	//}

	match := matchParam.String()
	replace := replaceParam.String()

	input := in.String()

	var result string
	if countParam == nil {
		result = strings.ReplaceAll(input, match, replace)
	} else {
		// Set notation returns strings
		countStr := countParam.String()
		count, err := strconv.ParseInt(countStr, 10, 64)
		if err != nil {
			return nil, &pongo2.Error{
				Sender:    "filter:Replace",
				OrigError: FilterError{Reason: "element 2 of filter param must be an integer"},
			}
		}

		result = strings.Replace(input, match, replace, int(count))
	}

	return pongo2.AsValue(result), nil
}

func (fs *FilterSet) FilterToJSON(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()

	useIndent := true
	indent := ""
	switch {
	case param.IsInteger():
		indent = strings.Repeat(" ", param.Integer())
	case param.IsBool():
		indent = "    "
	case param.IsString():
		indent = param.String()
	default:
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

func (fs *FilterSet) FilterToYAML(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
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

func (fs *FilterSet) FilterToTOML(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
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

func (fs *FilterSet) FilterToBase64(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		// encode string
		return pongo2.AsValue(base64.StdEncoding.EncodeToString([]byte(in.String()))), nil
	}

	intf := in.Interface()
	b, ok := intf.([]byte)
	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:toBase64",
			OrigError: FilterError{Reason: "filter requires a []byte or string input"},
		}
	}

	// encode bytes
	return pongo2.AsValue(base64.StdEncoding.EncodeToString(b)), nil
}

func (fs *FilterSet) FilterFromBase64(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if !in.IsString() {
		return nil, &pongo2.Error{
			Sender:    "filter:FromBase64",
			OrigError: FilterError{Reason: "filter input must be of type 'string'."},
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

func (fs *FilterSet) FilterString(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		return pongo2.AsValue(in.String()), nil
	}

	intf := in.Interface()

	byteData, ok := intf.([]byte)
	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:string",
			OrigError: FilterError{Reason: "filter requires a []byte or string input"},
		}
	}
	return pongo2.AsValue(string(byteData)), nil
}

func (fs *FilterSet) FilterBytes(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	if in.IsString() {
		return pongo2.AsValue([]byte(in.String())), nil
	}

	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:string",
			OrigError: FilterError{Reason: "filter requires a []byte or string input"},
		}
	}

	return pongo2.AsValue(b), nil
}

func (fs *FilterSet) FilterToGzip(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	level := 9
	if param.IsInteger() {
		level = param.Integer()
	}

	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:to_gzip",
			OrigError: FilterError{Reason: "filter requires a []byte input"},
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

func (fs *FilterSet) FilterFromGzip(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	intf := in.Interface()
	b, ok := intf.([]byte)

	if !ok {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: FilterError{Reason: "filter requires a []byte input"},
		}
	}

	rd, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: err,
		}
	}

	output, err := io.ReadAll(rd)
	if err != nil {
		return nil, &pongo2.Error{
			Sender:    "filter:from_gzip",
			OrigError: err,
		}
	}

	return pongo2.AsValue(output), nil
}
