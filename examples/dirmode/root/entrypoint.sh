#!/bin/bash
# Example of templating a script
# Note the use of SetMode which ensures this file is emitted with the correct mode.
{{ "0755"|SetMode }}
exec {{ subcommand }}