# p2cli
[![Build Status](https://drone.io/github.com/wrouesnel/p2cli/status.png)](https://drone.io/github.com/wrouesnel/p2cli/latest)
A command line tool for rendering pongo2 (jinja2) templates to stdout.

The rendering library is (pongo2)[https://github.com/flosch/pongo2].

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
It is technically possible to store complex data in environment variables. `p2`
supports this use-case (without commenting if it's a good idea). To use it,
pass the name of the environment variable as the `--input` and specify
`--use-env-key` and `--format`
```
p2 -t template.j2 -f json --use-env-key -i MY_ENV_VAR
```

## Building

It is recommended to build using the included Makefile. This correctly sets up
Go to build a cgo-independent, statically linked binary.

Note: users on platforms other then Linux will need to specify GOOS when
building.
