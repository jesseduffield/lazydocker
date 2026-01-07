# ![GoTree](https://rawgit.com/DiSiqueira/GoTree/master/gotree-logo.png)

# GoTree ![Language Badge](https://img.shields.io/badge/Language-Go-blue.svg) ![Go Report](https://goreportcard.com/badge/github.com/DiSiqueira/GoTree) ![License Badge](https://img.shields.io/badge/License-MIT-blue.svg) ![Status Badge](https://img.shields.io/badge/Status-Beta-brightgreen.svg) [![GoDoc](https://godoc.org/github.com/DiSiqueira/GoTree?status.svg)](https://godoc.org/github.com/DiSiqueira/GoTree) [![Build Status](https://travis-ci.org/DiSiqueira/GoTree.svg?branch=master)](https://travis-ci.org/DiSiqueira/GoTree)

Simple Go module to print tree structures in terminal. Heavily inpired by [The Tree Command for Linux][treecommand]

The GoTree's goal is to be a simple tool providing a stupidly easy-to-use and fast way to print recursive structures.

[treecommand]: http://mama.indstate.edu/users/ice/tree/

## Project Status

GoTree is on beta. Pull Requests [are welcome](https://github.com/DiSiqueira/GoTree#social-coding)

![](http://image.prntscr.com/image/2a0dbf0777454446b8083fb6a0dc51fe.png)

## Features

- Very simple and fast code
- Intuitive names
- Easy to extend
- Uses only native libs
- STUPIDLY [EASY TO USE](https://github.com/DiSiqueira/GoTree#usage)

## Installation

### Go Get

```bash
$ go get github.com/disiqueira/gotree
```

## Usage

### Simple create, populate and print example

![](http://image.prntscr.com/image/dd2fe3737e6543f7b21941a6953598c2.png)

```golang
package main

import (
    "fmt"

    "github.com/disiqueira/gotree"
)

func main() {
	artist := gotree.New("Pantera")
	album := artist.Add("Far Beyond Driven")
	album.Add("5 minutes Alone")

	fmt.Println(artist.Print())
}
```

## Contributing

### Bug Reports & Feature Requests

Please use the [issue tracker](https://github.com/DiSiqueira/GoTree/issues) to report any bugs or file feature requests.

### Developing

PRs are welcome. To begin developing, do this:

```bash
$ git clone --recursive git@github.com:DiSiqueira/GoTree.git
$ cd GoTree/
```

## Social Coding

1. Create an issue to discuss about your idea
2. [Fork it] (https://github.com/DiSiqueira/GoTree/fork)
3. Create your feature branch (`git checkout -b my-new-feature`)
4. Commit your changes (`git commit -am 'Add some feature'`)
5. Push to the branch (`git push origin my-new-feature`)
6. Create a new Pull Request
7. Profit! :white_check_mark:

## License

The MIT License (MIT)

Copyright (c) 2013-2018 Diego Siqueira

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
