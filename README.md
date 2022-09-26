# p2cli
![Build Status](https://github.com/wrouesnel/p2cli/actions/workflows/release.yml/badge.svg?branch=master)
[![Coverage Status](https://coveralls.io/repos/github/wrouesnel/p2cli/badge.svg?branch=master)](https://coveralls.io/github/wrouesnel/p2cli?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/wrouesnel/p2cli)](https://goreportcard.com/report/github.com/wrouesnel/p2cli)

A command line tool for rendering pongo2 (jinja2-like) templates to stdout.

The rendering library is [pongo2](https://github.com/flosch/pongo2).

It is inspired (and pretty much a copy of) the j2cli utility for Python, but
leveraging Golang's static compilation for easier use in Docker and other
minimal environments.

## Usage
p2 defaults to using the local environment variables as a data source.

If `--format` is specified explicitely, then p2 will read that format from
stdin (or the supplied given data file specified with `--input`).

If only `--input` is specified, then it will guess the data type based on the
file extension.

Render template with environment variables (most useful for Docker):
```
p2 -t template.j2
```

Render a template with environment variables piped from stdin:
```
cat vars.env | p2 -t template.j2
```

Render a template with using a JSON source file:
```
p2 -t template.j2 -i source.json
```

Render a template using a YAML on stdin:
```
cat someYaml | p2 -t template.j2 -f yaml
```

### Advanced Usage

#### Extra Built-In Filters

* `indent` - output data with the given indent. Can be given either a string or number of spaces.
* `to_json` - outputs structured data as JSON. Supplying a parameter sets the indent.
* `to_yaml` - outputs structured data as YAML.
* `to_toml` - outputs structured data as TOML. Must be supplied a map.
* `string` - convert input data to string (use with `from_base64`)
* `bytes` - convert input data to bytes
* `to_base64` - encode a string or bytes to base64
* `from_base64` - decode a string from base64 to bytes
* `to_gzip` - compress bytes with gzip (supply level as parameter, default 9)
* `from_gzip` - decompress bytes with gzip

#### Special Output Functions

Several utility functions are provided to improve the configuration file
templating experience:

* `SetOwner` - try and set the owner of the output file to the supplied argument.
* `SetGroup` - try and get the group of the output file to the supplied argument.
* `SetMode` - try and set the mode of the output file to the supplied argument.

Additionally, special variables are exposed under the key "p2":

* `p2.OutputPath` - return the output path of the current template.
* `p2.OutputName` - return the basename of the output path.
* `p2.OutputDir` - return the directory of the current output path.

#### Directory tree templating via `--directory-mode`

Invoking `p2` with the `--directory-mode` option causes it to expect that the template file
path is in fact a directory tree root which should be duplicated and copied to
1:1 to the output path, which also must be a directory. This is useful for
templating large numbers of files in complex configurations using a single
input source.

In this mode, directives such as `write_file` execute relative to to the
template file subpath they are found in - i.e. the working directory is
changed to be the output directory location of the input template file. 

The `SetOwner`, `SetGroup`, and `SetMode` special filters exist principally
to support this mode.

#### Delete substrings in output filenames when `--directory-mode` enabled

You can use the optional flag `--directory-mode-filename-substr-del` to delete 
substrings that are present in output filenames when running `p2` with 
`--directory-mode` enabled. 

This is useful for cases where your templates have asuffix e.g `.tmpl`, 
`.template` that you want removed once the template has been rendered. For 
example, the output file for a template named `mytemplate.tmpl.json` will 
become `mytemplate.json`.

#### Side-effectful filters
`p2` allows enabling a suite of non-standard pongo2 filters which have
side-effects on the system. These filters add a certain amount of
minimal scripting ability to `p2`, namely by allowing a single template
to use filters which can perform operations like writing files and
creating directories.

These are __unsafe__ filters to use with uninspected templates, and so
by default are disabled. They can be enabled on a per-filter basis with
the `--enable-filters` flag. For template debugging purposes, they can
also be enabled in no-op mode with `--enable-noops` which will allow
all filters but disable their side-effects.

#### Passing structured data in environment variable keys
It is technically possible to store complex data in environment variables. `p2`
supports this use-case (without commenting if it's a good idea). To use it,
pass the name of the environment variable as the `--input` and specify
`--use-env-key` and `--format`
```
p2 -t template.j2 -f json --use-env-key -i MY_ENV_VAR
```

#### Multiple file templating via `write_file`
`p2` implements the custom `write_file` filter extension to pongo2.
`write_file` takes a filename as an argument (which can itself be a
templated value) and creates and outputs any input content to that
filename (overwriting the destination file).

This makes it possible to write a template which acts more like a
script, and generates multiple output values. Example:

`input.yml`:
```yaml
users:
- user:
  name: Mike
  content: This is Mike's content.
- user:
  name: Sally
  content: This is Sally's content.
- user:
  name: Greg
  content: This is Greg's content.
```

`template.p2`:
```Django
{% macro user_content(content) %}
{{content|safe}}
{% endmacro %}

{% for user in users %}
##  {{user.name}}.txt output
{% set filename = user.name|stringformat:"%s.txt" %}
{{ user_content( user.content ) | write_file:filename }}
##
{% endfor %}
```

Now executing the template:
```sh
$ p2 -t template.p2 -i input.yml -f yaml --enable-filters write_file



##  mike.txt output


This is Mike's content.

##

##  sally.txt output


This is Sally's content.

##

##  greg.txt output


This is greg's content.

##

$ ls
greg.txt  input.yml  mike.txt  sally.txt  template.p2
```

We get the output, but we have also created a new set of files
containing the content from our macro.

Note that until pongo2 supports multiple filter arguments, the file
output plugin creates files with the maximum possible umask of the user.

#### Run p2 in docker
```
docker build . -t p2
docker run -v $(pwd):/t -ti p2 -t /t/template.p2 -i /t/input.yml
```

## Building

It is recommended to build using the included Makefile. This correctly sets up
Go to build a cgo-independent, statically linked binary.

Note: users on platforms other then Linux will need to specify GOOS when
building.

### Vendoring
Vendoring is managed by govendor
