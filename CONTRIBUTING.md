# Contributing

Wanna learn Go? This is the project for you!

When contributing to this repository, please first discuss the change you wish
to make via issue, email, or any other method with the owners of this repository
before making a change.

## So all code changes happen through Pull Requests

Pull requests are the best way to propose changes to the codebase. We actively
welcome your pull requests:

1. Fork the repo and create your branch from `master`.
2. If you've added code that should be tested, add tests.
3. If you've added code that need documentation, update the documentation.
4. Make sure your code follows the [effective go](https://golang.org/doc/effective_go.html) guidelines as much as possible.
5. Be sure to test your modifications.
6. Write a [good commit message](http://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html).
7. Issue that pull request!

## Vendoring

We use a vendor directory to store all dependent files. A vendor directory ensures a single source of truth, so that it's clear in each PR what changes are being made, as well as allowing quick testing-out of ideas across various dependent package, or searching the files in your dependent packages via your editor.

BUT this currently comes at a cost. I have begrudgingly migrated from dep to go modules, and go modules are still working on their support for vendor directories:
https://github.com/golang/go/issues/27227
https://github.com/golang/go/issues/30240

This means there is a little overhead in working with the code base. If you need to make changes to dependent packages, you have two approaches you can take:

# 1)

a) Set `export GOFLAGS=-mod=vendor` in your ~/.bashrc file
b) use `go run main.go` to run lazydocker
c) if you need to bump a dependency e.g. jesseduffield/gocui, use

```
GOFLAGS= go get -u github.com/jesseduffield/gocui@master
go mod tidy
go mod vendor
```

# 2)

a) don't worry about your ~/.bashrc file
b) use `go run -mod=vendor main.go` to run lazydocker
c) if you need to bump a dependency e.g. jesseduffield/gocui, use

```
go get -u github.com/jesseduffield/gocui@master
go mod tidy
go mod vendor
```

Hopefully this will be much more streamlined in the future :)

## Code of conduct

Please note by participating in this project, you agree to abide by the [code of conduct].

[code of conduct]: https://github.com/jesseduffield/lazydocker/blob/master/CODE-OF-CONDUCT.md

## Any contributions you make will be under the MIT Software License

In short, when you submit code changes, your submissions are understood to be
under the same [MIT License](http://choosealicense.com/licenses/mit/) that
covers the project. Feel free to contact the maintainers if that's a concern.

## Report bugs using Github's [issues](https://github.com/jesseduffield/lazydocker/issues)

We use GitHub issues to track public bugs. Report a bug by [opening a new
issue](https://github.com/jesseduffield/lazydocker/issues/new); it's that easy!
