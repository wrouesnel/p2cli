# p2cli
[![Build Status](https://travis-ci.org/wrouesnel/p2cli.svg?branch=master)](https://travis-ci.org/wrouesnel/p2cli)

A command line tool for rendering pongo2 (jinja2) templates to stdout.

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
- user: mike
  content: This is Mike's content.
- user: sally
  content: This is Sally's content.
- user: greg
  content: This is greg's content.
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
$ p2 --enable-write_file -t template.p2 -i input.yml



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

## Building

It is recommended to build using the included Makefile. This correctly sets up
Go to build a cgo-independent, statically linked binary.

Note: users on platforms other then Linux will need to specify GOOS when
building.

### Vendoring
Vendoring is managed by govendor
