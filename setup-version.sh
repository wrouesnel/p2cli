#   Copyright 2014 Commonwealth Bank of Australia
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

#!/bin/bash

set -e
set -u
set -v

function get_version() {
    local branch=$TRAVIS_BRANCH
    local ts=`date "+%Y%m%d%H%M%S"`
    local commish=`git rev-parse --short HEAD`
    local version="$1-$ts-$commish"
    if [ $TRAVIS_PULL_REQUEST != "false" ]; then
        echo "$version-PR$TRAVIS_PULL_REQUEST"
    elif [ $TRAVIS_BRANCH == "master" ]; then
        echo "$version"
    else
        echo "$version-$branch"
    fi
}

if [ -f VERSION ]; then
    version=`grep -E -o "[0-9]+\.[0-9]+\.[0-9]+" VERSION`
    echo $version
    new_version=$(get_version $version)
    echo "$new_version" > VERSION
else
    exit 1
fi

exit 0
